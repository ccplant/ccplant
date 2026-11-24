package controllers

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/labstack/echo/v4"
	"github.com/takutakahashi/agentapi-proxy/internal/domain/entities"
	"github.com/takutakahashi/agentapi-proxy/pkg/codexauth"
)

func (c *CodexDeviceAuthController) finishAttemptWithError(attempt *deviceAuthAttempt, status, code, message string) {
	attempt.mu.Lock()
	attempt.ErrorCode = strings.TrimSpace(code)
	attempt.ErrorMessage = strings.TrimSpace(message)
	attempt.mu.Unlock()
	c.finishAttempt(attempt, status)
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
	return &codexauth.Attempt{ID: attempt.ID, UserID: attempt.UserID, CredentialName: attempt.CredentialName, TokenHash: append([]byte(nil), attempt.TokenHash[:]...), Status: attempt.Status, Challenge: attempt.Challenge, ExpiresAt: attempt.ExpiresAt, Input: attempt.Input, InputVersion: attempt.InputVersion, ErrorCode: attempt.ErrorCode, ErrorMessage: attempt.ErrorMessage}
}

func runtimeAttempt(attempt *codexauth.Attempt) *deviceAuthAttempt {
	r := &deviceAuthAttempt{ID: attempt.ID, UserID: attempt.UserID, CredentialName: attempt.CredentialName, Status: attempt.Status, Challenge: attempt.Challenge, ExpiresAt: attempt.ExpiresAt, Input: attempt.Input, InputVersion: attempt.InputVersion, ErrorCode: attempt.ErrorCode, ErrorMessage: attempt.ErrorMessage}
	copy(r.TokenHash[:], attempt.TokenHash)
	return r
}

func responseForAttempt(attempt *deviceAuthAttempt) StartDeviceAuthResponse {
	attempt.mu.RLock()
	defer attempt.mu.RUnlock()
	return StartDeviceAuthResponse{AttemptID: attempt.ID, Status: attempt.Status, UserCode: attempt.Challenge.UserCode, VerificationURI: attempt.Challenge.VerificationURI, ExpiresAt: attempt.ExpiresAt, PollAfterMS: 2000, ErrorCode: attempt.ErrorCode, ErrorMessage: attempt.ErrorMessage}
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
// worker. The callback is security-sensitive because it receives credentials,
// so it must only be derived from trusted server-side configuration.
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
	return "", fmt.Errorf("device auth callback base URL is required")
}

// requestDerivedCallbackURL remains as a parser for compatibility checks. It
// is deliberately not used to select the credential callback destination.
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
