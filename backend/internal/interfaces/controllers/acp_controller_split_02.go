package controllers

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"

	"github.com/labstack/echo/v4"
	portrepos "github.com/takutakahashi/agentapi-proxy/internal/usecases/ports/repositories"
	"github.com/takutakahashi/agentapi-proxy/pkg/auth"
	closeutils "github.com/takutakahashi/agentapi-proxy/pkg/utils"
)

func (c *ACPController) handleSessionLoad(ctx echo.Context, req acpRequest) error {
	var params struct {
		SessionId string `json:"sessionId"`
	}
	if len(req.Params) > 0 {
		if err := json.Unmarshal(req.Params, &params); err != nil {
			return ctx.JSON(http.StatusOK, acpErrResp(req.ID, -32602, "invalid params: "+err.Error()))
		}
	}
	if params.SessionId == "" {
		return ctx.JSON(http.StatusOK, acpErrResp(req.ID, -32602, "sessionId is required"))
	}

	authzCtx := auth.GetAuthorizationContext(ctx)
	session := c.sessionManagerProvider.GetSessionManager().GetSession(params.SessionId)
	if session == nil {
		return ctx.JSON(http.StatusOK, acpErrResp(req.ID, -32602, "session not found"))
	}
	if !authzCtx.CanAccessResource(session.UserID(), string(session.Scope()), session.TeamID()) {
		return ctx.JSON(http.StatusOK, acpErrResp(req.ID, -32603, "permission denied"))
	}

	log.Printf("[ACP] session/load sessionId=%s (history replay via GET /acp/sse)", params.SessionId)
	return ctx.JSON(http.StatusOK, acpSuccessResp(req.ID, struct{}{}))
}

// ----------------------------------------------------------------------------
// session/prompt and session/cancel → ACP bridge /rpc
// ----------------------------------------------------------------------------

// getBridgeSessionID returns the bridge-internal session ID for the given proxy session ID
// and bridge address. Results are cached so subsequent calls are free.
func (c *ACPController) getBridgeSessionID(addr, proxySessionID string) (string, error) {
	if cached, ok := c.bridgeSessionCache.Load(proxySessionID); ok {
		return cached.(string), nil
	}

	resp, err := http.Get("http://" + addr + "/session") //nolint:gosec
	if err != nil {
		return "", fmt.Errorf("GET /session failed: %w", err)
	}
	defer func() { closeutils.SafeCloseResponseBody(resp.Body) }()

	var bridgeSession struct {
		SessionId string `json:"sessionId"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&bridgeSession); err != nil {
		return "", fmt.Errorf("decode /session response: %w", err)
	}
	if bridgeSession.SessionId != "" {
		c.bridgeSessionCache.Store(proxySessionID, bridgeSession.SessionId)
	}
	return bridgeSession.SessionId, nil
}

// proxyToBridge forwards session/prompt, session/cancel, and session/set_config_option to the ACP bridge's
// /rpc endpoint. Requires the session to run with an ACP-native agent (e.g. claude-acp).
func (c *ACPController) proxyToBridge(ctx echo.Context, req acpRequest) error {
	var baseParams struct {
		SessionId string `json:"sessionId"`
	}
	if len(req.Params) > 0 {
		if err := json.Unmarshal(req.Params, &baseParams); err != nil {
			return ctx.JSON(http.StatusOK, acpErrResp(req.ID, -32602, "invalid params: "+err.Error()))
		}
	}
	if baseParams.SessionId == "" {
		return ctx.JSON(http.StatusOK, acpErrResp(req.ID, -32602, "sessionId is required"))
	}
	if route, routeErr := c.directRuntimeRoute(ctx.Request().Context(), baseParams.SessionId); routeErr != nil {
		return ctx.JSON(http.StatusOK, acpErrResp(req.ID, -32603, "session route lookup failed"))
	} else if route != nil {
		authzCtx := auth.GetAuthorizationContext(ctx)
		if authzCtx == nil || !authzCtx.CanAccessResource(route.UserID, route.Scope, route.TeamID) {
			return ctx.JSON(http.StatusOK, acpErrResp(req.ID, -32603, "permission denied"))
		}
		return c.proxyToOutboundBridge(ctx, route, req, baseParams.SessionId)
	}

	authzCtx := auth.GetAuthorizationContext(ctx)
	session := c.sessionManagerProvider.GetSessionManager().GetSession(baseParams.SessionId)
	if session == nil {
		return ctx.JSON(http.StatusOK, acpErrResp(req.ID, -32602, "session not found"))
	}
	if !authzCtx.CanAccessResource(session.UserID(), string(session.Scope()), session.TeamID()) {
		return ctx.JSON(http.StatusOK, acpErrResp(req.ID, -32603, "permission denied"))
	}
	if c.runtimeRequiresOutbound() {
		return ctx.JSON(http.StatusOK, acpErrResp(req.ID, -32603, "session outbound runtime connection is unavailable"))
	}

	addr := session.Addr()
	if addr == "" {
		return ctx.JSON(http.StatusOK, acpErrResp(req.ID, -32603, "session has no address"))
	}

	// The bridge uses its own internal session ID, not the proxy session ID.
	// Fetch the bridge's session ID (cached after the first call) and substitute
	// it into params.sessionId before forwarding.
	bridgeSessionID, err := c.getBridgeSessionID(addr, baseParams.SessionId)
	if err != nil || bridgeSessionID == "" {
		log.Printf("[ACP] proxyToBridge: failed to get bridge session ID for %s: %v", baseParams.SessionId, err)
		return ctx.JSON(http.StatusOK, acpErrResp(req.ID, -32603, "failed to resolve bridge session ID"))
	}

	var rawParams map[string]json.RawMessage
	if len(req.Params) > 0 {
		if err := json.Unmarshal(req.Params, &rawParams); err == nil {
			bridgeIDJSON, _ := json.Marshal(bridgeSessionID)
			rawParams["sessionId"] = bridgeIDJSON
			if newParams, err := json.Marshal(rawParams); err == nil {
				req.Params = newParams
			}
		}
	}

	body, err := json.Marshal(req)
	if err != nil {
		return ctx.JSON(http.StatusOK, acpErrResp(req.ID, -32603, "failed to marshal request"))
	}

	rpcURL := "http://" + addr + "/rpc"
	log.Printf("[ACP] %s → POST %s (proxySessionId=%s bridgeSessionId=%s)", req.Method, rpcURL, baseParams.SessionId, bridgeSessionID)

	resp, err := http.Post(rpcURL, "application/json", bytes.NewReader(body)) //nolint:gosec
	if err != nil {
		return ctx.JSON(http.StatusOK, acpErrResp(req.ID, -32603, "bridge unreachable: "+err.Error()))
	}
	defer func() { closeutils.SafeCloseResponseBody(resp.Body) }()

	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		if req.Method == "session/set_config_option" {
			var rpcResp acpResponse
			if err := json.NewDecoder(resp.Body).Decode(&rpcResp); err != nil {
				return ctx.JSON(http.StatusOK, acpErrResp(req.ID, -32603, "failed to decode bridge response: "+err.Error()))
			}
			return ctx.JSON(http.StatusOK, rpcResp)
		}
		return ctx.JSON(http.StatusOK, acpSuccessResp(req.ID, struct{}{}))
	}

	var rpcResp interface{}
	_ = json.NewDecoder(resp.Body).Decode(&rpcResp)
	return ctx.JSON(http.StatusOK, acpErrResp(req.ID, -32603, fmt.Sprintf("bridge error (HTTP %d)", resp.StatusCode)))
}

// proxyResultToBridge forwards a JSON-RPC result (no method field) to the per-session
// bridge /rpc endpoint. The target session is identified by the Acp-Session-Id header.
func (c *ACPController) proxyResultToBridge(ctx echo.Context, req acpRequest) error {
	sessionId := ctx.Request().Header.Get("Acp-Session-Id")
	if sessionId == "" {
		return ctx.JSON(http.StatusBadRequest, map[string]string{"message": "Acp-Session-Id header is required"})
	}
	if route, routeErr := c.directRuntimeRoute(ctx.Request().Context(), sessionId); routeErr != nil {
		return ctx.JSON(http.StatusInternalServerError, map[string]string{"message": "session route lookup failed"})
	} else if route != nil {
		authzCtx := auth.GetAuthorizationContext(ctx)
		if authzCtx == nil || !authzCtx.CanAccessResource(route.UserID, route.Scope, route.TeamID) {
			return ctx.JSON(http.StatusForbidden, map[string]string{"message": "permission denied"})
		}
		body, err := json.Marshal(req)
		if err != nil {
			return ctx.JSON(http.StatusInternalServerError, map[string]string{"message": "failed to marshal request"})
		}
		resp, err := c.doDirectRuntimeRequest(ctx.Request().Context(), route, http.MethodPost, "/rpc", http.Header{"Content-Type": {"application/json"}}, body)
		if err != nil {
			return ctx.JSON(http.StatusBadGateway, map[string]string{"message": err.Error()})
		}
		defer func() { closeutils.SafeCloseResponseBody(resp.Body) }()
		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			return ctx.JSON(http.StatusBadGateway, map[string]string{"message": fmt.Sprintf("bridge error (HTTP %d)", resp.StatusCode)})
		}
		return ctx.JSON(http.StatusOK, map[string]interface{}{})
	}

	authzCtx := auth.GetAuthorizationContext(ctx)
	session := c.sessionManagerProvider.GetSessionManager().GetSession(sessionId)
	if session == nil {
		return ctx.JSON(http.StatusNotFound, map[string]string{"message": "session not found"})
	}
	if !authzCtx.CanAccessResource(session.UserID(), string(session.Scope()), session.TeamID()) {
		return ctx.JSON(http.StatusForbidden, map[string]string{"message": "permission denied"})
	}
	if c.runtimeRequiresOutbound() {
		return ctx.JSON(http.StatusServiceUnavailable, map[string]string{"message": "session outbound runtime connection is unavailable"})
	}
	addr := session.Addr()
	if addr == "" {
		return ctx.JSON(http.StatusServiceUnavailable, map[string]string{"message": "session has no address"})
	}

	body, err := json.Marshal(req)
	if err != nil {
		return ctx.JSON(http.StatusInternalServerError, map[string]string{"message": "failed to marshal request"})
	}

	rpcURL := "http://" + addr + "/rpc"
	log.Printf("[ACP] result → POST %s (session=%s)", rpcURL, sessionId)

	resp, err := http.Post(rpcURL, "application/json", bytes.NewReader(body)) //nolint:gosec
	if err != nil {
		return ctx.JSON(http.StatusBadGateway, map[string]string{"message": "bridge unreachable: " + err.Error()})
	}
	defer func() { closeutils.SafeCloseResponseBody(resp.Body) }()

	return ctx.JSON(http.StatusOK, map[string]interface{}{})
}

func (c *ACPController) directRuntimeRoute(ctx context.Context, sessionID string) (*portrepos.SessionRoute, error) {
	if c.sessionRouteRepo == nil {
		return nil, nil
	}
	route, err := c.sessionRouteRepo.Get(ctx, sessionID)
	if err != nil || route == nil || route.Transport != portrepos.SessionRouteTransportDirectRuntime {
		return nil, err
	}
	return route, nil
}

func (c *ACPController) doDirectRuntimeRequest(ctx context.Context, route *portrepos.SessionRoute, method, path string, headers http.Header, body []byte) (*http.Response, error) {
	if c.esmControlTunnel == nil || !c.esmControlTunnel.IsConnected(ctx, route.SessionID) {
		return nil, fmt.Errorf("session runtime connection is unavailable")
	}
	req, err := http.NewRequestWithContext(ctx, method, "http://session.local"+path, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header = headers.Clone()
	return c.esmControlTunnel.Do(ctx, route.SessionID, route.SessionID, route.RemoteSessionID, req)
}

func (c *ACPController) proxyToOutboundBridge(ctx echo.Context, route *portrepos.SessionRoute, req acpRequest, proxySessionID string) error {
	infoResp, err := c.doDirectRuntimeRequest(ctx.Request().Context(), route, http.MethodGet, "/session", make(http.Header), nil)
	if err != nil {
		return ctx.JSON(http.StatusOK, acpErrResp(req.ID, -32603, err.Error()))
	}
	defer func() { closeutils.SafeCloseResponseBody(infoResp.Body) }()
	var info struct {
		SessionID string `json:"sessionId"`
	}
	if infoResp.StatusCode/100 != 2 || json.NewDecoder(infoResp.Body).Decode(&info) != nil || info.SessionID == "" {
		return ctx.JSON(http.StatusOK, acpErrResp(req.ID, -32603, "failed to resolve bridge session ID"))
	}
	var params map[string]json.RawMessage
	if json.Unmarshal(req.Params, &params) == nil {
		params["sessionId"], _ = json.Marshal(info.SessionID)
		req.Params, _ = json.Marshal(params)
	}
	body, err := json.Marshal(req)
	if err != nil {
		return ctx.JSON(http.StatusOK, acpErrResp(req.ID, -32603, "failed to marshal request"))
	}
	resp, err := c.doDirectRuntimeRequest(ctx.Request().Context(), route, http.MethodPost, "/rpc", http.Header{"Content-Type": {"application/json"}}, body)
	if err != nil {
		return ctx.JSON(http.StatusOK, acpErrResp(req.ID, -32603, err.Error()))
	}
	defer func() { closeutils.SafeCloseResponseBody(resp.Body) }()
	if resp.StatusCode/100 != 2 {
		return ctx.JSON(http.StatusOK, acpErrResp(req.ID, -32603, fmt.Sprintf("bridge error (HTTP %d)", resp.StatusCode)))
	}
	log.Printf("[ACP] %s routed over outbound runtime (proxySessionId=%s bridgeSessionId=%s)", req.Method, proxySessionID, info.SessionID)
	if req.Method == "session/set_config_option" {
		var rpcResp acpResponse
		if err := json.NewDecoder(resp.Body).Decode(&rpcResp); err != nil {
			return ctx.JSON(http.StatusOK, acpErrResp(req.ID, -32603, "failed to decode bridge response"))
		}
		return ctx.JSON(http.StatusOK, rpcResp)
	}
	return ctx.JSON(http.StatusOK, acpSuccessResp(req.ID, struct{}{}))
}

// ----------------------------------------------------------------------------
// HandleSessionSSE – GET /acp
// ----------------------------------------------------------------------------

// HandleSessionSSE handles GET /acp with Acp-Session-Id header.
// Streams live session/update notifications from the per-session bridge until
// the client disconnects. History is NOT replayed here; clients fetch it separately.
func (c *ACPController) HandleSessionSSE(ctx echo.Context) error {
	log.Printf("[ACP] HandleSessionSSE: called (method=%s, origin=%s, headers=%v)",
		ctx.Request().Method,
		ctx.Request().Header.Get("Origin"),
		ctx.Request().Header,
	)
	sessionId := ctx.Request().Header.Get("Acp-Session-Id")
	if sessionId == "" {
		log.Printf("[ACP] HandleSessionSSE: missing Acp-Session-Id header")
		return ctx.JSON(http.StatusBadRequest, map[string]string{"message": "Acp-Session-Id header is required"})
	}

	authzCtx := auth.GetAuthorizationContext(ctx)
	if c.sessionRouteRepo != nil {
		route, err := c.sessionRouteRepo.Get(ctx.Request().Context(), sessionId)
		if err != nil {
			log.Printf("[ACP] HandleSessionSSE: route lookup failed (sessionId=%s): %v", sessionId, err)
			return ctx.JSON(http.StatusInternalServerError, map[string]string{"message": "session route lookup failed"})
		}
		if route != nil && route.Transport == portrepos.SessionRouteTransportDirectRuntime {
			if !authzCtx.CanAccessResource(route.UserID, route.Scope, route.TeamID) {
				return ctx.JSON(http.StatusForbidden, map[string]string{"message": "permission denied"})
			}
			return c.handleRemoteSessionSSE(ctx, route)
		}
	}
	session := c.sessionManagerProvider.GetSessionManager().GetSession(sessionId)
	if session == nil {
		if c.sessionRouteRepo != nil {
			route, err := c.sessionRouteRepo.Get(ctx.Request().Context(), sessionId)
			if err != nil {
				log.Printf("[ACP] HandleSessionSSE: route lookup failed (sessionId=%s): %v", sessionId, err)
				return ctx.JSON(http.StatusInternalServerError, map[string]string{"message": "session route lookup failed"})
			}
			if route != nil && route.ManagerID != "" && route.RemoteSessionID != "" {
				if !authzCtx.CanAccessResource(route.UserID, route.Scope, route.TeamID) {
					return ctx.JSON(http.StatusForbidden, map[string]string{"message": "permission denied"})
				}
				return c.handleRemoteSessionSSE(ctx, route)
			}
		}
		log.Printf("[ACP] HandleSessionSSE: session not found (sessionId=%s)", sessionId)
		return ctx.JSON(http.StatusNotFound, map[string]string{"message": "session not found"})
	}
	if !authzCtx.CanAccessResource(session.UserID(), string(session.Scope()), session.TeamID()) {
		log.Printf("[ACP] HandleSessionSSE: permission denied (sessionId=%s)", sessionId)
		return ctx.JSON(http.StatusForbidden, map[string]string{"message": "permission denied"})
	}
	if c.runtimeRequiresOutbound() {
		return ctx.JSON(http.StatusServiceUnavailable, map[string]string{"message": "session outbound runtime connection is unavailable"})
	}
	addr := session.Addr()
	if addr == "" {
		log.Printf("[ACP] HandleSessionSSE: session has no address (sessionId=%s)", sessionId)
		return ctx.JSON(http.StatusServiceUnavailable, map[string]string{"message": "session has no address"})
	}

	log.Printf("[ACP] HandleSessionSSE: connecting to bridge SSE (sessionId=%s, addr=%s)", sessionId, addr)

	// Forward Last-Event-ID so the bridge replays only missed messages.
	lastEventID := ctx.Request().Header.Get("Last-Event-ID")
	if lastEventID == "" {
		lastEventID = ctx.QueryParam("lastEventId")
	}

	w := ctx.Response()
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)

	flusher, hasFlusher := w.Writer.(http.Flusher)

	// Flush response headers immediately so the client receives the 200 OK
	// before we block waiting for the bridge's first SSE event.
	if hasFlusher {
		flusher.Flush()
	}

	// Stream events from the bridge's /sse endpoint, forwarding Last-Event-ID so
	// the bridge replays history from the correct position on reconnect.
	c.streamBridgeSSE(ctx.Request().Context(), addr, w, flusher, hasFlusher, lastEventID)
	return nil
}
