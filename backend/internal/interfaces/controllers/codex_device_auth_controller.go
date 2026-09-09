package controllers

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/labstack/echo/v4"
	"github.com/takutakahashi/agentapi-proxy/internal/domain/entities"
	"github.com/takutakahashi/agentapi-proxy/internal/usecases/ports/repositories"
	"github.com/takutakahashi/agentapi-proxy/pkg/auth"
	"github.com/takutakahashi/agentapi-proxy/pkg/codexauth"
	"github.com/takutakahashi/agentapi-proxy/pkg/sessionsettings"
)

const (
	deviceAuthTTL          = 10 * time.Minute
	maxDeviceAuthBodyBytes = 64 << 10
	maxAuthJSONBytes       = 32 << 10
)

type deviceAuthAttempt struct {
	ID             string
	UserID         string
	CredentialName string
	TokenHash      [sha256.Size]byte
	Status         string
	Challenge      codexauth.Challenge
	ExpiresAt      time.Time
	mu             sync.RWMutex
}

type CodexDeviceAuthController struct {
	repo       repositories.CredentialsRepository
	launcher   codexauth.WorkloadLauncher
	store      codexauth.AttemptStore
	attempts   sync.Map // attempt ID -> *deviceAuthAttempt
	active     sync.Map // credential name -> attempt ID
	userLatest sync.Map // user ID -> attempt ID; legacy poll compatibility
}

func NewCodexDeviceAuthController(repo repositories.CredentialsRepository, launchers ...codexauth.WorkloadLauncher) *CodexDeviceAuthController {
	c := &CodexDeviceAuthController{repo: repo}
	if len(launchers) > 0 {
		c.launcher = launchers[0]
	}
	return c
}

func (c *CodexDeviceAuthController) WithAttemptStore(store codexauth.AttemptStore) *CodexDeviceAuthController {
	c.store = store
	return c
}

func (c *CodexDeviceAuthController) GetName() string { return "CodexDeviceAuthController" }

type StartDeviceAuthRequest struct {
	Scope   string `json:"scope,omitempty"`
	TeamID  string `json:"team_id,omitempty"`
	Replace bool   `json:"replace,omitempty"`
}

type StartDeviceAuthResponse struct {
	AttemptID       string    `json:"attempt_id"`
	Status          string    `json:"status"`
	UserCode        string    `json:"user_code,omitempty"`
	VerificationURI string    `json:"verification_uri,omitempty"`
	ExpiresAt       time.Time `json:"expires_at"`
	PollAfterMS     int       `json:"poll_after_ms"`
}

type PollDeviceAuthResponse struct {
	Status string `json:"status"`
}

type CodexAuthConfigResponse struct {
	Configured    bool   `json:"configured"`
	ExecutionMode string `json:"execution_mode,omitempty"`
}

// workloadAvailability is implemented by launchers that can report whether an
// execution plane is currently reachable.
type workloadAvailability interface {
	Available(context.Context) bool
}

func (c *CodexDeviceAuthController) GetConfig(ctx echo.Context) error {
	if auth.GetUserFromContext(ctx) == nil {
		return echo.NewHTTPError(http.StatusUnauthorized, "Authentication required")
	}
	configured := c.launcher != nil
	if configured {
		if provider, ok := c.launcher.(workloadAvailability); ok {
			configured = provider.Available(ctx.Request().Context())
		}
	}
	return ctx.JSON(http.StatusOK, CodexAuthConfigResponse{Configured: configured, ExecutionMode: "auth_workload"})
}

func (c *CodexDeviceAuthController) StartDeviceAuth(ctx echo.Context) error {
	user := auth.GetUserFromContext(ctx)
	if user == nil {
		return echo.NewHTTPError(http.StatusUnauthorized, "Authentication required")
	}
	if c.launcher == nil {
		return echo.NewHTTPError(http.StatusServiceUnavailable, "Codex device auth workload is unavailable")
	}
	var req StartDeviceAuthRequest
	if ctx.Request().ContentLength != 0 {
		if err := ctx.Bind(&req); err != nil {
			return echo.NewHTTPError(http.StatusBadRequest, "Invalid request body")
		}
	}
	credentialName, err := deviceAuthCredentialName(user, req)
	if err != nil {
		return err
	}
	if prior, loaded := c.activeAttempt(ctx.Request().Context(), credentialName); loaded {
		if !req.Replace {
			return echo.NewHTTPError(http.StatusConflict, "Codex device auth is already in progress")
		}
		_ = c.cancelAttempt(context.Background(), prior.ID)
	}
	token, tokenHash, err := newDeviceAuthToken()
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "Failed to create device auth token")
	}
	attemptID := "cda-" + strings.ReplaceAll(uuid.NewString(), "-", "")
	attempt := &deviceAuthAttempt{ID: attemptID, UserID: string(user.ID()), CredentialName: credentialName, TokenHash: tokenHash, Status: codexauth.StatusStarting, ExpiresAt: time.Now().UTC().Add(deviceAuthTTL)}
	if c.store != nil {
		if err := c.store.Create(ctx.Request().Context(), durableAttempt(attempt)); err != nil {
			if err == codexauth.ErrAttemptActive {
				return echo.NewHTTPError(http.StatusConflict, "Codex device auth is already in progress")
			}
			return echo.NewHTTPError(http.StatusServiceUnavailable, "Failed to persist Codex device auth attempt")
		}
	}
	c.attempts.Store(attemptID, attempt)
	c.active.Store(credentialName, attemptID)
	c.userLatest.Store(attempt.UserID, attemptID)
	callbackURL, err := deviceAuthCallbackURL(ctx)
	if err != nil {
		c.finishAttempt(attempt, codexauth.StatusFailed)
		return echo.NewHTTPError(http.StatusBadRequest, err.Error())
	}
	workload := codexauth.WorkloadRequest{AttemptID: attemptID, CallbackURL: callbackURL, Token: token, ExpiresAt: attempt.ExpiresAt}
	if err := c.launcher.StartCodexDeviceAuth(ctx.Request().Context(), workload); err != nil {
		c.finishAttempt(attempt, codexauth.StatusFailed)
		return echo.NewHTTPError(http.StatusServiceUnavailable, fmt.Sprintf("Failed to start Codex device auth: %v", err))
	}
	return ctx.JSON(http.StatusAccepted, responseForAttempt(attempt))
}

func (c *CodexDeviceAuthController) GetAttempt(ctx echo.Context) error {
	user := auth.GetUserFromContext(ctx)
	if user == nil {
		return echo.NewHTTPError(http.StatusUnauthorized, "Authentication required")
	}
	attempt, ok := c.loadAttempt(ctx.Request().Context(), ctx.Param("attemptId"))
	if !ok || attempt.UserID != string(user.ID()) {
		return echo.NewHTTPError(http.StatusNotFound, "Codex device auth attempt not found")
	}
	c.expireAttempt(attempt)
	return ctx.JSON(http.StatusOK, responseForAttempt(attempt))
}

func (c *CodexDeviceAuthController) CancelAttempt(ctx echo.Context) error {
	user := auth.GetUserFromContext(ctx)
	if user == nil {
		return echo.NewHTTPError(http.StatusUnauthorized, "Authentication required")
	}
	attempt, ok := c.loadAttempt(ctx.Request().Context(), ctx.Param("attemptId"))
	if !ok || attempt.UserID != string(user.ID()) {
		return echo.NewHTTPError(http.StatusNotFound, "Codex device auth attempt not found")
	}
	if err := c.cancelAttempt(ctx.Request().Context(), attempt.ID); err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
	}
	return ctx.NoContent(http.StatusNoContent)
}

func (c *CodexDeviceAuthController) PollDeviceAuth(ctx echo.Context) error {
	user := auth.GetUserFromContext(ctx)
	if user == nil {
		return echo.NewHTTPError(http.StatusUnauthorized, "Authentication required")
	}
	attempt, ok := c.latestAttempt(ctx.Request().Context(), string(user.ID()))
	if !ok {
		return ctx.JSON(http.StatusOK, PollDeviceAuthResponse{Status: "pending"})
	}
	c.expireAttempt(attempt)
	attempt.mu.RLock()
	status := attempt.Status
	attempt.mu.RUnlock()
	if status == codexauth.StatusStarting || status == codexauth.StatusWaitingForUser {
		status = "pending"
	}
	if status == codexauth.StatusFailed || status == codexauth.StatusCancelled {
		status = "denied"
	}
	return ctx.JSON(http.StatusOK, PollDeviceAuthResponse{Status: status})
}

func (c *CodexDeviceAuthController) ReportChallenge(ctx echo.Context) error {
	attempt, ok := c.authorizeWorker(ctx)
	if !ok {
		return ctx.NoContent(http.StatusUnauthorized)
	}
	var challenge codexauth.Challenge
	if err := bindLimitedJSON(ctx, &challenge); err != nil || challenge.UserCode == "" || !validVerificationURI(challenge.VerificationURI) {
		return echo.NewHTTPError(http.StatusBadRequest, "Invalid device auth challenge")
	}
	attempt.mu.Lock()
	if attempt.Status != codexauth.StatusStarting || time.Now().After(attempt.ExpiresAt) {
		attempt.mu.Unlock()
		return ctx.NoContent(http.StatusConflict)
	}
	attempt.Challenge = challenge
	attempt.Status = codexauth.StatusWaitingForUser
	attempt.mu.Unlock()
	if err := c.persistAttempt(ctx.Request().Context(), attempt); err != nil {
		return echo.NewHTTPError(http.StatusServiceUnavailable, "Failed to persist device auth challenge")
	}
	return ctx.NoContent(http.StatusNoContent)
}

func (c *CodexDeviceAuthController) ReportResult(ctx echo.Context) error {
	attempt, ok := c.authorizeWorker(ctx)
	if !ok {
		return ctx.NoContent(http.StatusUnauthorized)
	}
	var result codexauth.Result
	if err := bindLimitedJSON(ctx, &result); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "Invalid device auth result")
	}
	attempt.mu.RLock()
	current := attempt.Status
	attempt.mu.RUnlock()
	if current != codexauth.StatusStarting && current != codexauth.StatusWaitingForUser {
		return ctx.NoContent(http.StatusConflict)
	}
	if result.Status != codexauth.StatusAuthorized {
		if result.Status != codexauth.StatusDenied {
			result.Status = codexauth.StatusFailed
		}
		c.finishAttempt(attempt, result.Status)
		go func() { _ = c.launcher.CancelCodexDeviceAuth(context.Background(), attempt.ID) }()
		return ctx.NoContent(http.StatusNoContent)
	}
	if len(result.AuthJSON) == 0 || len(result.AuthJSON) > maxAuthJSONBytes || !validAuthJSON(result.AuthJSON) {
		return echo.NewHTTPError(http.StatusBadRequest, "Invalid auth.json")
	}
	creds := entities.NewCredentials(attempt.CredentialName, json.RawMessage(result.AuthJSON))
	creds.SetFileType(sessionsettings.FileTypeCodexAuth)
	if err := c.repo.Save(ctx.Request().Context(), creds); err != nil {
		return echo.NewHTTPError(http.StatusServiceUnavailable, "Failed to save Codex credentials")
	}
	c.finishAttempt(attempt, codexauth.StatusAuthorized)
	go func() { _ = c.launcher.CancelCodexDeviceAuth(context.Background(), attempt.ID) }()
	return ctx.NoContent(http.StatusNoContent)
}

func (c *CodexDeviceAuthController) authorizeWorker(ctx echo.Context) (*deviceAuthAttempt, bool) {
	attempt, ok := c.loadAttempt(ctx.Request().Context(), ctx.Param("attemptId"))
	if !ok {
		return nil, false
	}
	token := strings.TrimPrefix(ctx.Request().Header.Get(echo.HeaderAuthorization), "Bearer ")
	candidate := sha256.Sum256([]byte(token))
	return attempt, token != "" && subtle.ConstantTimeCompare(candidate[:], attempt.TokenHash[:]) == 1
}

func (c *CodexDeviceAuthController) cancelAttempt(ctx context.Context, id string) error {
	attempt, ok := c.loadAttempt(ctx, id)
	if !ok {
		return nil
	}
	c.finishAttempt(attempt, codexauth.StatusCancelled)
	if c.launcher != nil {
		return c.launcher.CancelCodexDeviceAuth(ctx, id)
	}
	return nil
}

func (c *CodexDeviceAuthController) finishAttempt(attempt *deviceAuthAttempt, status string) {
	attempt.mu.Lock()
	attempt.Status = status
	attempt.TokenHash = [sha256.Size]byte{}
	attempt.mu.Unlock()
	c.active.CompareAndDelete(attempt.CredentialName, attempt.ID)
	if c.store != nil {
		_ = c.store.Update(context.Background(), durableAttempt(attempt))
		_ = c.store.Release(context.Background(), durableAttempt(attempt))
	}
}

func (c *CodexDeviceAuthController) expireAttempt(attempt *deviceAuthAttempt) {
	attempt.mu.RLock()
	expired := time.Now().After(attempt.ExpiresAt) && (attempt.Status == codexauth.StatusStarting || attempt.Status == codexauth.StatusWaitingForUser)
	attempt.mu.RUnlock()
	if expired {
		c.finishAttempt(attempt, codexauth.StatusFailed)
	}
}

func (c *CodexDeviceAuthController) loadAttempt(ctx context.Context, id string) (*deviceAuthAttempt, bool) {
	value, ok := c.attempts.Load(id)
	if ok {
		return value.(*deviceAuthAttempt), true
	}
	if c.store != nil {
		stored, err := c.store.Get(ctx, id)
		if err == nil {
			attempt := runtimeAttempt(stored)
			c.attempts.Store(id, attempt)
			return attempt, true
		}
	}
	return nil, false
}

func (c *CodexDeviceAuthController) activeAttempt(ctx context.Context, credentialName string) (*deviceAuthAttempt, bool) {
	if c.store != nil {
		attempt, err := c.store.ActiveByCredential(ctx, credentialName)
		if err == nil {
			return runtimeAttempt(attempt), true
		}
		return nil, false
	}
	value, ok := c.active.Load(credentialName)
	if !ok {
		return nil, false
	}
	return c.loadAttempt(ctx, value.(string))
}

func (c *CodexDeviceAuthController) latestAttempt(ctx context.Context, userID string) (*deviceAuthAttempt, bool) {
	if c.store != nil {
		attempt, err := c.store.LatestByUser(ctx, userID)
		if err == nil {
			return runtimeAttempt(attempt), true
		}
		return nil, false
	}
	id, ok := c.userLatest.Load(userID)
	if !ok {
		return nil, false
	}
	return c.loadAttempt(ctx, id.(string))
}

func (c *CodexDeviceAuthController) persistAttempt(ctx context.Context, attempt *deviceAuthAttempt) error {
	if c.store == nil {
		return nil
	}
	return c.store.Update(ctx, durableAttempt(attempt))
}

func durableAttempt(attempt *deviceAuthAttempt) *codexauth.Attempt {
	attempt.mu.RLock()
	defer attempt.mu.RUnlock()
	return &codexauth.Attempt{ID: attempt.ID, UserID: attempt.UserID, CredentialName: attempt.CredentialName, TokenHash: append([]byte(nil), attempt.TokenHash[:]...), Status: attempt.Status, Challenge: attempt.Challenge, ExpiresAt: attempt.ExpiresAt}
}

func runtimeAttempt(attempt *codexauth.Attempt) *deviceAuthAttempt {
	r := &deviceAuthAttempt{ID: attempt.ID, UserID: attempt.UserID, CredentialName: attempt.CredentialName, Status: attempt.Status, Challenge: attempt.Challenge, ExpiresAt: attempt.ExpiresAt}
	copy(r.TokenHash[:], attempt.TokenHash)
	return r
}

func responseForAttempt(attempt *deviceAuthAttempt) StartDeviceAuthResponse {
	attempt.mu.RLock()
	defer attempt.mu.RUnlock()
	return StartDeviceAuthResponse{AttemptID: attempt.ID, Status: attempt.Status, UserCode: attempt.Challenge.UserCode, VerificationURI: attempt.Challenge.VerificationURI, ExpiresAt: attempt.ExpiresAt, PollAfterMS: 2000}
}

func deviceAuthCredentialName(user *entities.User, req StartDeviceAuthRequest) (string, error) {
	scope, teamID := strings.TrimSpace(req.Scope), strings.TrimSpace(req.TeamID)
	if scope == "" {
		scope = string(entities.ScopeUser)
	}
	switch scope {
	case string(entities.ScopeUser):
		if teamID != "" {
			return "", echo.NewHTTPError(http.StatusBadRequest, "team_id is only valid when scope is 'team'")
		}
		return string(user.ID()), nil
	case string(entities.ScopeTeam):
		if teamID == "" {
			return "", echo.NewHTTPError(http.StatusBadRequest, "team_id is required when scope is 'team'")
		}
		if !user.IsAdmin() && !user.IsMemberOfTeam(teamID) {
			return "", echo.NewHTTPError(http.StatusForbidden, "Access denied: not a member of the specified team")
		}
		return teamID, nil
	default:
		return "", echo.NewHTTPError(http.StatusBadRequest, "scope must be 'user' or 'team'")
	}
}

func newDeviceAuthToken() (string, [sha256.Size]byte, error) {
	var raw [32]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "", [sha256.Size]byte{}, err
	}
	token := base64.RawURLEncoding.EncodeToString(raw[:])
	return token, sha256.Sum256([]byte(token)), nil
}

func deviceAuthCallbackURL(ctx echo.Context) (string, error) {
	scheme := ctx.Request().Header.Get("X-Forwarded-Proto")
	if scheme == "" {
		scheme = ctx.Scheme()
	}
	host := ctx.Request().Header.Get("X-Forwarded-Host")
	if host == "" {
		host = ctx.Request().Host
	}
	if scheme != "http" && scheme != "https" {
		return "", fmt.Errorf("invalid callback scheme")
	}
	if host == "" || strings.ContainsAny(host, "\r\n/") {
		return "", fmt.Errorf("invalid callback host")
	}
	return scheme + "://" + host + "/internal/codex-device-auth", nil
}

func validVerificationURI(raw string) bool {
	u, err := url.Parse(raw)
	return err == nil && u.Scheme == "https" && u.Host != "" && u.User == nil
}

func validAuthJSON(raw []byte) bool {
	var object map[string]json.RawMessage
	return json.Unmarshal(raw, &object) == nil && object != nil
}

func bindLimitedJSON(ctx echo.Context, value any) error {
	ctx.Request().Body = http.MaxBytesReader(ctx.Response(), ctx.Request().Body, maxDeviceAuthBodyBytes)
	return json.NewDecoder(ctx.Request().Body).Decode(value)
}
