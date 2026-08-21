package controllers

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"net/http"
	"strings"
	"time"

	"github.com/labstack/echo/v4"
	"github.com/takutakahashi/agentapi-proxy/internal/domain/entities"
	"github.com/takutakahashi/agentapi-proxy/internal/usecases/ports/repositories"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

// WorkerControlController is the deliberately small API surface available to
// background workers. Its credential is not accepted by provisioner or
// session-manager endpoints.
type WorkerControlController struct {
	manager        repositories.SessionManager
	token          string
	teams          workerTeamEnsurer
	routes         repositories.SessionRouteRepository
	leases         kubernetes.Interface
	leaseNamespace string
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

func (wc *WorkerControlController) WithLeases(client kubernetes.Interface, namespace string) *WorkerControlController {
	if namespace == "" {
		namespace = "default"
	}
	wc.leases, wc.leaseNamespace = client, namespace
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
	digest := sha256.Sum256([]byte(key))
	name := "agentapi-worker-lease-" + hex.EncodeToString(digest[:])[:20]
	items := wc.leases.CoreV1().ConfigMaps(wc.leaseNamespace)
	now := time.Now().UTC()
	current, err := items.Get(c.Request().Context(), name, metav1.GetOptions{})
	if apierrors.IsNotFound(err) {
		if req.Action != "acquire" {
			return c.JSON(http.StatusOK, map[string]bool{"acquired": false})
		}
		_, err = items.Create(c.Request().Context(), &corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: name}, Data: map[string]string{"key": key, "identity": req.Identity, "expires_at": now.Add(time.Duration(req.DurationMS) * time.Millisecond).Format(time.RFC3339Nano)}}, metav1.CreateOptions{})
		if apierrors.IsAlreadyExists(err) {
			return c.JSON(http.StatusOK, map[string]bool{"acquired": false})
		}
		if err != nil {
			return c.JSON(http.StatusBadGateway, map[string]string{"error": err.Error()})
		}
		return c.JSON(http.StatusOK, map[string]bool{"acquired": true})
	}
	if err != nil {
		return c.JSON(http.StatusBadGateway, map[string]string{"error": err.Error()})
	}
	expires, _ := time.Parse(time.RFC3339Nano, current.Data["expires_at"])
	owned := current.Data["identity"] == req.Identity
	switch req.Action {
	case "acquire":
		if !owned && now.Before(expires) {
			return c.JSON(http.StatusOK, map[string]bool{"acquired": false})
		}
		current.Data["identity"], current.Data["expires_at"] = req.Identity, now.Add(time.Duration(req.DurationMS)*time.Millisecond).Format(time.RFC3339Nano)
		_, err = items.Update(c.Request().Context(), current, metav1.UpdateOptions{})
	case "renew":
		if !owned || !now.Before(expires) {
			return c.JSON(http.StatusOK, map[string]bool{"acquired": false})
		}
		current.Data["expires_at"] = now.Add(time.Duration(req.DurationMS) * time.Millisecond).Format(time.RFC3339Nano)
		_, err = items.Update(c.Request().Context(), current, metav1.UpdateOptions{})
	case "release":
		if !owned {
			return c.JSON(http.StatusOK, map[string]bool{"acquired": false})
		}
		err = items.Delete(c.Request().Context(), name, metav1.DeleteOptions{Preconditions: &metav1.Preconditions{ResourceVersion: &current.ResourceVersion}})
	default:
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "unsupported lease action"})
	}
	if apierrors.IsConflict(err) || apierrors.IsNotFound(err) {
		return c.JSON(http.StatusOK, map[string]bool{"acquired": false})
	}
	if err != nil {
		return c.JSON(http.StatusBadGateway, map[string]string{"error": err.Error()})
	}
	return c.JSON(http.StatusOK, map[string]bool{"acquired": true})
}

func (wc *WorkerControlController) runtimeID(ctx context.Context, publicID string) string {
	if wc.routes == nil {
		return publicID
	}
	route, err := wc.routes.Get(ctx, publicID)
	if err == nil && route != nil && route.ProxyURL == "" && route.RemoteSessionID != "" {
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
			if route.ProxyURL != "" || route.RemoteSessionID == "" {
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
