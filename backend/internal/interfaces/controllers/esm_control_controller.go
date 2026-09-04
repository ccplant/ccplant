package controllers

import (
	"context"
	"net/http"
	"time"

	"github.com/labstack/echo/v4"
	core "github.com/takutakahashi/agentapi-proxy/internal/core/esmcontrol"
	sessionrunner "github.com/takutakahashi/agentapi-proxy/internal/core/sessionrunner"
	"github.com/takutakahashi/agentapi-proxy/pkg/telemetry"
)

type ESMControlController struct {
	store       core.Store
	provisioner *ProvisionerController
	managers    sessionrunner.Store
}

func NewESMControlController(store core.Store, provisioner *ProvisionerController, managerStores ...sessionrunner.Store) *ESMControlController {
	controller := &ESMControlController{store: store, provisioner: provisioner}
	if len(managerStores) > 0 {
		controller.managers = managerStores[0]
	}
	return controller
}

func (c *ESMControlController) authorize(ctx echo.Context) (string, bool) {
	if c == nil {
		return "", false
	}
	pathManagerID := ctx.Param("managerId")
	if c.managers != nil {
		managers, err := c.managers.ListManagers(ctx.Request().Context())
		if err == nil {
			token := bearerToken(ctx.Request())
			for _, manager := range managers {
				if (pathManagerID == "" || manager.ID == pathManagerID) && verifySessionRunnerToken(manager.ConnectionTokenHash, token) {
					return manager.ID, true
				}
			}
		}
	}
	// Preserve authentication for legacy settings-based managers while they
	// migrate to the unified session-manager registry.
	if c.provisioner == nil {
		return "", false
	}
	managerID, _, ok := c.provisioner.authorizedExternalManager(ctx)
	return managerID, ok && (pathManagerID == "" || managerID == pathManagerID)
}

func (c *ESMControlController) WaitCommands(ctx echo.Context) error {
	managerID, ok := c.authorize(ctx)
	if !ok {
		return ctx.NoContent(http.StatusUnauthorized)
	}
	if err := c.store.TouchManager(ctx.Request().Context(), managerID, ctx.QueryParam("instance_id")); err != nil {
		return ctx.JSON(http.StatusServiceUnavailable, map[string]string{"error": err.Error()})
	}
	commands, err := c.store.ReadCommands(ctx.Request().Context(), managerID, ctx.QueryParam("after"), parseWait(ctx.QueryParam("wait")), parseControlCount(ctx.QueryParam("count")))
	if err != nil {
		return ctx.JSON(http.StatusServiceUnavailable, map[string]string{"error": err.Error()})
	}
	if len(commands) == 0 {
		return ctx.NoContent(http.StatusNoContent)
	}
	return ctx.JSON(http.StatusOK, map[string]interface{}{"commands": commands, "next_cursor": commands[len(commands)-1].StreamID})
}

func (c *ESMControlController) AppendFrames(ctx echo.Context) error {
	managerID, ok := c.authorize(ctx)
	if !ok {
		return ctx.NoContent(http.StatusUnauthorized)
	}
	if err := c.store.TouchManager(ctx.Request().Context(), managerID, ctx.QueryParam("instance_id")); err != nil {
		return ctx.JSON(http.StatusServiceUnavailable, map[string]string{"error": err.Error()})
	}
	var request struct {
		Frames []core.ResponseFrame `json:"frames"`
	}
	if err := ctx.Bind(&request); err != nil || len(request.Frames) == 0 || len(request.Frames) > 100 {
		return ctx.JSON(http.StatusBadRequest, map[string]string{"error": "frames must contain between 1 and 100 entries"})
	}
	requestID := request.Frames[0].RequestID
	belongs, err := c.store.RequestBelongsToManager(ctx.Request().Context(), requestID, managerID)
	if err != nil {
		return ctx.JSON(http.StatusServiceUnavailable, map[string]string{"error": err.Error()})
	}
	if !belongs {
		return ctx.NoContent(http.StatusForbidden)
	}
	for i := range request.Frames {
		if request.Frames[i].RequestID != requestID {
			return ctx.JSON(http.StatusBadRequest, map[string]string{"error": "all frames must have the same request_id"})
		}
		if request.Frames[i].ID == "" {
			return ctx.JSON(http.StatusBadRequest, map[string]string{"error": "frame id is required"})
		}
		if request.Frames[i].CreatedAt.IsZero() {
			request.Frames[i].CreatedAt = time.Now().UTC()
		}
	}
	last, err := telemetry.Operation(ctx.Request().Context(), "esmcontrol.Store.AppendFrames", func(operationCtx context.Context) (string, error) {
		return c.store.AppendFrames(operationCtx, requestID, request.Frames)
	}, telemetry.Int64("esm.response.frame_count", int64(len(request.Frames))), telemetry.Int64("esm.response.body.size", responseFrameBytes(request.Frames)))
	if err != nil {
		return ctx.JSON(http.StatusServiceUnavailable, map[string]string{"error": err.Error()})
	}
	for _, frame := range request.Frames {
		if frame.CommandStreamID != "" {
			if err := c.store.AckCommand(ctx.Request().Context(), managerID, frame.CommandStreamID); err != nil {
				return ctx.JSON(http.StatusServiceUnavailable, map[string]string{"error": err.Error()})
			}
		}
	}
	return ctx.JSON(http.StatusAccepted, map[string]string{"accepted_through": last})
}
