package controllers

import (
	"context"
	"crypto/subtle"
	"net/http"
	"strings"
	"time"

	"github.com/labstack/echo/v4"
	"github.com/takutakahashi/agentapi-proxy/internal/domain/entities"
	"github.com/takutakahashi/agentapi-proxy/internal/modules/schedule"
	"github.com/takutakahashi/agentapi-proxy/internal/usecases/ports/repositories"
	sessionuc "github.com/takutakahashi/agentapi-proxy/internal/usecases/session"
	"github.com/takutakahashi/agentapi-proxy/pkg/executiontoken"
)

// WorkerControlController is the deliberately small API surface available to
// background workers. Its credential is not accepted by provisioner or
// session-manager endpoints.
type WorkerControlController struct {
	manager         repositories.SessionManager
	token           string
	teams           workerTeamEnsurer
	routes          repositories.SessionRouteRepository
	leases          schedule.LeaseClient
	scheduleManager schedule.Manager
}

func (wc *WorkerControlController) WithScheduleManager(manager schedule.Manager) *WorkerControlController {
	wc.scheduleManager = manager
	return wc
}

type scheduleJob struct {
	ScheduleID     string                `json:"schedule_id"`
	ExecutionID    string                `json:"execution_id"`
	SessionID      string                `json:"session_id"`
	ExecutionToken string                `json:"execution_token"`
	StartRequest   entities.StartRequest `json:"start_request"`
}

func (wc *WorkerControlController) ClaimDueSchedules(c echo.Context) error {
	if !wc.authorized(c) {
		return c.NoContent(http.StatusUnauthorized)
	}
	if wc.scheduleManager == nil {
		return c.NoContent(http.StatusServiceUnavailable)
	}
	items, err := wc.scheduleManager.ClaimDueSchedules(c.Request().Context(), time.Now(), 5*time.Minute)
	if err != nil {
		return c.JSON(http.StatusBadGateway, map[string]string{"error": err.Error()})
	}
	jobs := make([]scheduleJob, 0, len(items))
	for _, item := range items {
		claims := executiontoken.ExecutionClaims{ScheduleID: item.ID, ExecutionID: item.PendingExecution.ExecutionID, SessionID: item.PendingExecution.SessionID, UserID: item.UserID, Scope: item.GetScope(), TeamID: item.TeamID, Teams: sessionuc.ResolveTeams(item.GetScope(), item.TeamID, item.UserTeams), ExpiresAt: item.PendingExecution.ExpiresAt.Unix()}
		token, err := executiontoken.SignExecutionToken([]byte(wc.token), claims)
		if err != nil {
			return err
		}
		tags := make(map[string]string, len(item.SessionConfig.Tags)+1)
		for k, v := range item.SessionConfig.Tags {
			tags[k] = v
		}
		tags["schedule_id"] = item.ID
		jobs = append(jobs, scheduleJob{ScheduleID: item.ID, ExecutionID: claims.ExecutionID, SessionID: claims.SessionID, ExecutionToken: token, StartRequest: entities.StartRequest{Environment: item.SessionConfig.Environment, Tags: tags, Params: item.SessionConfig.Params, Scope: item.GetScope(), TeamID: item.TeamID, MemoryKey: item.SessionConfig.MemoryKey, SessionProfileID: item.SessionConfig.SessionProfileID}})
	}
	return c.JSON(http.StatusOK, map[string]any{"jobs": jobs})
}

func (wc *WorkerControlController) FinalizeSchedule(c echo.Context) error {
	if !wc.authorized(c) {
		return c.NoContent(http.StatusUnauthorized)
	}
	var req struct {
		ExecutionID string `json:"execution_id"`
		SessionID   string `json:"session_id"`
		Status      string `json:"status"`
		Error       string `json:"error"`
	}
	if err := c.Bind(&req); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "invalid request")
	}
	if req.ExecutionID == "" || (req.Status != "success" && req.Status != "failed") {
		return echo.NewHTTPError(http.StatusBadRequest, "execution_id and a valid status are required")
	}
	if req.Status == "success" && req.SessionID == "" {
		return echo.NewHTTPError(http.StatusBadRequest, "session_id is required for successful execution")
	}
	item, err := wc.scheduleManager.Get(c.Request().Context(), c.Param("id"))
	if err != nil {
		return err
	}
	var next *time.Time
	completed := item.IsOneTime() && req.Status == "success"
	if item.IsOneTime() && !completed {
		retryAt := time.Now().Add(time.Minute)
		next = &retryAt
	} else if !completed {
		value, calcErr := schedule.CalculateNextExecution(item, time.Now())
		if calcErr != nil {
			return calcErr
		}
		next = value
	}
	record := schedule.ExecutionRecord{ExecutionID: req.ExecutionID, SessionID: req.SessionID, Status: req.Status, Error: req.Error, ExecutedAt: time.Now()}
	if err := wc.scheduleManager.FinalizeClaim(c.Request().Context(), item.ID, req.ExecutionID, record, next, completed); err != nil {
		return c.JSON(http.StatusConflict, map[string]string{"error": err.Error()})
	}
	return c.NoContent(http.StatusNoContent)
}

type workerTeamEnsurer interface {
	EnsureTeamServiceAccount(context.Context, string) error
	EnsurePersonalAPIKey(context.Context, string) error
}

type workerStockManager interface {
	CreateStockSession(context.Context, bool) error
	CountStockSessions(context.Context, bool) (int, error)
	PurgeStaleStockSessions(context.Context) error
}

type workerSessionLister interface {
	ListSessionsContext(context.Context, entities.SessionFilter) ([]entities.Session, error)
}

type workerSessionInfo struct {
	ID            string                 `json:"id"`
	UserID        string                 `json:"user_id"`
	Scope         entities.ResourceScope `json:"scope"`
	TeamID        string                 `json:"team_id"`
	Tags          map[string]string      `json:"tags"`
	Status        string                 `json:"status"`
	StartedAt     time.Time              `json:"started_at"`
	LastMessageAt time.Time              `json:"last_message_at"`
}

func workerSessionInfoFrom(session entities.Session) workerSessionInfo {
	tags := make(map[string]string, len(session.Tags())+1)
	for key, value := range session.Tags() {
		tags[key] = value
	}
	if provider, ok := session.(interface {
		Request() *entities.RunServerRequest
	}); ok && provider.Request() != nil && provider.Request().SessionTTL != "" {
		tags["session_ttl"] = provider.Request().SessionTTL
	}
	return workerSessionInfo{ID: session.ID(), UserID: session.UserID(), Scope: session.Scope(), TeamID: session.TeamID(), Tags: tags, Status: session.Status(), StartedAt: session.StartedAt(), LastMessageAt: session.LastMessageAt()}
}

func NewWorkerControlController(manager repositories.SessionManager, token string, teams workerTeamEnsurer, routes repositories.SessionRouteRepository) *WorkerControlController {
	controller := &WorkerControlController{manager: manager, token: token}
	controller.teams = teams
	controller.routes = routes
	return controller
}

func (wc *WorkerControlController) WithLeases(client schedule.LeaseClient) *WorkerControlController {
	wc.leases = client
	return wc
}

type workerLeaseRequest struct {
	Action     string `json:"action"`
	Identity   string `json:"identity"`
	DurationMS int64  `json:"duration_ms"`
}

func (wc *WorkerControlController) Lease(c echo.Context) error {
	if !wc.authorized(c) {
		return c.NoContent(http.StatusUnauthorized)
	}
	if wc.leases == nil {
		return c.NoContent(http.StatusServiceUnavailable)
	}
	var req workerLeaseRequest
	if err := c.Bind(&req); err != nil || req.Identity == "" {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "invalid lease request"})
	}
	if req.DurationMS <= 0 && req.Action != "release" {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "duration_ms must be positive"})
	}
	key := c.Param("leaseName")
	duration := time.Duration(req.DurationMS) * time.Millisecond
	var acquired bool
	var err error
	switch req.Action {
	case "acquire":
		acquired, err = wc.leases.Acquire(c.Request().Context(), key, req.Identity, duration)
	case "renew":
		acquired, err = wc.leases.Renew(c.Request().Context(), key, req.Identity, duration)
	case "release":
		acquired, err = wc.leases.Release(c.Request().Context(), key, req.Identity)
	default:
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "unsupported lease action"})
	}
	if err != nil {
		return c.JSON(http.StatusBadGateway, map[string]string{"error": err.Error()})
	}
	return c.JSON(http.StatusOK, map[string]bool{"acquired": acquired})
}

func (wc *WorkerControlController) runtimeID(ctx context.Context, publicID string) string {
	if wc.routes == nil {
		return publicID
	}
	route, err := wc.routes.Get(ctx, publicID)
	if err == nil && route != nil && route.ManagerID == "" && route.RemoteSessionID != "" {
		return route.RemoteSessionID
	}
	return publicID
}

func (wc *WorkerControlController) authorized(c echo.Context) bool {
	if wc == nil || wc.manager == nil || wc.token == "" {
		return false
	}
	header := c.Request().Header.Get(echo.HeaderAuthorization)
	provided, ok := strings.CutPrefix(header, "Bearer ")
	return ok && len(provided) == len(wc.token) && subtle.ConstantTimeCompare([]byte(provided), []byte(wc.token)) == 1
}

func (wc *WorkerControlController) CreateSession(c echo.Context) error {
	if !wc.authorized(c) {
		return c.NoContent(http.StatusUnauthorized)
	}
	var req entities.RunServerRequest
	if err := c.Bind(&req); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": err.Error()})
	}
	if req.Scope == entities.ScopeTeam && req.TeamID != "" && wc.teams != nil {
		if err := wc.teams.EnsureTeamServiceAccount(c.Request().Context(), req.TeamID); err != nil {
			return c.JSON(http.StatusInternalServerError, map[string]string{"error": err.Error()})
		}
	} else if wc.teams != nil {
		if err := wc.teams.EnsurePersonalAPIKey(c.Request().Context(), req.UserID); err != nil {
			return c.JSON(http.StatusInternalServerError, map[string]string{"error": err.Error()})
		}
	}
	session, err := wc.manager.CreateSession(c.Request().Context(), c.Param("sessionId"), &req, nil)
	if err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": err.Error()})
	}
	return c.JSON(http.StatusCreated, workerSessionInfoFrom(session))
}

func (wc *WorkerControlController) ListSessions(c echo.Context) error {
	if !wc.authorized(c) {
		return c.NoContent(http.StatusUnauthorized)
	}
	var sessions []entities.Session
	var err error
	if lister, ok := wc.manager.(workerSessionLister); ok {
		sessions, err = lister.ListSessionsContext(c.Request().Context(), entities.SessionFilter{})
	} else {
		sessions = wc.manager.ListSessions(entities.SessionFilter{})
	}
	if err != nil {
		return c.JSON(http.StatusBadGateway, map[string]string{"error": err.Error()})
	}
	if wc.routes != nil {
		routes, routeErr := wc.routes.List(c.Request().Context(), "")
		if routeErr != nil {
			return c.JSON(http.StatusBadGateway, map[string]string{"error": routeErr.Error()})
		}
		byID := make(map[string]entities.Session, len(sessions))
		for _, session := range sessions {
			byID[session.ID()] = session
		}
		aliasedRuntime := make(map[string]bool)
		for _, route := range routes {
			if route.ManagerID != "" || route.RemoteSessionID == "" {
				continue
			}
			if runtime := byID[route.RemoteSessionID]; runtime != nil {
				sessions = append(sessions, &workerAliasSession{Session: runtime, id: route.SessionID})
				aliasedRuntime[route.RemoteSessionID] = true
			}
		}
		filtered := make([]entities.Session, 0, len(sessions))
		for _, session := range sessions {
			if !aliasedRuntime[session.ID()] {
				filtered = append(filtered, session)
			}
		}
		sessions = filtered
	}
	result := make([]workerSessionInfo, 0, len(sessions))
	for _, session := range sessions {
		result = append(result, workerSessionInfoFrom(session))
	}
	return c.JSON(http.StatusOK, result)
}

func (wc *WorkerControlController) DeleteSession(c echo.Context) error {
	if !wc.authorized(c) {
		return c.NoContent(http.StatusUnauthorized)
	}
	if err := wc.manager.DeleteSession(wc.runtimeID(c.Request().Context(), c.Param("sessionId"))); err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": err.Error()})
	}
	return c.NoContent(http.StatusNoContent)
}

func (wc *WorkerControlController) SendMessage(c echo.Context) error {
	if !wc.authorized(c) {
		return c.NoContent(http.StatusUnauthorized)
	}
	var req struct {
		Message string `json:"message"`
	}
	if err := c.Bind(&req); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": err.Error()})
	}
	if err := wc.manager.SendMessage(c.Request().Context(), wc.runtimeID(c.Request().Context(), c.Param("sessionId")), req.Message); err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": err.Error()})
	}
	return c.NoContent(http.StatusNoContent)
}

func (wc *WorkerControlController) StopAgent(c echo.Context) error {
	if !wc.authorized(c) {
		return c.NoContent(http.StatusUnauthorized)
	}
	if err := wc.manager.StopAgent(c.Request().Context(), wc.runtimeID(c.Request().Context(), c.Param("sessionId"))); err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": err.Error()})
	}
	return c.NoContent(http.StatusNoContent)
}

type workerAliasSession struct {
	entities.Session
	id string
}

func (s *workerAliasSession) ID() string { return s.id }

func (wc *WorkerControlController) Stock(c echo.Context) error {
	if !wc.authorized(c) {
		return c.NoContent(http.StatusUnauthorized)
	}
	stock, ok := wc.manager.(workerStockManager)
	if !ok {
		return c.NoContent(http.StatusNotImplemented)
	}
	dind := c.QueryParam("dind") == "true"
	switch c.Request().Method {
	case http.MethodGet:
		count, err := stock.CountStockSessions(c.Request().Context(), dind)
		if err != nil {
			return c.JSON(http.StatusBadGateway, map[string]string{"error": err.Error()})
		}
		return c.JSON(http.StatusOK, map[string]int{"count": count})
	case http.MethodPost:
		if err := stock.CreateStockSession(c.Request().Context(), dind); err != nil {
			return c.JSON(http.StatusBadGateway, map[string]string{"error": err.Error()})
		}
		return c.NoContent(http.StatusCreated)
	case http.MethodDelete:
		if err := stock.PurgeStaleStockSessions(c.Request().Context()); err != nil {
			return c.JSON(http.StatusBadGateway, map[string]string{"error": err.Error()})
		}
		return c.NoContent(http.StatusNoContent)
	default:
		return c.NoContent(http.StatusMethodNotAllowed)
	}
}
