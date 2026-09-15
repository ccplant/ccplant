package controllers

import (
	"context"
	"net/http"
	"strings"

	"github.com/labstack/echo/v4"
	"github.com/takutakahashi/agentapi-proxy/internal/domain/entities"
	"github.com/takutakahashi/agentapi-proxy/pkg/sessionsettings"
)

const maxManagedFileSnapshotBytes = 4 << 20

type managedFileSessionManager interface {
	ValidateSessionControlToken(sessionID, token string) bool
	GetSession(id string) entities.Session
}

type managedFileSnapshotStore interface {
	SaveFiles(context.Context, string, []sessionsettings.ManagedFile) error
}

type ManagedFilesController struct {
	manager managedFileSessionManager
	store   managedFileSnapshotStore
}

func NewManagedFilesController(manager managedFileSessionManager, store managedFileSnapshotStore) *ManagedFilesController {
	return &ManagedFilesController{manager: manager, store: store}
}

func (c *ManagedFilesController) Save(cctx echo.Context) error {
	sessionID := cctx.Param("sessionId")
	token := strings.TrimPrefix(cctx.Request().Header.Get("Authorization"), "Bearer ")
	if c.manager == nil || !c.manager.ValidateSessionControlToken(sessionID, token) {
		return cctx.NoContent(http.StatusUnauthorized)
	}
	session := c.manager.GetSession(sessionID)
	if session == nil || session.UserID() == "" {
		return cctx.NoContent(http.StatusNotFound)
	}
	var req struct {
		Files    []sessionsettings.ManagedFile `json:"files"`
		Expected map[string]string             `json:"expected"`
	}
	if err := cctx.Bind(&req); err != nil {
		return cctx.JSON(http.StatusBadRequest, map[string]string{"error": "invalid managed-file snapshot"})
	}
	total := 0
	allowed := map[string]bool{}
	for _, path := range sessionsettings.ManagedFileTypes {
		allowed[path] = true
	}
	for _, file := range req.Files {
		total += len(file.Content)
		if !allowed[file.Path] || total > maxManagedFileSnapshotBytes {
			return cctx.JSON(http.StatusBadRequest, map[string]string{"error": "invalid managed-file path or snapshot size"})
		}
	}
	owner := session.UserID()
	if validator, ok := c.manager.(interface {
		ManagedFileSyncOwner(context.Context, string, string) (string, error)
	}); ok {
		resolved, err := validator.ManagedFileSyncOwner(cctx.Request().Context(), sessionID, cctx.Request().Header.Get("X-Credential-Sync-ID"))
		if err != nil {
			return cctx.NoContent(http.StatusConflict)
		}
		owner = resolved
	}
	if cctx.Request().Header.Get("X-Credential-Sync-ID") != "" {
		conditional, ok := c.store.(interface {
			SaveFilesIfUnchanged(context.Context, string, []sessionsettings.ManagedFile, map[string]string) error
		})
		if !ok || req.Expected == nil {
			return cctx.NoContent(http.StatusConflict)
		}
		if err := conditional.SaveFilesIfUnchanged(cctx.Request().Context(), owner, req.Files, req.Expected); err != nil {
			return cctx.NoContent(http.StatusConflict)
		}
		return cctx.NoContent(http.StatusNoContent)
	}
	if err := c.store.SaveFiles(cctx.Request().Context(), owner, req.Files); err != nil {
		return cctx.JSON(http.StatusServiceUnavailable, map[string]string{"error": "managed-file persistence unavailable"})
	}
	return cctx.NoContent(http.StatusNoContent)
}
