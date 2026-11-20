package controllers

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/labstack/echo/v4"
	"github.com/takutakahashi/agentapi-proxy/internal/domain/entities"
	"github.com/takutakahashi/agentapi-proxy/internal/usecases/ports/repositories"
	"github.com/takutakahashi/agentapi-proxy/pkg/auth"
	"github.com/takutakahashi/agentapi-proxy/pkg/codexauth"
	"github.com/takutakahashi/agentapi-proxy/pkg/sessionsettings"
)

const antigravityAuthTTL = 15 * time.Minute
const maxAntigravityAuthBundleBytes = 768 << 10
const failedAntigravityWorkloadRetention = 10 * time.Minute

type AntigravityAuthController struct{ *CodexDeviceAuthController }

type AntigravityAuthResponse struct {
	AttemptID       string    `json:"attempt_id"`
	Status          string    `json:"status"`
	VerificationURI string    `json:"verification_uri,omitempty"`
	Transcript      string    `json:"transcript,omitempty"`
	ExpiresAt       time.Time `json:"expires_at"`
	PollAfterMS     int       `json:"poll_after_ms"`
	ErrorCode       string    `json:"error_code,omitempty"`
	ErrorMessage    string    `json:"error_message,omitempty"`
}

type antigravityAuthInputRequest struct {
	Input  string `json:"input"`
	Action string `json:"action"`
}
type antigravityAuthInputResponse struct {
	Input   string `json:"input"`
	Version int64  `json:"version"`
}

func NewAntigravityAuthController(repo repositories.CredentialsRepository, launcher codexauth.WorkloadLauncher) *AntigravityAuthController {
	return &AntigravityAuthController{CodexDeviceAuthController: NewCodexDeviceAuthController(repo, launcher)}
}

func (c *AntigravityAuthController) GetName() string { return "AntigravityAuthController" }

func (c *AntigravityAuthController) StartSSO(ctx echo.Context) error {
	user := auth.GetUserFromContext(ctx)
	if user == nil {
		return echo.NewHTTPError(http.StatusUnauthorized, "Authentication required")
	}
	if c.launcher == nil {
		return echo.NewHTTPError(http.StatusServiceUnavailable, "Antigravity authentication workload is unavailable")
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
	if prior, ok := c.activeAttempt(ctx.Request().Context(), credentialName); ok {
		if !req.Replace {
			return echo.NewHTTPError(http.StatusConflict, "Antigravity authentication is already in progress")
		}
		_ = c.cancelAttempt(context.Background(), prior.ID)
	}
	token, tokenHash, err := newDeviceAuthToken()
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "Failed to create authentication token")
	}
	attempt := &deviceAuthAttempt{ID: "aga-" + strings.ReplaceAll(uuid.NewString(), "-", ""), UserID: string(user.ID()), CredentialName: credentialName, TokenHash: tokenHash, Status: codexauth.StatusStarting, ExpiresAt: time.Now().UTC().Add(antigravityAuthTTL)}
	if c.store != nil {
		if err := c.store.Create(ctx.Request().Context(), durableAttempt(attempt)); err != nil {
			if err == codexauth.ErrAttemptActive {
				return echo.NewHTTPError(http.StatusConflict, "Antigravity authentication is already in progress")
			}
			return echo.NewHTTPError(http.StatusServiceUnavailable, "Failed to persist Antigravity authentication attempt")
		}
	}
	c.attempts.Store(attempt.ID, attempt)
	c.active.Store(credentialName, attempt.ID)
	callbackURL, err := c.callbackURL()
	if err != nil {
		c.finishAttempt(attempt, codexauth.StatusFailed)
		return echo.NewHTTPError(http.StatusServiceUnavailable, err.Error())
	}
	subject := deviceAuthSubject(user, req)
	workload := codexauth.WorkloadRequest{AttemptID: attempt.ID, CallbackURL: callbackURL, Token: token, ExpiresAt: attempt.ExpiresAt, SubjectType: string(subject.Type), SubjectID: subject.ID, ManagerID: strings.TrimSpace(req.ManagerID), Provider: "antigravity"}
	if err := c.launcher.StartCodexDeviceAuth(ctx.Request().Context(), workload); err != nil {
		c.finishAttempt(attempt, codexauth.StatusFailed)
		return echo.NewHTTPError(http.StatusServiceUnavailable, fmt.Sprintf("Failed to start Antigravity authentication: %v", err))
	}
	return ctx.JSON(http.StatusAccepted, antigravityResponse(attempt))
}

func (c *AntigravityAuthController) GetAttempt(ctx echo.Context) error {
	user := auth.GetUserFromContext(ctx)
	if user == nil {
		return echo.NewHTTPError(http.StatusUnauthorized, "Authentication required")
	}
	attempt, ok := c.loadAttempt(ctx.Request().Context(), ctx.Param("attemptId"))
	if !ok || attempt.UserID != string(user.ID()) {
		return echo.NewHTTPError(http.StatusNotFound, "Antigravity authentication attempt not found")
	}
	c.expireAttempt(attempt)
	return ctx.JSON(http.StatusOK, antigravityResponse(attempt))
}

func (c *AntigravityAuthController) SubmitInput(ctx echo.Context) error {
	user := auth.GetUserFromContext(ctx)
	if user == nil {
		return echo.NewHTTPError(http.StatusUnauthorized, "Authentication required")
	}
	attempt, ok := c.loadAttempt(ctx.Request().Context(), ctx.Param("attemptId"))
	if !ok || attempt.UserID != string(user.ID()) {
		return echo.NewHTTPError(http.StatusNotFound, "Antigravity authentication attempt not found")
	}
	var request antigravityAuthInputRequest
	if err := bindLimitedJSON(ctx, &request); err != nil || !validAntigravityInput(request) {
		return echo.NewHTTPError(http.StatusBadRequest, "Input is required")
	}
	attempt.mu.Lock()
	if attempt.Status != codexauth.StatusWaitingForUser {
		attempt.mu.Unlock()
		return ctx.NoContent(http.StatusConflict)
	}
	if request.Action != "" {
		attempt.Input = antigravityInputActionPrefix + request.Action
	} else {
		attempt.Input = strings.TrimSpace(request.Input)
	}
	attempt.InputVersion++
	attempt.mu.Unlock()
	if err := c.persistAttempt(ctx.Request().Context(), attempt); err != nil {
		return echo.NewHTTPError(http.StatusServiceUnavailable, "Failed to submit authentication input")
	}
	return ctx.NoContent(http.StatusNoContent)
}

const antigravityInputActionPrefix = "\x00key:"

func validAntigravityInput(request antigravityAuthInputRequest) bool {
	if request.Action != "" {
		return strings.TrimSpace(request.Input) == "" && (request.Action == "up" || request.Action == "down" || request.Action == "enter")
	}
	return strings.TrimSpace(request.Input) != "" && len(request.Input) <= 8192
}

func (c *AntigravityAuthController) WorkerInput(ctx echo.Context) error {
	attempt, ok := c.authorizeWorker(ctx)
	if !ok {
		return ctx.NoContent(http.StatusUnauthorized)
	}
	attempt.mu.RLock()
	response := antigravityAuthInputResponse{Input: attempt.Input, Version: attempt.InputVersion}
	attempt.mu.RUnlock()
	return ctx.JSON(http.StatusOK, response)
}

func (c *AntigravityAuthController) ReportChallenge(ctx echo.Context) error {
	attempt, ok := c.authorizeWorker(ctx)
	if !ok {
		return ctx.NoContent(http.StatusUnauthorized)
	}
	var challenge codexauth.Challenge
	if err := bindLimitedJSON(ctx, &challenge); err != nil || !validAntigravityAuthURI(challenge.VerificationURI) || len(challenge.Transcript) > 16<<10 {
		return echo.NewHTTPError(http.StatusBadRequest, "Invalid Antigravity authentication challenge")
	}
	attempt.mu.Lock()
	if (attempt.Status != codexauth.StatusStarting && attempt.Status != codexauth.StatusWaitingForUser) || time.Now().After(attempt.ExpiresAt) {
		attempt.mu.Unlock()
		return ctx.NoContent(http.StatusConflict)
	}
	attempt.Challenge = challenge
	attempt.Status = codexauth.StatusWaitingForUser
	attempt.mu.Unlock()
	if err := c.persistAttempt(ctx.Request().Context(), attempt); err != nil {
		return echo.NewHTTPError(http.StatusServiceUnavailable, "Failed to persist Antigravity authentication challenge")
	}
	return ctx.NoContent(http.StatusNoContent)
}

func (c *AntigravityAuthController) ReportResult(ctx echo.Context) error {
	attempt, ok := c.authorizeWorker(ctx)
	if !ok {
		return ctx.NoContent(http.StatusUnauthorized)
	}
	var result codexauth.Result
	if err := bindAntigravityResultJSON(ctx, &result); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "Invalid Antigravity authentication result")
	}
	if result.Status != codexauth.StatusAuthorized {
		if result.Status != codexauth.StatusDenied {
			result.Status = codexauth.StatusFailed
		}
		c.finishAttemptWithError(attempt, result.Status, result.ErrorCode, result.ErrorMessage)
		log.Printf("[ANTIGRAVITY_AUTH] Attempt %s finished with status=%s error_code=%q error=%q", attempt.ID, result.Status, result.ErrorCode, result.ErrorMessage)
		if result.Status == codexauth.StatusFailed {
			time.AfterFunc(failedAntigravityWorkloadRetention, func() {
				if err := c.launcher.CancelCodexDeviceAuth(context.Background(), attempt.ID); err != nil {
					log.Printf("[ANTIGRAVITY_AUTH] Failed to clean retained workload for attempt %s: %v", attempt.ID, err)
				}
			})
		} else {
			go func() { _ = c.launcher.CancelCodexDeviceAuth(context.Background(), attempt.ID) }()
		}
		return ctx.NoContent(http.StatusNoContent)
	}
	if len(result.AuthJSON) == 0 || len(result.AuthJSON) > maxAntigravityAuthBundleBytes || !validAntigravityBundle(result.AuthJSON) {
		return echo.NewHTTPError(http.StatusBadRequest, "Invalid Antigravity authentication bundle")
	}
	creds := entities.NewCredentials(attempt.CredentialName, json.RawMessage(result.AuthJSON))
	creds.SetFileType(sessionsettings.FileTypeAntigravityAuth)
	if err := c.repo.Save(ctx.Request().Context(), creds); err != nil {
		return echo.NewHTTPError(http.StatusServiceUnavailable, "Failed to save Antigravity credentials")
	}
	c.finishAttempt(attempt, codexauth.StatusAuthorized)
	log.Printf("[ANTIGRAVITY_AUTH] Attempt %s finished with status=%s", attempt.ID, codexauth.StatusAuthorized)
	go func() { _ = c.launcher.CancelCodexDeviceAuth(context.Background(), attempt.ID) }()
	return ctx.NoContent(http.StatusNoContent)
}

func (c *AntigravityAuthController) callbackURL() (string, error) {
	base := strings.TrimSuffix(c.callbackBaseURL, "/")
	u, err := url.Parse(base)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return "", errors.New("Antigravity authentication callback base URL is required")
	}
	return base + "/internal/antigravity-auth", nil
}

func antigravityResponse(attempt *deviceAuthAttempt) AntigravityAuthResponse {
	attempt.mu.RLock()
	defer attempt.mu.RUnlock()
	return AntigravityAuthResponse{AttemptID: attempt.ID, Status: attempt.Status, VerificationURI: attempt.Challenge.VerificationURI, Transcript: attempt.Challenge.Transcript, ExpiresAt: attempt.ExpiresAt, PollAfterMS: 1000, ErrorCode: attempt.ErrorCode, ErrorMessage: attempt.ErrorMessage}
}

func validAntigravityAuthURI(raw string) bool { return validVerificationURI(raw) }
func validAntigravityBundle(raw []byte) bool {
	var bundle struct {
		Version int `json:"version"`
		Files   []struct {
			Path    string `json:"path"`
			Content string `json:"content_base64"`
		} `json:"files"`
	}
	if json.Unmarshal(raw, &bundle) != nil || bundle.Version != 1 || len(bundle.Files) == 0 {
		return false
	}
	for _, file := range bundle.Files {
		if file.Path == "" || strings.HasPrefix(file.Path, "/") || strings.Contains(file.Path, "..") || file.Content == "" {
			return false
		}
		if _, err := base64.StdEncoding.DecodeString(file.Content); err != nil {
			return false
		}
	}
	return true
}

func bindAntigravityResultJSON(ctx echo.Context, value any) error {
	// The portable auth bundle contains base64-encoded token files and can be
	// larger than the small interactive request limit used by device auth.
	ctx.Request().Body = http.MaxBytesReader(ctx.Response(), ctx.Request().Body, maxAntigravityAuthBundleBytes+64<<10)
	return json.NewDecoder(ctx.Request().Body).Decode(value)
}
