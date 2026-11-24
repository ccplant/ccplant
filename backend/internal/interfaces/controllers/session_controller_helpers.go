package controllers

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log"
	"net/http"
	"path/filepath"
	"time"

	"github.com/labstack/echo/v4"
	"github.com/takutakahashi/agentapi-proxy/internal/domain/entities"
	"github.com/takutakahashi/agentapi-proxy/internal/usecases/ports/repositories"
)

func copyResponseHeaders(target, source http.Header) {
	for key, values := range source {
		for _, value := range values {
			target.Add(key, value)
		}
	}
}

func streamTunnelResponse(ctx echo.Context, resp *http.Response) error {
	ctx.Response().WriteHeader(resp.StatusCode)
	// The first tunnel frame already contains the upstream status and headers.
	// Flush them before waiting for a body frame so idle SSE streams transition
	// the browser's EventSource from CONNECTING to OPEN immediately.
	ctx.Response().Flush()
	buffer := make([]byte, 64*1024)
	for {
		n, err := resp.Body.Read(buffer)
		if n > 0 {
			if _, writeErr := ctx.Response().Write(buffer[:n]); writeErr != nil {
				return writeErr
			}
			ctx.Response().Flush()
		}
		if err != nil {
			if err == io.EOF {
				return nil
			}
			return err
		}
	}
}

func (c *SessionController) cleanupRemoteProvisionRequest(ctx context.Context, sessionID string) {
	if cleaner, ok := c.sessionCreator.(interface {
		DeleteProvisionRequest(context.Context, string) error
	}); ok {
		if err := cleaner.DeleteProvisionRequest(ctx, sessionID); err != nil {
			log.Printf("[REMOTE_DELETE] Warning: failed to delete provision request for %s: %v", sessionID, err)
		}
	}
	if cleaner, ok := c.sessionCreator.(interface {
		DeleteSessionPoolAllocation(context.Context, string) error
	}); ok {
		if err := cleaner.DeleteSessionPoolAllocation(ctx, sessionID); err != nil {
			log.Printf("[REMOTE_DELETE] Warning: failed to delete session pool allocation for %s: %v", sessionID, err)
		}
	}
}

func (c *SessionController) cleanupSessionConfiguration(ctx context.Context, sessionID string) {
	store, ok := c.sessionRunnerStore.(sessionConfigurationDeleter)
	if !ok {
		return
	}
	if err := store.DeleteConfiguration(ctx, sessionID); err != nil {
		log.Printf("Warning: failed to delete session configuration for %s: %v", sessionID, err)
	}
}

// captureFirstMessage captures the first message content for session description
func (c *SessionController) captureFirstMessage(ctx echo.Context, session entities.Session) {
	// Skip if description already exists
	if session.Tags() != nil {
		if _, exists := session.Tags()["description"]; exists {
			return
		}
	}

	body, err := io.ReadAll(ctx.Request().Body)
	if err != nil {
		return
	}

	// Restore the request body for further processing
	ctx.Request().Body = io.NopCloser(bytes.NewBuffer(body))

	var messageReq map[string]interface{}
	if err := json.Unmarshal(body, &messageReq); err != nil {
		return
	}
}

// updateSessionTimestamp updates the session's updated_at and last_message_at timestamps.
// Called on every POST /message request routed through the proxy.
func (c *SessionController) updateSessionTimestamp(ctx echo.Context, session entities.Session) {
	toucher, ok := c.getSessionManager().(repositories.SessionToucher)
	if !ok {
		return
	}
	now := time.Now()
	go func() {
		updateCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := toucher.TouchSession(updateCtx, session.ID(), now); err != nil {
			log.Printf("[SESSION] Failed to update activity timestamp for session %s: %v", session.ID(), err)
		}
	}()
}

// filterHiddenSessions removes sessions tagged with hidden=true from the list.
func filterHiddenSessions(sessions []entities.Session) []entities.Session {
	result := make([]entities.Session, 0, len(sessions))
	for _, s := range sessions {
		if s.Tags()["hidden"] != "true" {
			result = append(result, s)
		}
	}
	return result
}

// setCORSHeaders sets CORS headers for all session management endpoints
func (c *SessionController) setCORSHeaders(ctx echo.Context) {
	ctx.Response().Header().Set("Access-Control-Allow-Origin", "*")
	ctx.Response().Header().Set("Access-Control-Allow-Methods", "GET, HEAD, PUT, PATCH, POST, DELETE, OPTIONS")
	ctx.Response().Header().Set("Access-Control-Allow-Headers", "Origin, Content-Type, Accept, Authorization, X-Requested-With, X-Forwarded-For, X-Forwarded-Proto, X-Forwarded-Host, X-API-Key")
	ctx.Response().Header().Set("Access-Control-Allow-Credentials", "true")
	ctx.Response().Header().Set("Access-Control-Max-Age", "86400")
}

// mergeSessionParams merges base (profile) params with override (request) params.
// For each field: if the override field is the zero value, the base value is used.
func mergeSessionParams(base, override *entities.SessionParams) *entities.SessionParams {
	merged := *base // start from profile defaults
	if override.Pool != "" {
		merged.Pool = override.Pool
	}
	if override.ResumeFrom != "" {
		merged.ResumeFrom = override.ResumeFrom
	}
	if override.Message != "" {
		merged.Message = override.Message
	}
	if override.GithubToken != "" {
		merged.GithubToken = override.GithubToken
	}
	if override.ConnectionID != "" {
		merged.ConnectionID = override.ConnectionID
	}
	if override.AgentType != "" {
		merged.AgentType = override.AgentType
	}
	if override.Model != "" {
		merged.Model = override.Model
	}
	if len(override.ModelOptions) > 0 {
		merged.ModelOptions = append([]string(nil), override.ModelOptions...)
	}
	if override.Slack != nil {
		merged.Slack = override.Slack
	}
	if override.Oneshot {
		merged.Oneshot = override.Oneshot
	}
	if override.InitialMessageWaitSecond != nil {
		merged.InitialMessageWaitSecond = override.InitialMessageWaitSecond
	}
	if override.ManagerID != "" {
		merged.ManagerID = override.ManagerID
	}
	if override.CycleMessage != "" {
		merged.CycleMessage = override.CycleMessage
	}
	if override.CycleMaxCount != 0 {
		merged.CycleMaxCount = override.CycleMaxCount
	}
	if override.RepoFullName != "" {
		merged.RepoFullName = override.RepoFullName
	}
	if override.Sandbox != nil {
		merged.Sandbox = override.Sandbox
	}
	if override.Docker != nil {
		merged.Docker = override.Docker
	}
	if override.AuthProxy != nil {
		merged.AuthProxy = override.AuthProxy
	}
	if override.SessionTTL != "" {
		merged.SessionTTL = override.SessionTTL
	}
	if len(override.UnsyncedFilePaths) > 0 {
		merged.UnsyncedFilePaths = append([]string(nil), override.UnsyncedFilePaths...)
	}
	if override.CodexAuthMode != "" {
		merged.CodexAuthMode = override.CodexAuthMode
	}
	if override.ClaudeAuthMode != "" {
		merged.ClaudeAuthMode = override.ClaudeAuthMode
	}
	if override.CredentialSource != "" {
		merged.CredentialSource = override.CredentialSource
	}
	merged.Storages = mergeStorageAttachments(base.Storages, override.Storages)
	return &merged
}

// mergeStorageAttachments keeps profile ordering, replaces matching mount paths
// with explicit request entries, and appends request-only entries.
func mergeStorageAttachments(base, override []entities.StorageAttachment) []entities.StorageAttachment {
	if len(override) == 0 {
		return append([]entities.StorageAttachment(nil), base...)
	}
	overrides := make(map[string]entities.StorageAttachment, len(override))
	for _, attachment := range override {
		overrides[filepath.Clean(attachment.MountPath)] = attachment
	}
	result := make([]entities.StorageAttachment, 0, len(base)+len(override))
	seen := make(map[string]struct{}, len(base)+len(override))
	for _, attachment := range base {
		key := filepath.Clean(attachment.MountPath)
		if replacement, ok := overrides[key]; ok {
			attachment = replacement
		}
		result = append(result, attachment)
		seen[key] = struct{}{}
	}
	for _, attachment := range override {
		key := filepath.Clean(attachment.MountPath)
		if _, ok := seen[key]; ok {
			continue
		}
		result = append(result, attachment)
		seen[key] = struct{}{}
	}
	return result
}

func applyProfileSandboxDefaults(cfg entities.SessionProfileConfig, params *entities.SessionParams) {
	if params == nil {
		return
	}
	if cfg.SandboxPolicyID() != "" {
		if params.Sandbox == nil {
			params.Sandbox = &entities.SandboxParams{Enabled: true, PolicyID: cfg.SandboxPolicyID()}
		} else if params.Sandbox.PolicyID == "" {
			params.Sandbox.Enabled = true
			params.Sandbox.PolicyID = cfg.SandboxPolicyID()
		}
		return
	}
	if params.Sandbox == nil {
		params.Sandbox = &entities.SandboxParams{Enabled: true, CountMode: true}
	} else if params.Sandbox.PolicyID == "" {
		params.Sandbox.Enabled = true
		params.Sandbox.CountMode = true
	}
}

// SandboxDomainsResponse is the JSON body returned by GET /sessions/:sessionId/sandbox-domains.
