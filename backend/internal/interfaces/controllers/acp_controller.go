package controllers

import (
	"encoding/base64"
	"encoding/json"
	"log"
	"net/http"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/labstack/echo/v4"
	"github.com/takutakahashi/agentapi-proxy/internal/domain/entities"
	portrepos "github.com/takutakahashi/agentapi-proxy/internal/usecases/ports/repositories"
	"github.com/takutakahashi/agentapi-proxy/pkg/auth"
)

const (
	acpProtocolVersion  = "2025-01-15"
	acpSessionListLimit = 20
)

// ACPController handles ACP (Agent Client Protocol) JSON-RPC 2.0 endpoints.
// POST /acp  – JSON-RPC request/response
// GET  /acp/sse – SSE stream of session/update notifications
type ACPController struct {
	sessionManagerProvider SessionManagerProvider
	sessionCreator         SessionCreator
	sessionRouteRepo       portrepos.SessionRouteRepository
	esmControlTunnel       ESMControlTunnel
	// bridgeSessionCache maps proxy session ID → bridge-internal session ID.
	// Populated on first use to avoid fetching GET /session on every RPC call.
	bridgeSessionCache sync.Map
}

func (c *ACPController) SetESMControlTunnel(tunnel ESMControlTunnel) {
	c.esmControlTunnel = tunnel
}

// NewACPController creates a new ACPController.
func NewACPController(
	sessionManagerProvider SessionManagerProvider,
	sessionCreator SessionCreator,
	sessionRouteRepos ...portrepos.SessionRouteRepository,
) *ACPController {
	var sessionRouteRepo portrepos.SessionRouteRepository
	if len(sessionRouteRepos) > 0 {
		sessionRouteRepo = sessionRouteRepos[0]
	}
	return &ACPController{
		sessionManagerProvider: sessionManagerProvider,
		sessionCreator:         sessionCreator,
		sessionRouteRepo:       sessionRouteRepo,
	}
}

// ----------------------------------------------------------------------------
// Wire types
// ----------------------------------------------------------------------------

type acpRequest struct {
	JSONRPC string           `json:"jsonrpc"`
	ID      *json.RawMessage `json:"id,omitempty"`
	Method  string           `json:"method"`
	Params  json.RawMessage  `json:"params,omitempty"`
}

type acpResponse struct {
	JSONRPC string           `json:"jsonrpc"`
	ID      *json.RawMessage `json:"id,omitempty"`
	Result  interface{}      `json:"result,omitempty"`
	Error   *acpRPCError     `json:"error,omitempty"`
}

type acpRPCError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

func acpSuccessResp(id *json.RawMessage, result interface{}) acpResponse {
	return acpResponse{JSONRPC: "2.0", ID: id, Result: result}
}

func acpErrResp(id *json.RawMessage, code int, message string) acpResponse {
	return acpResponse{JSONRPC: "2.0", ID: id, Error: &acpRPCError{Code: code, Message: message}}
}

// ----------------------------------------------------------------------------
// HandleRPC – POST /acp
// ----------------------------------------------------------------------------

// HandleRPC handles POST /acp (JSON-RPC 2.0).
func (c *ACPController) HandleRPC(ctx echo.Context) error {
	var req acpRequest
	if err := ctx.Bind(&req); err != nil {
		return ctx.JSON(http.StatusOK, acpErrResp(nil, -32700, "Parse error"))
	}
	if req.JSONRPC != "2.0" {
		return ctx.JSON(http.StatusOK, acpErrResp(req.ID, -32600, "Invalid Request: jsonrpc must be \"2.0\""))
	}

	// JSON-RPC result (no method field) — route to session bridge via Acp-Session-Id header.
	if req.Method == "" {
		return c.proxyResultToBridge(ctx, req)
	}

	switch req.Method {
	case "initialize":
		return c.handleInitialize(ctx, req)
	case "session/new":
		return c.handleSessionNew(ctx, req)
	case "session/list":
		return c.handleSessionList(ctx, req)
	case "session/close":
		return c.handleSessionClose(ctx, req)
	case "session/resume":
		return c.handleSessionResume(ctx, req)
	case "session/load":
		return c.handleSessionLoad(ctx, req)
	case "session/prompt", "session/cancel", "session/set_config_option":
		return c.proxyToBridge(ctx, req)
	default:
		return ctx.JSON(http.StatusOK, acpErrResp(req.ID, -32601, "Method not found: "+req.Method))
	}
}

// ----------------------------------------------------------------------------
// initialize
// ----------------------------------------------------------------------------

func (c *ACPController) handleInitialize(ctx echo.Context, req acpRequest) error {
	result := map[string]interface{}{
		"protocolVersion": acpProtocolVersion,
		"capabilities": map[string]interface{}{
			"sessionCapabilities": map[string]bool{
				"list":        true,
				"close":       true,
				"resume":      true,
				"loadSession": true,
			},
		},
		"serverInfo": map[string]string{
			"name": "agentapi-proxy",
		},
	}
	return ctx.JSON(http.StatusOK, acpSuccessResp(req.ID, result))
}

// ----------------------------------------------------------------------------
// session/new
// ----------------------------------------------------------------------------

type sessionNewParams struct {
	Cwd        string      `json:"cwd"`
	McpServers interface{} `json:"mcpServers"`
	Meta       struct {
		Tags   map[string]string `json:"tags"`
		Params struct {
			Message   string `json:"message"`
			AgentType string `json:"agentType"`
			Model     string `json:"model"`
			Pool      string `json:"pool"`
		} `json:"params"`
	} `json:"_meta"`
}

func (c *ACPController) handleSessionNew(ctx echo.Context, req acpRequest) error {
	var params sessionNewParams
	if len(req.Params) > 0 {
		if err := json.Unmarshal(req.Params, &params); err != nil {
			return ctx.JSON(http.StatusOK, acpErrResp(req.ID, -32602, "invalid params: "+err.Error()))
		}
	}
	if params.Cwd == "" {
		return ctx.JSON(http.StatusOK, acpErrResp(req.ID, -32602, "cwd is required"))
	}

	authzCtx := auth.GetAuthorizationContext(ctx)
	userID := authzCtx.PersonalScope.UserID
	userRole := "user"
	if authzCtx.User != nil && len(authzCtx.User.Roles()) > 0 {
		userRole = string(authzCtx.User.Roles()[0])
	}
	teams := authzCtx.TeamScope.Teams

	tags := make(map[string]string)
	for k, v := range params.Meta.Tags {
		tags[k] = v
	}
	tags["cwd"] = params.Cwd

	startReq := entities.StartRequest{
		Scope: entities.ScopeUser,
		Tags:  tags,
		Params: &entities.SessionParams{
			Message:   params.Meta.Params.Message,
			AgentType: params.Meta.Params.AgentType,
			Model:     params.Meta.Params.Model,
			Pool:      params.Meta.Params.Pool,
		},
	}

	resolvedScope, resolvedTeamID := authzCtx.ResolveScope(string(startReq.Scope), startReq.TeamID)
	startReq.Scope = entities.ResourceScope(resolvedScope)
	startReq.TeamID = resolvedTeamID

	sessionID := uuid.New().String()
	session, err := c.sessionCreator.CreateSession(ctx.Request().Context(), sessionID, startReq, userID, userRole, teams)
	if err != nil {
		log.Printf("[ACP] session/new failed: %v", err)
		return ctx.JSON(http.StatusOK, acpErrResp(req.ID, -32603, "failed to create session: "+err.Error()))
	}

	// Use the actual session ID from the returned entity: a stock K8s session may have been
	// adopted with a different ID than the one we generated.
	actualID := session.ID()
	log.Printf("[ACP] session/new created sessionId=%s for user=%s", actualID, userID)
	return ctx.JSON(http.StatusOK, acpSuccessResp(req.ID, map[string]string{
		"sessionId": actualID,
	}))
}

// ----------------------------------------------------------------------------
// session/list
// ----------------------------------------------------------------------------

type sessionListParams struct {
	Cwd    string `json:"cwd"`
	Cursor string `json:"cursor"`
}

func (c *ACPController) handleSessionList(ctx echo.Context, req acpRequest) error {
	var params sessionListParams
	if len(req.Params) > 0 {
		_ = json.Unmarshal(req.Params, &params)
	}

	authzCtx := auth.GetAuthorizationContext(ctx)
	userID := authzCtx.PersonalScope.UserID

	filter := entities.SessionFilter{
		UserID: userID,
		Scope:  entities.ScopeUser,
	}
	if params.Cwd != "" {
		filter.Tags = map[string]string{"cwd": params.Cwd}
	}

	sessions := c.sessionManagerProvider.GetSessionManager().ListSessions(filter)

	// Decode cursor to determine start index.
	startIdx := 0
	if params.Cursor != "" {
		if decoded, err := base64.StdEncoding.DecodeString(params.Cursor); err == nil {
			cursorID := string(decoded)
			for i, s := range sessions {
				if s.ID() == cursorID {
					startIdx = i + 1
					break
				}
			}
		}
	}

	endIdx := startIdx + acpSessionListLimit
	var nextCursor string
	if endIdx < len(sessions) {
		nextCursor = base64.StdEncoding.EncodeToString([]byte(sessions[endIdx-1].ID()))
	} else {
		endIdx = len(sessions)
	}

	result := make([]map[string]interface{}, 0, endIdx-startIdx)
	for _, s := range sessions[startIdx:endIdx] {
		result = append(result, map[string]interface{}{
			"sessionId": s.ID(),
			"cwd":       s.Tags()["cwd"],
			"title":     s.Description(),
			"updatedAt": s.UpdatedAt().Format(time.RFC3339),
			"_meta": map[string]interface{}{
				"status": s.Status(),
				"scope":  string(s.Scope()),
				"teamId": s.TeamID(),
				"tags":   s.Tags(),
			},
		})
	}

	resp := map[string]interface{}{
		"sessions": result,
	}
	if nextCursor != "" {
		resp["nextCursor"] = nextCursor
	}
	return ctx.JSON(http.StatusOK, acpSuccessResp(req.ID, resp))
}

// ----------------------------------------------------------------------------
// session/close
// ----------------------------------------------------------------------------

func (c *ACPController) handleSessionClose(ctx echo.Context, req acpRequest) error {
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
	if !authzCtx.CanModifyResource(session.UserID(), string(session.Scope()), session.TeamID()) {
		return ctx.JSON(http.StatusOK, acpErrResp(req.ID, -32603, "permission denied"))
	}

	if err := c.sessionCreator.DeleteSessionByID(params.SessionId); err != nil {
		log.Printf("[ACP] session/close failed: %v", err)
		return ctx.JSON(http.StatusOK, acpErrResp(req.ID, -32603, "failed to close session: "+err.Error()))
	}

	log.Printf("[ACP] session/close sessionId=%s", params.SessionId)
	return ctx.JSON(http.StatusOK, acpSuccessResp(req.ID, struct{}{}))
}

// ----------------------------------------------------------------------------
// session/resume
// ----------------------------------------------------------------------------

func (c *ACPController) handleSessionResume(ctx echo.Context, req acpRequest) error {
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
	if ensurer, ok := c.sessionManagerProvider.GetSessionManager().(portrepos.SessionWorkloadEnsurer); ok {
		ensured, restoring, err := ensurer.EnsureSessionWorkload(ctx.Request().Context(), params.SessionId)
		if err != nil {
			return ctx.JSON(http.StatusOK, acpErrResp(req.ID, -32603, "failed to resume session workload: "+err.Error()))
		}
		if ensured != nil {
			session = ensured
		}
		if restoring {
			return ctx.JSON(http.StatusOK, acpSuccessResp(req.ID, map[string]string{"status": "resuming"}))
		}
	}

	// "active" is the proxy-level status for a running K8s session.
	// "running"/"stable" are agentapi-level statuses used in other contexts.
	status := session.Status()
	if status != "active" && status != "running" && status != "stable" {
		return ctx.JSON(http.StatusOK, acpErrResp(req.ID, -32603, "session is not active (status: "+status+")"))
	}

	log.Printf("[ACP] session/resume sessionId=%s status=%s", params.SessionId, status)
	return ctx.JSON(http.StatusOK, acpSuccessResp(req.ID, struct{}{}))
}

// ----------------------------------------------------------------------------
// session/load
// ----------------------------------------------------------------------------

// handleSessionLoad confirms the session exists and returns {}.
// History replay is handled by the SSE endpoint (GET /acp/sse) which automatically
// replays stored bridge messages when a client connects.
