package controllers

import (
	"net/http"
	"time"

	"github.com/labstack/echo/v4"
	"github.com/takutakahashi/agentapi-proxy/internal/domain/entities"
	"github.com/takutakahashi/agentapi-proxy/internal/usecases/ports/repositories"
)

func (wc *WorkerControlController) DeleteSession(c echo.Context) error {
	if !wc.authorized(c) {
		return c.NoContent(http.StatusUnauthorized)
	}
	publicID := c.Param("sessionId")
	if wc.routes != nil {
		route, err := wc.routes.Get(c.Request().Context(), publicID)
		if err != nil {
			return c.JSON(http.StatusInternalServerError, map[string]string{"error": err.Error()})
		}
		if route != nil && route.ManagerID != "" && wc.sessionDeleter != nil {
			// Direct runtimes are owned by an external session manager. Reuse the
			// durable public deletion path so the workload is removed before its
			// route alias is reconciled away.
			if err := wc.sessionDeleter(c); err != nil {
				return err
			}
			// Direct-runtime deletion may be accepted asynchronously. TTL cleanup
			// will not issue another DELETE after a 2xx response, so remove the pool
			// allocation here instead of relying on later route reconciliation.
			if wc.allocationCleaner != nil {
				if err := wc.allocationCleaner(c.Request().Context(), publicID); err != nil {
					return err
				}
			}
			return nil
		}
	}
	if err := wc.manager.DeleteSession(wc.runtimeID(c.Request().Context(), publicID)); err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": err.Error()})
	}
	// A pool route can still have no manager assigned when TTL cleanup observes
	// it. This fallback path does not pass through SessionController's durable
	// remote deletion, so explicitly remove its allocation after the workload.
	if wc.allocationCleaner != nil {
		if err := wc.allocationCleaner(c.Request().Context(), publicID); err != nil {
			return c.JSON(http.StatusInternalServerError, map[string]string{"error": err.Error()})
		}
	}
	// Pool-backed sessions use a public route alias for the adopted runtime ID.
	// TTL cleanup must remove that alias after the runtime deletion succeeds or
	// /search continues to expose a session whose workload no longer exists.
	if wc.routes != nil {
		if err := wc.routes.Delete(c.Request().Context(), publicID); err != nil {
			return c.JSON(http.StatusInternalServerError, map[string]string{"error": err.Error()})
		}
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
	route *repositories.SessionRoute
}

func (s *workerAliasSession) ID() string { return s.route.SessionID }

// Tags combines runtime metadata with the durable public route. Route values
// win because they contain the policy attached to the public session, while a
// claimed pool runtime may still expose metadata from its stock lifecycle.
func (s *workerAliasSession) Tags() map[string]string {
	tags := make(map[string]string, len(s.Session.Tags())+len(s.route.Tags))
	for key, value := range s.Session.Tags() {
		tags[key] = value
	}
	for key, value := range s.route.Tags {
		tags[key] = value
	}
	return tags
}

func (s *workerAliasSession) Status() string {
	if s.route.Status != "" {
		return s.route.Status
	}
	return s.Session.Status()
}

func (s *workerAliasSession) UpdatedAt() time.Time {
	if !s.route.StatusUpdatedAt.IsZero() {
		return s.route.StatusUpdatedAt
	}
	return s.Session.UpdatedAt()
}

// Preserve cleanup settings that are not part of the embedded Session interface.
func (s *workerAliasSession) Request() *entities.RunServerRequest {
	var request *entities.RunServerRequest
	if provider, ok := s.Session.(interface {
		Request() *entities.RunServerRequest
	}); ok {
		request = provider.Request()
	}
	if ttl := s.route.Tags["session_ttl"]; ttl != "" {
		if request == nil {
			return &entities.RunServerRequest{SessionTTL: ttl}
		}
		copy := *request
		copy.SessionTTL = ttl
		return &copy
	}
	return request
}

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
