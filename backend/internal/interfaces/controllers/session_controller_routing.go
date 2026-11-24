package controllers

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strings"
	"time"

	"github.com/labstack/echo/v4"
	"github.com/takutakahashi/agentapi-proxy/internal/domain/entities"
	"github.com/takutakahashi/agentapi-proxy/internal/infrastructure/services"
	"github.com/takutakahashi/agentapi-proxy/internal/usecases/ports/repositories"
	"github.com/takutakahashi/agentapi-proxy/pkg/auth"
	"github.com/takutakahashi/agentapi-proxy/pkg/telemetry"
	closeutils "github.com/takutakahashi/agentapi-proxy/pkg/utils"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
)

func (c *SessionController) RouteToSession(ctx echo.Context) error {
	if err := c.checkRestartHold(ctx); err != nil {
		return err
	}
	return telemetry.OperationErr(ctx.Request().Context(), "controllers.SessionController.RouteToSession", func(requestCtx context.Context) error {
		ctx.SetRequest(ctx.Request().WithContext(requestCtx))
		return c.routeToSession(ctx)
	}, telemetry.String("http.request.method", ctx.Request().Method))
}

func (c *SessionController) routeToSession(ctx echo.Context) error {
	sessionID := ctx.Param("sessionId")
	session, handled, err := c.resolveSessionRoute(ctx, sessionID)
	if err != nil || handled {
		return err
	}
	session, handled, err = c.prepareLocalSessionRoute(ctx, sessionID, session)
	if err != nil || handled {
		return err
	}
	return c.serveLocalSessionProxy(ctx, sessionID, session)
}

func (c *SessionController) resolveSessionRoute(ctx echo.Context, sessionID string) (entities.Session, bool, error) {
	if session := c.getSessionManager().GetSession(sessionID); session != nil {
		return session, false, nil
	}
	if c.sessionRouteRepo != nil {
		route, err := c.sessionRouteRepo.Get(ctx.Request().Context(), sessionID)
		if err != nil {
			log.Printf("[ROUTE] Failed to look up session route for %s: %v", sessionID, err)
		} else if route != nil {
			return c.resolvePersistedSessionRoute(ctx, route)
		}
	}
	if handled, err := c.respondForTemplatedSession(ctx, sessionID); handled || err != nil {
		return nil, handled, err
	}
	return nil, false, echo.NewHTTPError(http.StatusNotFound, "Session not found")
}

func (c *SessionController) resolvePersistedSessionRoute(ctx echo.Context, route *repositories.SessionRoute) (entities.Session, bool, error) {
	if route.Status == "templated" {
		authzCtx := auth.GetAuthorizationContext(ctx)
		if authzCtx == nil || !authzCtx.CanAccessResource(route.UserID, route.Scope, route.TeamID) {
			return nil, false, echo.NewHTTPError(http.StatusForbidden, "You don't have permission to access this session")
		}
		return nil, true, ctx.JSON(http.StatusGone, map[string]string{"error": "session_templated", "template_id": route.ContextTemplateID})
	}
	if route.Transport == repositories.SessionRouteTransportDirectRuntime || route.ManagerID != "" || route.RemoteSessionID == "" {
		return nil, true, c.routeToRemoteSession(ctx, route)
	}
	session := c.getSessionManager().GetSession(route.RemoteSessionID)
	if session == nil {
		return nil, false, echo.NewHTTPError(http.StatusNotFound, "Session not found")
	}
	return session, false, nil
}

func (c *SessionController) respondForTemplatedSession(ctx echo.Context, sessionID string) (bool, error) {
	if c.contextTemplateRepo == nil {
		return false, nil
	}
	authzCtx := auth.GetAuthorizationContext(ctx)
	if authzCtx == nil {
		return false, nil
	}
	templates, err := c.contextTemplateRepo.List(ctx.Request().Context(), repositories.SessionContextTemplateFilter{UserID: authzCtx.PersonalScope.UserID, TeamIDs: authzCtx.TeamScope.Teams})
	if err != nil {
		return false, nil
	}
	for _, template := range templates {
		if template.SourceSessionID == sessionID {
			return true, ctx.JSON(http.StatusGone, map[string]string{"error": "session_templated", "template_id": template.ID})
		}
	}
	return false, nil
}

func (c *SessionController) prepareLocalSessionRoute(ctx echo.Context, sessionID string, session entities.Session) (entities.Session, bool, error) {
	if ctx.Request().Method != http.MethodOptions {
		authzCtx := auth.GetAuthorizationContext(ctx)
		if !authzCtx.CanAccessResource(session.UserID(), string(session.Scope()), session.TeamID()) {
			log.Printf("User does not have access to session %s", sessionID)
			return nil, false, echo.NewHTTPError(http.StatusForbidden, "You don't have permission to access this session")
		}
		ensured, handled, err := c.ensureLocalSessionWorkload(ctx, sessionID, session)
		if err != nil || handled {
			return ensured, handled, err
		}
		session = ensured
	}
	if c.sessionRouteRepo != nil {
		route, err := c.sessionRouteRepo.Get(ctx.Request().Context(), sessionID)
		if err != nil {
			return nil, false, echo.NewHTTPError(http.StatusServiceUnavailable, "Failed to resolve session runtime route").SetInternal(err)
		}
		if route != nil && route.Transport == repositories.SessionRouteTransportDirectRuntime {
			return nil, true, c.routeToRemoteSession(ctx, route)
		}
	}
	if manager, ok := c.getSessionManager().(interface{ RuntimeTrafficRequiresOutbound() bool }); ok && manager.RuntimeTrafficRequiresOutbound() {
		return nil, false, echo.NewHTTPError(http.StatusServiceUnavailable, "Session outbound runtime connection is unavailable")
	}
	return session, false, nil
}

func (c *SessionController) ensureLocalSessionWorkload(ctx echo.Context, sessionID string, session entities.Session) (entities.Session, bool, error) {
	if ctx.Request().Method == http.MethodGet && strings.HasSuffix(ctx.Request().URL.Path, "/status") && session.Status() == "suspended" {
		return session, true, ctx.JSON(http.StatusOK, map[string]string{"status": "suspended"})
	}
	ensurer, ok := c.getSessionManager().(repositories.SessionWorkloadEnsurer)
	if !ok {
		return session, false, nil
	}
	ensured, resuming, err := ensurer.EnsureSessionWorkload(ctx.Request().Context(), session.ID())
	if err != nil {
		return nil, false, echo.NewHTTPError(http.StatusServiceUnavailable, "Failed to resume session workload").SetInternal(err)
	}
	if ensured != nil {
		session = ensured
	}
	if !resuming {
		return session, false, nil
	}
	ctx.Response().Header().Set("Retry-After", "2")
	err = ctx.JSON(http.StatusServiceUnavailable, map[string]interface{}{
		"error": map[string]string{"code": "session_resuming", "message": "Session workload is resuming", "session_id": sessionID, "status": "resuming"},
	})
	return session, true, err
}

func (c *SessionController) serveLocalSessionProxy(ctx echo.Context, sessionID string, session entities.Session) error {
	target, err := url.Parse(fmt.Sprintf("http://%s", session.Addr()))
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, fmt.Sprintf("Invalid target URL: %v", err))
	}
	if ctx.Request().Method == http.MethodPost && strings.HasSuffix(ctx.Request().URL.Path, "/message") {
		c.captureFirstMessage(ctx, session)
		c.updateSessionTimestamp(ctx, session)
	}
	proxyContext := localSessionProxyContext{echoContext: ctx, sessionID: sessionID, session: session, target: target}
	proxy := proxyContext.build()
	proxy.ServeHTTP(ctx.Response(), ctx.Request())
	return nil
}

type localSessionProxyContext struct {
	echoContext echo.Context
	sessionID   string
	session     entities.Session
	target      *url.URL
}

func (p localSessionProxyContext) build() *httputil.ReverseProxy {
	proxy := httputil.NewSingleHostReverseProxy(p.target)
	proxy.FlushInterval = time.Millisecond * 100
	proxy.Transport = otelhttp.NewTransport(http.DefaultTransport)
	originalDirector := proxy.Director
	proxy.Director = func(req *http.Request) {
		originalDirector(req)
		rewriteLocalProxyRequest(p.echoContext, req)
	}
	proxy.ModifyResponse = modifyLocalProxyResponse
	proxy.ErrorHandler = func(w http.ResponseWriter, r *http.Request, err error) {
		p.handleError(w, r, err)
	}
	return proxy
}

func rewriteLocalProxyRequest(ctx echo.Context, req *http.Request) {
	pathParts := strings.SplitN(req.URL.Path, "/", 3)
	if len(pathParts) >= 3 {
		req.URL.Path = "/" + pathParts[2]
	} else {
		req.URL.Path = "/"
	}
	originalHost := ctx.Request().Host
	if originalHost == "" {
		originalHost = ctx.Request().Header.Get("Host")
	}
	req.Header.Set("X-Forwarded-Host", originalHost)
	req.Header.Set("X-Forwarded-Proto", "http")
	if req.TLS != nil {
		req.Header.Set("X-Forwarded-Proto", "https")
	}
}

func modifyLocalProxyResponse(resp *http.Response) error {
	resp.Header.Set("Access-Control-Allow-Origin", "*")
	resp.Header.Set("Access-Control-Allow-Methods", "GET, HEAD, PUT, PATCH, POST, DELETE, OPTIONS")
	resp.Header.Set("Access-Control-Allow-Headers", "Origin, Content-Type, Accept, Authorization, X-Requested-With, X-Forwarded-For, X-Forwarded-Proto, X-Forwarded-Host, X-API-Key")
	resp.Header.Set("Access-Control-Allow-Credentials", "true")
	resp.Header.Set("Access-Control-Max-Age", "86400")
	if resp.Header.Get("Content-Type") == "text/event-stream" {
		resp.Header.Set("Cache-Control", "no-cache")
		resp.Header.Set("Connection", "keep-alive")
		resp.Header.Del("Content-Length")
	}
	return nil
}

func (p localSessionProxyContext) handleError(w http.ResponseWriter, r *http.Request, proxyErr error) {
	log.Printf("Proxy error for session %s: %v", p.sessionID, proxyErr)
	if strings.HasSuffix(r.URL.Path, "/status") && p.relayProvisionerError(w) {
		return
	}
	http.Error(w, "Bad Gateway", http.StatusBadGateway)
}

func (p localSessionProxyContext) relayProvisionerError(w http.ResponseWriter) bool {
	ks, ok := p.session.(*services.KubernetesSession)
	if !ok {
		return false
	}
	provisionerURL := fmt.Sprintf("http://%s:%d/status", ks.ServiceDNS(), services.ProvisionerPort)
	response, err := (&http.Client{Timeout: 2 * time.Second}).Get(provisionerURL)
	if err != nil {
		return false
	}
	defer func() { closeutils.SafeCloseResponseBody(response.Body) }()
	if response.StatusCode != http.StatusInternalServerError {
		return false
	}
	body, err := io.ReadAll(response.Body)
	if err != nil {
		log.Printf("Failed to read provisioner error for session %s: %v", p.sessionID, err)
		return false
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusInternalServerError)
	written, writeErr := w.Write(body)
	if writeErr != nil || written != len(body) {
		log.Printf("Failed to relay provisioner error for session %s: wrote %d/%d bytes: %v", p.sessionID, written, len(body), writeErr)
	}
	return true
}

func (c *SessionController) deleteLocalSessionAlias(ctx echo.Context, route *repositories.SessionRoute) error {
	usageDimensions := c.resolveSessionUsageDimensions(ctx.Request().Context(), route.SessionID, "local_alias")
	session := c.getSessionManager().GetSession(route.RemoteSessionID)
	if session == nil {
		// The runtime may already have removed itself (for example via a oneshot
		// Stop hook). Authorize from the persisted route metadata and make DELETE
		// idempotently clean up the stale public alias.
		authzCtx := auth.GetAuthorizationContext(ctx)
		if !authzCtx.CanModifyResource(route.UserID, route.Scope, route.TeamID) {
			return echo.NewHTTPError(http.StatusForbidden, "You don't have permission to delete this session")
		}
		if err := c.sessionRouteRepo.Delete(ctx.Request().Context(), route.SessionID); err != nil {
			log.Printf("Failed to delete stale session alias %s: %v", route.SessionID, err)
			return echo.NewHTTPError(http.StatusInternalServerError, "Failed to delete session alias")
		}
		c.recordSessionTerminated(ctx.Request().Context(), usageDimensions, "stale_local_alias", time.Now())
		c.cleanupRemoteProvisionRequest(ctx.Request().Context(), route.SessionID)
		c.cleanupSessionConfiguration(ctx.Request().Context(), route.SessionID)
		return ctx.JSON(http.StatusOK, map[string]interface{}{
			"message": "Stale session alias removed", "session_id": route.SessionID, "status": "terminated",
		})
	}
	authzCtx := auth.GetAuthorizationContext(ctx)
	if !authzCtx.CanModifyResource(session.UserID(), string(session.Scope()), session.TeamID()) {
		return echo.NewHTTPError(http.StatusForbidden, "You don't have permission to delete this session")
	}
	if err := c.sessionCreator.DeleteSessionByID(route.RemoteSessionID); err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "Failed to delete session")
	}
	if err := c.sessionRouteRepo.Delete(ctx.Request().Context(), route.SessionID); err != nil {
		log.Printf("Failed to delete session alias %s: %v", route.SessionID, err)
	}
	c.recordSessionTerminated(ctx.Request().Context(), usageDimensions, "local_alias", time.Now())
	c.cleanupRemoteProvisionRequest(ctx.Request().Context(), route.SessionID)
	c.cleanupSessionConfiguration(ctx.Request().Context(), route.SessionID)
	return ctx.JSON(http.StatusOK, map[string]interface{}{
		"message": "Session terminated successfully", "session_id": route.SessionID, "status": "terminated",
	})
}

// routeToRemoteSession sends session requests over an outbound connection opened
// by the Session Pod or external session manager. The parent never dials the manager.
func (c *SessionController) routeToRemoteSession(ctx echo.Context, route *repositories.SessionRoute) error {
	return telemetry.OperationErr(ctx.Request().Context(), "controllers.SessionController.routeToRemoteSession", func(requestCtx context.Context) error {
		ctx.SetRequest(ctx.Request().WithContext(requestCtx))
		return c.routeToRemoteSessionRequest(ctx, route)
	}, telemetry.String("session.transport", route.Transport))
}

func (c *SessionController) routeToRemoteSessionRequest(ctx echo.Context, route *repositories.SessionRoute) error {
	routing := remoteSessionRouting{controller: c, echoContext: ctx, route: route, sessionID: ctx.Param("sessionId")}
	handled, err := routing.validate()
	if err != nil || handled {
		return err
	}
	body, err := io.ReadAll(ctx.Request().Body)
	if err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "Failed to read request body")
	}
	ctx.Request().Body = io.NopCloser(bytes.NewReader(body))
	routing.body = body
	routing.suffix = strings.TrimPrefix(ctx.Request().URL.Path, "/"+routing.sessionID)

	if route.Transport == repositories.SessionRouteTransportDirectRuntime {
		return routing.routeDirectRuntime()
	}
	return routing.routeManagerRuntime()
}

type remoteSessionRouting struct {
	controller  *SessionController
	echoContext echo.Context
	route       *repositories.SessionRoute
	sessionID   string
	suffix      string
	body        []byte
}

func (r remoteSessionRouting) validate() (bool, error) {
	if r.route.Transport != repositories.SessionRouteTransportDirectRuntime && (r.route.RemoteSessionID == "" || r.route.ManagerID == "") {
		return false, echo.NewHTTPError(http.StatusServiceUnavailable, "External session manager has not reported a routable session yet")
	}
	isStatus := r.echoContext.Request().Method == http.MethodGet && strings.HasSuffix(r.echoContext.Request().URL.Path, "/status")
	if (r.route.Status == "suspended" || r.route.Status == "resuming") && isStatus {
		return true, r.echoContext.JSON(http.StatusOK, map[string]string{"status": r.route.Status})
	}
	if r.route.Status == "suspended" {
		response, err := r.controller.requestRemoteResume(r.echoContext, r.route)
		if err != nil {
			return false, err
		}
		closeutils.SafeCloseResponseBody(response.Body)
		if response.StatusCode < 200 || response.StatusCode >= 300 {
			return false, echo.NewHTTPError(http.StatusServiceUnavailable, "Failed to resume external session workload")
		}
		return true, sessionResumingResponse(r.echoContext, r.route.SessionID)
	}
	if r.route.Status == "resuming" {
		return true, sessionResumingResponse(r.echoContext, r.route.SessionID)
	}
	return false, r.authorize()
}

func (r remoteSessionRouting) authorize() error {
	if r.echoContext.Request().Method == http.MethodOptions {
		return nil
	}
	authzCtx := auth.GetAuthorizationContext(r.echoContext)
	if authzCtx == nil {
		return echo.NewHTTPError(http.StatusUnauthorized, "Authentication required")
	}
	if r.route.UserID != "" && !authzCtx.CanAccessResource(r.route.UserID, r.route.Scope, r.route.TeamID) {
		return echo.NewHTTPError(http.StatusForbidden, "You don't have permission to access this session")
	}
	return nil
}

func (r remoteSessionRouting) routeDirectRuntime() error {
	ctx := r.echoContext
	if r.controller.esmControlTunnel == nil || !r.controller.esmControlTunnel.IsConnected(ctx.Request().Context(), r.route.SessionID) {
		ctx.Response().Header().Set("Retry-After", "2")
		return echo.NewHTTPError(http.StatusServiceUnavailable, "Session runtime connection is unavailable")
	}
	suffix := r.suffix
	if suffix == "" {
		suffix = "/"
	}
	requestURL := &url.URL{Scheme: "http", Host: "session.local", Path: suffix, RawQuery: ctx.Request().URL.RawQuery}
	request, err := http.NewRequestWithContext(ctx.Request().Context(), ctx.Request().Method, requestURL.String(), bytes.NewReader(r.body))
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "Failed to build direct runtime request")
	}
	request.Header = ctx.Request().Header.Clone()
	telemetry.InjectHTTP(ctx.Request().Context(), request)
	response, err := r.controller.esmControlTunnel.Do(ctx.Request().Context(), r.route.SessionID, r.route.SessionID, r.route.RemoteSessionID, request)
	if err != nil {
		return echo.NewHTTPError(http.StatusBadGateway, err.Error())
	}
	defer func() { closeutils.SafeCloseResponseBody(response.Body) }()
	return r.streamResponse(response, suffix)
}

func (r remoteSessionRouting) routeManagerRuntime() error {
	ctx := r.echoContext
	if r.controller.esmControlTunnel == nil || !r.controller.esmControlTunnel.IsConnected(ctx.Request().Context(), r.route.ManagerID) {
		return echo.NewHTTPError(http.StatusServiceUnavailable, "External session manager outbound control connection is unavailable")
	}
	send := func() (*http.Response, error) { return r.sendManagerRequest() }
	response, err := send()
	if err != nil {
		return err
	}
	if remoteWorkloadSuspended(response) {
		closeutils.SafeCloseResponseBody(response.Body)
		if r.suffix == "/status" && ctx.Request().Method == http.MethodGet {
			if err := r.controller.recordRemoteLifecycleStatus(ctx.Request().Context(), r.route, "suspended"); err != nil {
				log.Printf("[ROUTE] Failed to record suspended status for %s: %v", r.route.SessionID, err)
			}
			return ctx.JSON(http.StatusOK, map[string]string{"status": "suspended"})
		}
		response, err = r.controller.resumeAndRetryRemoteRequest(ctx, r.route, send)
		if err != nil {
			return err
		}
		if response == nil {
			return sessionResumingResponse(ctx, r.route.SessionID)
		}
	}
	defer func() { closeutils.SafeCloseResponseBody(response.Body) }()
	return r.streamResponse(response, r.suffix)
}

func (r remoteSessionRouting) sendManagerRequest() (*http.Response, error) {
	ctx := r.echoContext
	requestURL := &url.URL{Scheme: "http", Host: "esm.local", Path: "/" + r.route.RemoteSessionID + r.suffix, RawQuery: ctx.Request().URL.RawQuery}
	request, err := http.NewRequestWithContext(ctx.Request().Context(), ctx.Request().Method, requestURL.String(), bytes.NewReader(r.body))
	if err != nil {
		return nil, echo.NewHTTPError(http.StatusInternalServerError, "Failed to build outbound ESM request")
	}
	request.Header = ctx.Request().Header.Clone()
	telemetry.InjectHTTP(ctx.Request().Context(), request)
	authzCtx := auth.GetAuthorizationContext(ctx)
	if authzCtx != nil && authzCtx.PersonalScope.UserID != "" {
		request.Header.Set("X-Forwarded-User", authzCtx.PersonalScope.UserID)
	}
	if r.route.TeamID != "" {
		request.Header.Set("X-Forwarded-Team", r.route.TeamID)
	}
	response, err := r.controller.esmControlTunnel.Do(ctx.Request().Context(), r.route.ManagerID, r.route.SessionID, r.route.RemoteSessionID, request)
	if err != nil {
		return nil, echo.NewHTTPError(http.StatusBadGateway, err.Error())
	}
	return response, nil
}

func (r remoteSessionRouting) streamResponse(response *http.Response, suffix string) error {
	if suffix == "/status" && r.echoContext.Request().Method == http.MethodGet {
		r.controller.rememberRemoteSessionStatus(r.echoContext.Request().Context(), r.route, response)
	}
	copyResponseHeaders(r.echoContext.Response().Header(), response.Header)
	return streamTunnelResponse(r.echoContext, response)
}
