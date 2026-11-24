package controllers

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"sort"
	"strings"

	"github.com/labstack/echo/v4"
	"github.com/takutakahashi/agentapi-proxy/internal/domain/entities"
	"github.com/takutakahashi/agentapi-proxy/internal/usecases/ports/repositories"
	"github.com/takutakahashi/agentapi-proxy/pkg/auth"
	"github.com/takutakahashi/agentapi-proxy/pkg/authzscope"
	"github.com/takutakahashi/agentapi-proxy/pkg/executiontoken"
	"github.com/takutakahashi/agentapi-proxy/pkg/telemetry"
	closeutils "github.com/takutakahashi/agentapi-proxy/pkg/utils"
)

func withTriggerCredentialPrincipal(ctx context.Context, claims executiontoken.ExecutionClaims, credentialSource string) context.Context {
	if claims.TriggeredUserID == "" || (credentialSource != "triggered_user" && credentialSource != "github_sender") {
		return ctx
	}
	principal, ok := authzscope.FromContext(ctx)
	if !ok {
		return ctx
	}
	for _, userID := range principal.DelegatedUserIDs {
		if userID == claims.TriggeredUserID {
			return ctx
		}
	}
	principal.DelegatedUserIDs = append(principal.DelegatedUserIDs, claims.TriggeredUserID)
	return authzscope.WithPrincipal(ctx, principal)
}

func (c *SessionController) previewReuseStartSession(ctx echo.Context, startReq entities.StartRequest, ownerUserID string) (string, bool, string, error) {
	if len(startReq.ReuseMatchTags) == 0 || startReq.ReuseMessage == "" {
		return "", false, "", nil
	}
	if c.sessionRouteRepo != nil {
		var routes []*repositories.SessionRoute
		var err error
		if filtered, ok := c.sessionRouteRepo.(repositories.FilteredSessionRouteRepository); ok {
			routes, err = filtered.ListFiltered(ctx.Request().Context(), repositories.SessionRouteFilter{
				UserID: ownerUserID, Scope: string(startReq.Scope), TeamID: startReq.TeamID, Tags: startReq.ReuseMatchTags,
			})
		} else {
			routes, err = c.sessionRouteRepo.List(ctx.Request().Context(), ownerUserID)
		}
		if err != nil {
			return "", false, "", echo.NewHTTPError(http.StatusInternalServerError, "failed to list reusable sessions").SetInternal(err)
		}
		sort.SliceStable(routes, func(i, j int) bool { return routes[i].StartedAt.After(routes[j].StartedAt) })
		for _, route := range routes {
			if route.Transport == repositories.SessionRouteTransportDirectRuntime && route.Scope == string(startReq.Scope) &&
				route.TeamID == startReq.TeamID && (startReq.Scope == entities.ScopeTeam || route.UserID == ownerUserID) &&
				tagsContain(route.Tags, startReq.ReuseMatchTags) && !terminalReuseStatus(route.Status) {
				return route.SessionID, true, route.Status, nil
			}
		}
	}
	for _, existing := range c.getSessionManager().ListSessions(entities.SessionFilter{Tags: startReq.ReuseMatchTags}) {
		status := strings.ToLower(existing.Status())
		if existing.Scope() == startReq.Scope && existing.TeamID() == startReq.TeamID &&
			(startReq.Scope == entities.ScopeTeam || existing.UserID() == ownerUserID) && !terminalReuseStatus(status) && status != "suspended" {
			return existing.ID(), true, existing.Status(), nil
		}
	}
	return "", false, "", nil
}

// reuseStartSession makes tag-based trigger reuse authoritative at /start.
// Direct-runtime prompts are appended to the durable reverse-RPC queue, so the
// request is not lost while a matching session is still connecting.
func (c *SessionController) reuseStartSession(ctx echo.Context, startReq entities.StartRequest, ownerUserID string) (string, bool, error) {
	if len(startReq.ReuseMatchTags) == 0 || startReq.ReuseMessage == "" {
		return "", false, nil
	}
	// Direct runtimes are durable routes and are intentionally absent from the
	// API process's local SessionManager. Search the route repository first.
	if c.sessionRouteRepo != nil {
		var routes []*repositories.SessionRoute
		var err error
		routes, err = telemetry.LoggedOperation(ctx.Request().Context(), "controllers.SessionController.FindReusableRoutes", func(operationCtx context.Context) ([]*repositories.SessionRoute, error) {
			if filtered, ok := c.sessionRouteRepo.(repositories.FilteredSessionRouteRepository); ok {
				return filtered.ListFiltered(operationCtx, repositories.SessionRouteFilter{
					UserID: ownerUserID, Scope: string(startReq.Scope), TeamID: startReq.TeamID, Tags: startReq.ReuseMatchTags,
				})
			}
			return c.sessionRouteRepo.List(operationCtx, ownerUserID)
		}, telemetry.Int64("session.reuse_tag_count", int64(len(startReq.ReuseMatchTags))))
		if err != nil {
			return "", false, echo.NewHTTPError(http.StatusInternalServerError, "failed to list reusable sessions").SetInternal(err)
		}
		sort.SliceStable(routes, func(i, j int) bool { return routes[i].StartedAt.After(routes[j].StartedAt) })
		for _, route := range routes {
			if route.Transport != repositories.SessionRouteTransportDirectRuntime ||
				route.Scope != string(startReq.Scope) || route.TeamID != startReq.TeamID ||
				(startReq.Scope != entities.ScopeTeam && route.UserID != ownerUserID) ||
				!tagsContain(route.Tags, startReq.ReuseMatchTags) || terminalReuseStatus(route.Status) {
				continue
			}
			enqueuer, ok := c.esmControlTunnel.(esmControlEnqueuer)
			if !ok {
				return "", false, echo.NewHTTPError(http.StatusServiceUnavailable, "session runtime queue is unavailable")
			}
			body, _ := json.Marshal(map[string]string{"content": startReq.ReuseMessage, "type": "user"})
			promptPath := "/internal/session-prompt"
			if startReq.StopBeforeReuse && strings.EqualFold(route.Status, "running") {
				promptPath = "/internal/session-interrupt-prompt"
			}
			req, reqErr := http.NewRequestWithContext(ctx.Request().Context(), http.MethodPost, "http://session.local"+promptPath, bytes.NewReader(body))
			if reqErr != nil {
				return "", false, echo.NewHTTPError(http.StatusInternalServerError, "failed to build reusable session message").SetInternal(reqErr)
			}
			req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
			if strings.EqualFold(route.Status, "suspended") {
				resp, resumeErr := c.requestRemoteResume(ctx, route)
				if resumeErr != nil {
					return "", false, resumeErr
				}
				closeutils.SafeCloseResponseBody(resp.Body)
				if resp.StatusCode < 200 || resp.StatusCode >= 300 {
					return "", false, echo.NewHTTPError(http.StatusServiceUnavailable, "failed to resume reusable session")
				}
			}
			commandID, err := telemetry.LoggedOperation(ctx.Request().Context(), "controllers.SessionController.EnqueueReusePrompt", func(operationCtx context.Context) (string, error) {
				return enqueuer.Enqueue(operationCtx, route.SessionID, route.SessionID, route.RemoteSessionID, req)
			}, telemetry.String("session.id", route.SessionID), telemetry.String("session.prompt_path", promptPath))
			if err != nil {
				return "", false, echo.NewHTTPError(http.StatusServiceUnavailable, "failed to queue reusable session prompt").SetInternal(err)
			}
			log.Printf("[SESSION_REUSE] Reused direct runtime %s command_id=%s for tags %v", route.SessionID, commandID, startReq.ReuseMatchTags)
			return route.SessionID, true, nil
		}
	}

	existingSessions, _ := telemetry.LoggedOperation(ctx.Request().Context(), "controllers.SessionController.FindReusableSessions", func(context.Context) ([]entities.Session, error) {
		return c.getSessionManager().ListSessions(entities.SessionFilter{Tags: startReq.ReuseMatchTags}), nil
	}, telemetry.Int64("session.reuse_tag_count", int64(len(startReq.ReuseMatchTags))))
	for _, existing := range existingSessions {
		if existing.Scope() != startReq.Scope || existing.TeamID() != startReq.TeamID || (startReq.Scope != entities.ScopeTeam && existing.UserID() != ownerUserID) {
			continue
		}
		status := strings.ToLower(existing.Status())
		if terminalReuseStatus(status) || status == "suspended" {
			continue
		}
		if startReq.StopBeforeReuse && status == "running" {
			if err := telemetry.LoggedOperationErr(ctx.Request().Context(), "controllers.SessionController.StopReusableSession", func(operationCtx context.Context) error {
				return c.getSessionManager().StopAgent(operationCtx, existing.ID())
			}, telemetry.String("session.id", existing.ID())); err != nil {
				return "", false, echo.NewHTTPError(http.StatusInternalServerError, "failed to interrupt reusable session").SetInternal(err)
			}
		}
		if err := telemetry.LoggedOperationErr(ctx.Request().Context(), "controllers.SessionController.SendReuseMessage", func(operationCtx context.Context) error {
			return c.getSessionManager().SendMessage(operationCtx, existing.ID(), startReq.ReuseMessage)
		}, telemetry.String("session.id", existing.ID())); err != nil {
			return "", false, echo.NewHTTPError(http.StatusInternalServerError, "failed to route reusable session message").SetInternal(err)
		}
		return existing.ID(), true, nil
	}
	return "", false, nil
}

func tagsContain(tags, expected map[string]string) bool {
	for key, value := range expected {
		if tags[key] != value {
			return false
		}
	}
	return true
}

func terminalReuseStatus(status string) bool {
	switch strings.ToLower(status) {
	case "stopped", "failed", "terminated", "terminating":
		return true
	default:
		return false
	}
}

func shouldUseGitHubBroker(startReq entities.StartRequest, repository string) bool {
	if repository == "" {
		return false
	}
	if startReq.Params != nil && startReq.Params.GithubToken != "" {
		return false
	}
	switch startReq.Scope {
	case entities.ScopeUser:
		return true
	case entities.ScopeTeam:
		return startReq.Params == nil || startReq.Params.ConnectionID == ""
	default:
		return false
	}
}

func githubBrokerURL(ctx echo.Context, sessionID, baseURL string) (string, error) {
	endpoint := "/internal/sessions/" + url.PathEscape(sessionID) + "/github-credentials"
	if baseURL = strings.TrimSpace(baseURL); baseURL != "" {
		base, err := url.Parse(baseURL)
		if err != nil || (base.Scheme != "http" && base.Scheme != "https") || base.Hostname() == "" ||
			base.User != nil || strings.ContainsAny(baseURL, "?#") {
			return "", errors.New("github_broker_base_url must be an absolute HTTP(S) URL without userinfo, query, or fragment")
		}
		// The configured base is authoritative; do not append forwarded prefixes
		// from the browser-facing route when sessions use a different endpoint.
		return strings.TrimRight(base.String(), "/") + endpoint, nil
	}
	scheme := strings.TrimSpace(ctx.Request().Header.Get("X-Forwarded-Proto"))
	if scheme == "" {
		if ctx.Request().TLS != nil {
			scheme = "https"
		} else {
			scheme = "http"
		}
	}
	host := strings.TrimSpace(ctx.Request().Header.Get("X-Forwarded-Host"))
	if host == "" {
		host = ctx.Request().Host
	}
	if scheme != "http" && scheme != "https" {
		return "", errors.New("invalid GitHub broker scheme")
	}
	if host == "" || strings.ContainsAny(host, "\r\n/") {
		return "", errors.New("invalid GitHub broker host")
	}
	prefix := strings.TrimSuffix(strings.TrimSpace(ctx.Request().Header.Get("X-Forwarded-Prefix")), "/")
	if prefix != "" && (!strings.HasPrefix(prefix, "/") || strings.Contains(prefix, "..") || strings.ContainsAny(prefix, "\r\n?#")) {
		return "", errors.New("invalid GitHub broker prefix")
	}
	return scheme + "://" + host + prefix + endpoint, nil
}

func populateGitHubTokenFromAuthHeader(ctx echo.Context, startReq *entities.StartRequest) {
	if startReq.Scope == entities.ScopeTeam {
		return
	}
	if startReq.Params == nil {
		startReq.Params = &entities.SessionParams{}
	}
	if startReq.Params.GithubToken != "" {
		return
	}
	if credential, ok := auth.GetGitHubCredentialFromContext(ctx); ok {
		startReq.Params.GithubToken = credential.Token
		startReq.Params.ConnectionID = credential.ConnectionID
	}
}

func (c *SessionController) logSessionTokenRouting(sessionID, source, connectionID, token string) {
	if !c.sessionTokenDebug {
		return
	}
	fingerprint := "none"
	if token != "" {
		sum := sha256.Sum256([]byte(token))
		fingerprint = fmt.Sprintf("%x", sum[:6])
	}
	log.Printf("[SESSION_TOKEN_DEBUG] session_id=%s source=%s connection_id=%q token_fingerprint=%s token_present=%t", sessionID, source, connectionID, fingerprint, token != "")
}

func (c *SessionController) applyGitHubConnectionURLs(ctx context.Context, startReq *entities.StartRequest, connectionID string) error {
	baseURL, apiURL, err := c.githubTokenResolver.ResolveConnectionURLs(ctx, connectionID)
	if err != nil {
		return err
	}
	parsedBaseURL, err := url.Parse(baseURL)
	if err != nil || parsedBaseURL.Host == "" {
		return fmt.Errorf("selected GitHub connection has an invalid base URL")
	}
	if startReq.Environment == nil {
		startReq.Environment = make(map[string]string)
	}
	// These are part of the selected credential's routing context. Overwrite
	// request-provided values so a token is never sent to a different GitHub host.
	startReq.Environment["GITHUB_URL"] = baseURL
	startReq.Environment["GITHUB_API"] = apiURL
	// GH_HOST may already be present in deployment or team settings. Set it even
	// for github.com so gh cannot keep using a deployment-wide Enterprise host.
	startReq.Environment["GH_HOST"] = parsedBaseURL.Host
	return nil
}

func repositoryOwner(repoFullName string) string {
	parts := strings.SplitN(strings.TrimSpace(repoFullName), "/", 2)
	if len(parts) != 2 || strings.TrimSpace(parts[0]) == "" || strings.TrimSpace(parts[1]) == "" {
		return ""
	}
	return strings.ToLower(strings.TrimSpace(parts[0]))
}

func sessionRepository(startReq entities.StartRequest) string {
	if startReq.Params != nil && strings.TrimSpace(startReq.Params.RepoFullName) != "" {
		return startReq.Params.RepoFullName
	}
	return startReq.Tags["repository"]
}

func containsAllocatorSelector(tags map[string]string) bool {
	for key := range tags {
		if strings.HasPrefix(key, "allocator.") {
			return true
		}
	}
	return false
}

func hasRequestedSessionPool(startReq entities.StartRequest) bool {
	return startReq.Params != nil && strings.TrimSpace(startReq.Params.Pool) != ""
}

func removeImplicitAllocatorCapabilities(params *entities.SessionParams, explicitSandbox, explicitDocker bool) {
	if params == nil {
		return
	}
	if !explicitSandbox {
		params.Sandbox = nil
	}
	if !explicitDocker {
		params.Docker = nil
	}
}

// resolveSessionModelOptions returns the ACP model switching candidates exposed
// for a session: the value snapshotted in the session's run request, falling
// back to the session profile it was created from.
func (c *SessionController) resolveSessionModelOptions(ctx context.Context, session entities.Session, cache map[string][]string) []string {
	if provider, ok := session.(sessionModelOptionsProvider); ok {
		if options := provider.ModelOptions(); len(options) > 0 {
			return options
		}
	}
	return c.sessionModelOptionsFromTags(ctx, session.Tags(), cache)
}

// sessionModelOptionsFromTags resolves model switching candidates from the
// session profile referenced by a session's tags. Sessions handled by an
// external session manager no longer carry the original run request, so the
// profile is the durable source of truth for the candidates.
func (c *SessionController) sessionModelOptionsFromTags(ctx context.Context, tags map[string]string, cache map[string][]string) []string {
	if c.sessionProfileRepo == nil {
		return nil
	}
	profileID := strings.TrimSpace(tags["session_profile_id"])
	if profileID == "" {
		return nil
	}
	if cached, ok := cache[profileID]; ok {
		return cached
	}
	var options []string
	profile, err := c.sessionProfileRepo.Get(ctx, profileID)
	if err != nil {
		log.Printf("[SESSION] Warning: could not resolve model options from session profile %q: %v", profileID, err)
	} else if profile != nil {
		cfg := profile.Config()
		if params := cfg.Params(); params != nil {
			options = append([]string(nil), params.ModelOptions...)
		}
	}
	if cache != nil {
		cache[profileID] = options
	}
	return options
}

// SearchSessions handles GET /search requests to list and filter active sessions
