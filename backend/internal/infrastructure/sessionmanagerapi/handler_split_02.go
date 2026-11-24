package sessionmanagerapi

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/labstack/echo/v4"
	coreallocation "github.com/takutakahashi/agentapi-proxy/internal/core/sessionallocation"
	"github.com/takutakahashi/agentapi-proxy/internal/domain/entities"
	portrepos "github.com/takutakahashi/agentapi-proxy/internal/usecases/ports/repositories"
	"github.com/takutakahashi/agentapi-proxy/pkg/sessionsettings"
)

func (h *Handler) updateAnnotations(c echo.Context) error {
	updater, ok := h.manager.(SessionAnnotationUpdater)
	if !ok {
		return unsupported(c, "session annotations are not supported")
	}
	var patch entities.UpdateSessionAnnotationsRequest
	if err := decodeJSON(c, &patch); err != nil {
		return badRequest(c, err)
	}
	annotations, err := updater.UpdateSessionAnnotations(c.Request().Context(), c.Param("sessionId"), patch)
	if err != nil {
		return internalError(c, err)
	}
	return c.JSON(http.StatusOK, annotationsResponse{Annotations: annotations})
}

func (h *Handler) stock(c echo.Context) error {
	manager, ok := h.manager.(StockManager)
	if !ok {
		return unsupported(c, "stock sessions are not supported")
	}
	dind, _ := strconv.ParseBool(c.QueryParam("dind"))
	pool := strings.TrimSpace(c.QueryParam("pool"))
	switch c.Request().Method {
	case http.MethodGet:
		var count int
		var err error
		if pool != "" {
			named, supported := h.manager.(PoolStockManager)
			if !supported {
				return unsupported(c, "named stock pools are not supported")
			}
			count, err = named.CountStockSessionsForPool(c.Request().Context(), pool, dind)
		} else {
			count, err = manager.CountStockSessions(c.Request().Context(), dind)
		}
		if err != nil {
			return internalError(c, err)
		}
		return c.JSON(http.StatusOK, stockCountResponse{Count: count})
	case http.MethodPost:
		var err error
		if pool != "" {
			named, supported := h.manager.(PoolStockManager)
			if !supported {
				return unsupported(c, "named stock pools are not supported")
			}
			err = named.CreateStockSessionForPool(c.Request().Context(), pool, dind)
		} else {
			err = manager.CreateStockSession(c.Request().Context(), dind)
		}
		if err != nil {
			return internalError(c, err)
		}
		return c.NoContent(http.StatusCreated)
	case http.MethodDelete:
		if err := manager.PurgeStaleStockSessions(c.Request().Context()); err != nil {
			return internalError(c, err)
		}
		return c.NoContent(http.StatusNoContent)
	default:
		return c.NoContent(http.StatusMethodNotAllowed)
	}
}

func (h *Handler) deletePendingAllocation(c echo.Context) error {
	deleter, ok := h.manager.(PendingAllocationDeleter)
	if !ok {
		return unsupported(c, "pending allocation deletion is not supported")
	}
	deleted, err := deleter.DeletePendingSessionAllocation(c.Request().Context(), c.Param("sessionId"))
	if err != nil {
		return internalError(c, err)
	}
	return c.JSON(http.StatusOK, pendingAllocationDeleteResponse{Deleted: deleted})
}

func (h *Handler) deleteProvisionRequest(c echo.Context) error {
	deleter, ok := h.manager.(ProvisionRequestDeleter)
	if !ok {
		return unsupported(c, "provision request deletion is not supported")
	}
	if err := deleter.DeleteProvisionRequest(c.Request().Context(), c.Param("sessionId")); err != nil {
		return internalError(c, err)
	}
	return c.NoContent(http.StatusNoContent)
}

func (h *Handler) submitExternalAllocation(c echo.Context) error {
	queue, ok := h.manager.(coreallocation.Queue)
	if !ok {
		return unsupported(c, "session allocation queue is not supported")
	}
	var input submitExternalAllocationRequest
	if err := decodeJSON(c, &input); err != nil {
		return badRequest(c, err)
	}
	if input.ManagerID == "" || input.Request == nil {
		return badRequest(c, errors.New("manager_id and request are required"))
	}
	if err := queue.SubmitExternalSessionAllocation(c.Request().Context(), input.ManagerID, c.Param("sessionId"), input.ProvisionSettings, input.Request, input.Runtime); err != nil {
		return internalError(c, err)
	}
	return c.NoContent(http.StatusAccepted)
}

func (h *Handler) nextAllocation(c echo.Context) error {
	queue, ok := h.manager.(coreallocation.Queue)
	if !ok {
		return unsupported(c, "session allocation queue is not supported")
	}
	allocation, found, err := queue.NextSessionAllocation(c.Request().Context(), parseWait(c.QueryParam("wait")))
	if err != nil {
		return internalError(c, err)
	}
	if !found {
		return c.NoContent(http.StatusNoContent)
	}
	return c.JSON(http.StatusOK, allocation)
}

func (h *Handler) completeAllocation(c echo.Context) error {
	queue, ok := h.manager.(coreallocation.Queue)
	if !ok {
		return unsupported(c, "session allocation queue is not supported")
	}
	var result coreallocation.AllocationResult
	if err := decodeJSON(c, &result); err != nil {
		return badRequest(c, err)
	}
	allocation, err := queue.CompleteSessionAllocation(c.Request().Context(), c.Param("sessionId"), result)
	if err != nil {
		return internalError(c, err)
	}
	return c.JSON(http.StatusOK, allocation)
}

func (h *Handler) nextExternalAllocation(c echo.Context) error {
	queue, ok := h.manager.(coreallocation.Queue)
	if !ok {
		return unsupported(c, "session allocation queue is not supported")
	}
	managerID := c.QueryParam("manager_id")
	if managerID == "" {
		return badRequest(c, errors.New("manager_id is required"))
	}
	allocation, found, err := queue.NextExternalSessionAllocation(c.Request().Context(), managerID, parseWait(c.QueryParam("wait")))
	if err != nil {
		return internalError(c, err)
	}
	if !found {
		return c.NoContent(http.StatusNoContent)
	}
	return c.JSON(http.StatusOK, allocation)
}

func (h *Handler) completeExternalAllocation(c echo.Context) error {
	queue, ok := h.manager.(coreallocation.Queue)
	if !ok {
		return unsupported(c, "session allocation queue is not supported")
	}
	var result coreallocation.AllocationResult
	if err := decodeJSON(c, &result); err != nil {
		return badRequest(c, err)
	}
	allocation, err := queue.CompleteExternalSessionAllocation(c.Request().Context(), c.Param("sessionId"), result)
	if err != nil {
		return internalError(c, err)
	}
	return c.JSON(http.StatusOK, allocation)
}

func decodeJSON(c echo.Context, target any) error {
	body := http.MaxBytesReader(c.Response(), c.Request().Body, maxRequestBodyBytes)
	decoder := json.NewDecoder(body)
	if err := decoder.Decode(target); err != nil {
		return fmt.Errorf("invalid JSON body: %w", err)
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		if err == nil {
			return errors.New("request body must contain exactly one JSON value")
		}
		return fmt.Errorf("invalid trailing JSON: %w", err)
	}
	return nil
}

func parseWait(value string) time.Duration {
	if value == "" {
		return 0
	}
	wait, err := time.ParseDuration(value)
	if err != nil {
		seconds, intErr := strconv.Atoi(value)
		if intErr != nil || seconds <= 0 {
			return 0
		}
		wait = time.Duration(seconds) * time.Second
	}
	if wait < 0 {
		return 0
	}
	if wait > 30*time.Second {
		return 30 * time.Second
	}
	return wait
}

func splitNonEmpty(value string) []string {
	if value == "" {
		return nil
	}
	parts := strings.Split(value, ",")
	result := make([]string, 0, len(parts))
	for _, part := range parts {
		if part = strings.TrimSpace(part); part != "" {
			result = append(result, part)
		}
	}
	return result
}

func badRequest(c echo.Context, err error) error {
	return c.JSON(http.StatusBadRequest, errorResponse{Error: err.Error()})
}

func internalError(c echo.Context, err error) error {
	return c.JSON(http.StatusInternalServerError, errorResponse{Error: err.Error()})
}

func unsupported(c echo.Context, message string) error {
	return c.JSON(http.StatusNotImplemented, errorResponse{Error: message})
}

func (h *Handler) restartSession(c echo.Context) error {
	m, ok := h.manager.(portrepos.SessionRestarter)
	if !ok {
		return unsupported(c, "restart unavailable")
	}
	var req struct {
		ID       string
		Settings *sessionsettings.SessionSettings
	}
	if err := json.NewDecoder(c.Request().Body).Decode(&req); err != nil || req.ID == "" || req.Settings == nil {
		return echo.NewHTTPError(400, "invalid restart request")
	}
	if err := m.RestartSession(c.Request().Context(), c.Param("sessionId"), req.ID, req.Settings); err != nil {
		return internalError(c, err)
	}
	return c.NoContent(204)
}
func (h *Handler) validateRestart(c echo.Context) error {
	m, ok := h.manager.(portrepos.SessionRestarter)
	if !ok {
		return unsupported(c, "restart unavailable")
	}
	var settings sessionsettings.SessionSettings
	if err := json.NewDecoder(c.Request().Body).Decode(&settings); err != nil {
		return echo.NewHTTPError(400, "invalid settings")
	}
	if err := m.ValidateSessionRestart(c.Request().Context(), c.Param("sessionId"), &settings); err != nil {
		return echo.NewHTTPError(422, err.Error())
	}
	return c.NoContent(204)
}
func (h *Handler) currentSettings(c echo.Context) error {
	m, ok := h.manager.(portrepos.SessionRestarter)
	if !ok {
		return unsupported(c, "restart unavailable")
	}
	settings, err := m.CurrentSessionSettings(c.Request().Context(), c.Param("sessionId"))
	if err != nil {
		return internalError(c, err)
	}
	return c.JSON(200, settings)
}
func (h *Handler) pauseSession(c echo.Context) error {
	m, ok := h.manager.(portrepos.SessionRestarter)
	if !ok {
		return unsupported(c, "pause unavailable")
	}
	if err := m.PauseSession(c.Request().Context(), c.Param("sessionId")); err != nil {
		return internalError(c, err)
	}
	return c.NoContent(204)
}
