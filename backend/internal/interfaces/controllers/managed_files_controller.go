package controllers

import (
	"context"
	"errors"
	"net/http"
	"strings"

	"github.com/labstack/echo/v4"
	"github.com/takutakahashi/agentapi-proxy/internal/domain/entities"
	"github.com/takutakahashi/agentapi-proxy/pkg/sessionsettings"
)

var ErrManagedFileConflict = errors.New("managed-file snapshot conflict")

const maxManagedFileSnapshotBytes = 4 << 20

type managedFileSessionManager interface {
	ValidateSessionControlToken(sessionID, token string) bool
	GetSession(id string) entities.Session
}

type managedFileSnapshotStore interface {
	SaveFiles(context.Context, string, []sessionsettings.ManagedFile) error
}

// ManagedFileSnapshotProxy persists an ESM-managed session's file snapshot in
// the parent control plane after the ESM validates the local session.
type ManagedFileSnapshotProxy interface {
	SaveManagedFiles(context.Context, string, string, string, []sessionsettings.ManagedFile, map[string]string) error
}

type ManagedFilesController struct {
	manager managedFileSessionManager
	store   managedFileSnapshotStore
	proxy   ManagedFileSnapshotProxy
}

func NewManagedFilesController(manager managedFileSessionManager, store managedFileSnapshotStore) *ManagedFilesController {
	return &ManagedFilesController{manager: manager, store: store}
}

func (c *ManagedFilesController) WithProxy(proxy ManagedFileSnapshotProxy) *ManagedFilesController {
	c.proxy = proxy
	return c
}

func (c *ManagedFilesController) Save(cctx echo.Context) error {
	sessionID := cctx.Param("sessionId")
	token := cctx.Request().Header.Get("X-Session-Control-Token")
	customTokenPresent := token != ""
	if token == "" {
		token = strings.TrimPrefix(cctx.Request().Header.Get("Authorization"), "Bearer ")
	}
	if c.manager == nil || !c.manager.ValidateSessionControlToken(sessionID, token) {
		return cctx.JSON(http.StatusUnauthorized, map[string]any{
			"error":                 "session control token rejected",
			"session_id":            sessionID,
			"custom_token_present":  customTokenPresent,
			"authorization_present": cctx.Request().Header.Get("Authorization") != "",
		})
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
		if c.proxy != nil {
			if req.Expected == nil {
				return cctx.NoContent(http.StatusConflict)
			}
			if err := c.proxy.SaveManagedFiles(cctx.Request().Context(), sessionID, owner, cctx.Request().Header.Get("X-Credential-Sync-ID"), req.Files, req.Expected); errors.Is(err, ErrManagedFileConflict) {
				return cctx.NoContent(http.StatusConflict)
			} else if err != nil {
				return cctx.JSON(http.StatusServiceUnavailable, map[string]string{"error": "managed-file persistence unavailable"})
			}
			return cctx.NoContent(http.StatusNoContent)
		}
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
	if c.proxy != nil {
		if err := c.proxy.SaveManagedFiles(cctx.Request().Context(), sessionID, owner, "", req.Files, nil); err != nil {
			return cctx.JSON(http.StatusServiceUnavailable, map[string]string{"error": "managed-file persistence unavailable"})
		}
		return cctx.NoContent(http.StatusNoContent)
	}
	if err := c.store.SaveFiles(cctx.Request().Context(), owner, req.Files); err != nil {
		return cctx.JSON(http.StatusServiceUnavailable, map[string]string{"error": "managed-file persistence unavailable"})
	}
	return cctx.NoContent(http.StatusNoContent)
}
