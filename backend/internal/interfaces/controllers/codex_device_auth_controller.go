package controllers

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/labstack/echo/v4"
	sessionrunnercore "github.com/takutakahashi/agentapi-proxy/internal/core/sessionrunner"
	"github.com/takutakahashi/agentapi-proxy/internal/domain/entities"
	"github.com/takutakahashi/agentapi-proxy/internal/usecases/ports/repositories"
	"github.com/takutakahashi/agentapi-proxy/pkg/auth"
	"github.com/takutakahashi/agentapi-proxy/pkg/codexauth"
	"github.com/takutakahashi/agentapi-proxy/pkg/sessionsettings"
)

const (
	// Codex device codes stay valid for 15 minutes, so the attempt must not
	// expire earlier than the code the user is asked to enter.
	deviceAuthTTL          = 15 * time.Minute
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
	repo            repositories.CredentialsRepository
	launcher        codexauth.WorkloadLauncher
	store           codexauth.AttemptStore
	callbackBaseURL string
	attempts        sync.Map // attempt ID -> *deviceAuthAttempt
	active          sync.Map // credential name -> attempt ID
	userLatest      sync.Map // user ID -> attempt ID; legacy poll compatibility
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

// WithCallbackBaseURL pins the base URL that Codex device auth workers use to
// report their challenge and result. Workloads that run inside the session
// cluster cannot necessarily reach the browser-facing host (for example when
// the UI is published behind an authentication proxy), so deployments may pin
// a directly reachable API origin instead of relying on the request headers.
func (c *CodexDeviceAuthController) WithCallbackBaseURL(baseURL string) *CodexDeviceAuthController {
	c.callbackBaseURL = strings.TrimSpace(baseURL)
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
	log.Printf("[CODEX_DEVICE_AUTH] Created attempt %s for user=%s credential=%q", attemptID, user.ID(), credentialName)
	if c.store != nil {
		if err := c.store.Create(ctx.Request().Context(), durableAttempt(attempt)); err != nil {
			log.Printf("[CODEX_DEVICE_AUTH] Failed to persist attempt %s: %v", attemptID, err)
			if err == codexauth.ErrAttemptActive {
				return echo.NewHTTPError(http.StatusConflict, "Codex device auth is already in progress")
			}
			return echo.NewHTTPError(http.StatusServiceUnavailable, "Failed to persist Codex device auth attempt")
		}
		log.Printf("[CODEX_DEVICE_AUTH] Persisted attempt %s with status=%s", attemptID, attempt.Status)
	}
	c.attempts.Store(attemptID, attempt)
	c.active.Store(credentialName, attemptID)
	c.userLatest.Store(attempt.UserID, attemptID)
	callbackURL, err := c.deviceAuthCallbackURL(ctx)
	if err != nil {
		log.Printf("[CODEX_DEVICE_AUTH] Failed to build callback URL for attempt %s: %v", attemptID, err)
		c.finishAttempt(attempt, codexauth.StatusFailed)
		return echo.NewHTTPError(http.StatusBadRequest, err.Error())
	}
	subject := deviceAuthSubject(user, req)
	workload := codexauth.WorkloadRequest{
		AttemptID: attemptID, CallbackURL: callbackURL, Token: token, ExpiresAt: attempt.ExpiresAt,
		SubjectType: string(subject.Type), SubjectID: subject.ID,
	}
	log.Printf("[CODEX_DEVICE_AUTH] Launching workload for attempt %s", attemptID)
	if err := c.launcher.StartCodexDeviceAuth(ctx.Request().Context(), workload); err != nil {
		log.Printf("[CODEX_DEVICE_AUTH] Failed to launch workload for attempt %s: %v", attemptID, err)
		c.finishAttempt(attempt, codexauth.StatusFailed)
		return echo.NewHTTPError(http.StatusServiceUnavailable, fmt.Sprintf("Failed to start Codex device auth: %v", err))
	}
	log.Printf("[CODEX_DEVICE_AUTH] Workload accepted for attempt %s", attemptID)
	return ctx.JSON(http.StatusAccepted, responseForAttempt(attempt))
}

func deviceAuthSubject(user *entities.User, req StartDeviceAuthRequest) sessionrunnercore.Subject {
	if strings.TrimSpace(req.Scope) == string(entities.ScopeTeam) {
		return sessionrunnercore.Subject{Type: sessionrunnercore.SubjectTeam, ID: strings.TrimSpace(req.TeamID)}
	}
	return sessionrunnercore.Subject{Type: sessionrunnercore.SubjectUser, ID: string(user.ID())}
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
		log.Printf("[CODEX_DEVICE_AUTH] Rejected unauthorized challenge callback for attempt %s", ctx.Param("attemptId"))
		return ctx.NoContent(http.StatusUnauthorized)
	}
	var challenge codexauth.Challenge
	if err := bindLimitedJSON(ctx, &challenge); err != nil || challenge.UserCode == "" || !validVerificationURI(challenge.VerificationURI) {
		log.Printf("[CODEX_DEVICE_AUTH] Invalid challenge callback for attempt %s: %v", attempt.ID, err)
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
		log.Printf("[CODEX_DEVICE_AUTH] Failed to persist challenge for attempt %s: %v", attempt.ID, err)
		return echo.NewHTTPError(http.StatusServiceUnavailable, "Failed to persist device auth challenge")
	}
	log.Printf("[CODEX_DEVICE_AUTH] Challenge accepted for attempt %s", attempt.ID)
	return ctx.NoContent(http.StatusNoContent)
}

func (c *CodexDeviceAuthController) ReportResult(ctx echo.Context) error {
	attempt, ok := c.authorizeWorker(ctx)
	if !ok {
		log.Printf("[CODEX_DEVICE_AUTH] Rejected unauthorized result callback for attempt %s", ctx.Param("attemptId"))
		return ctx.NoContent(http.StatusUnauthorized)
	}
	var result codexauth.Result
	if err := bindLimitedJSON(ctx, &result); err != nil {
		log.Printf("[CODEX_DEVICE_AUTH] Invalid result callback for attempt %s: %v", attempt.ID, err)
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
		log.Printf("[CODEX_DEVICE_AUTH] Attempt %s finished with status=%s error_code=%q", attempt.ID, result.Status, result.ErrorCode)
		go func() { _ = c.launcher.CancelCodexDeviceAuth(context.Background(), attempt.ID) }()
		return ctx.NoContent(http.StatusNoContent)
	}
	if len(result.AuthJSON) == 0 || len(result.AuthJSON) > maxAuthJSONBytes || !validAuthJSON(result.AuthJSON) {
		return echo.NewHTTPError(http.StatusBadRequest, "Invalid auth.json")
	}
	creds := entities.NewCredentials(attempt.CredentialName, json.RawMessage(result.AuthJSON))
	creds.SetFileType(sessionsettings.FileTypeCodexAuth)
	if err := c.repo.Save(ctx.Request().Context(), creds); err != nil {
		log.Printf("[CODEX_DEVICE_AUTH] Failed to save credentials for attempt %s: %v", attempt.ID, err)
		return echo.NewHTTPError(http.StatusServiceUnavailable, "Failed to save Codex credentials")
	}
	c.finishAttempt(attempt, codexauth.StatusAuthorized)
	log.Printf("[CODEX_DEVICE_AUTH] Attempt %s finished with status=%s", attempt.ID, codexauth.StatusAuthorized)
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
		if err := c.store.Update(context.Background(), durableAttempt(attempt)); err != nil {
			log.Printf("[CODEX_DEVICE_AUTH] Failed to persist final status for attempt %s status=%s: %v", attempt.ID, status, err)
		}
		if err := c.store.Release(context.Background(), durableAttempt(attempt)); err != nil {
			log.Printf("[CODEX_DEVICE_AUTH] Failed to release attempt %s status=%s: %v", attempt.ID, status, err)
		}
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

// loadAttempt resolves an attempt by ID. When a durable store is configured it
// is authoritative: the parent API can serve the same attempt from several
// replicas, and a process-local snapshot may lag behind state written by
// another replica (for example the device challenge reported by the auth
// worker). Trusting the cached copy would leave the UI polling forever in the
// "starting" state, so every read goes back to the store and the cache is only
// used as a fallback when no store is configured.
func (c *CodexDeviceAuthController) loadAttempt(ctx context.Context, id string) (*deviceAuthAttempt, bool) {
	if c.store != nil {
		stored, err := c.store.Get(ctx, id)
		if err != nil {
			return nil, false
		}
		attempt := runtimeAttempt(stored)
		c.attempts.Store(id, attempt)
		return attempt, true
	}
	value, ok := c.attempts.Load(id)
	if !ok {
		return nil, false
	}
	return value.(*deviceAuthAttempt), true
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

// deviceAuthCallbackURL resolves the callback URL advertised to the auth
// worker. An explicitly configured base URL always wins because the request
// derived host may only be reachable from the browser.
func (c *CodexDeviceAuthController) deviceAuthCallbackURL(ctx echo.Context) (string, error) {
	if base := strings.TrimSuffix(c.callbackBaseURL, "/"); base != "" {
		parsed, err := url.Parse(base)
		if err != nil {
			return "", fmt.Errorf("invalid Codex device auth callback base URL")
		}
		httpish := parsed.Scheme == "http" || parsed.Scheme == "https"
		if !httpish || parsed.Host == "" || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" {
			return "", fmt.Errorf("invalid Codex device auth callback base URL")
		}
		return base + "/internal/codex-device-auth", nil
	}
	return requestDerivedCallbackURL(ctx)
}

func requestDerivedCallbackURL(ctx echo.Context) (string, error) {
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
	prefix := strings.TrimSuffix(ctx.Request().Header.Get("X-Forwarded-Prefix"), "/")
	if prefix != "" && (!strings.HasPrefix(prefix, "/") || strings.Contains(prefix, "..") || strings.ContainsAny(prefix, "\r\n?#")) {
		return "", fmt.Errorf("invalid callback prefix")
	}
	return scheme + "://" + host + prefix + "/internal/codex-device-auth", nil
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
