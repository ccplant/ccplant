package controllers

import (
	"context"
	"log"
	"net/http"
	"time"

	"github.com/takutakahashi/agentapi-proxy/internal/usecases/ports/repositories"
)

func (c *SessionController) reconcileQueuedDeletion(ctx context.Context, route *repositories.SessionRoute) bool {
	if route == nil || route.DeletionRequestID == "" || c.sessionRouteRepo == nil {
		return false
	}
	reader, ok := c.esmControlTunnel.(esmControlCommandReader)
	if !ok {
		return false
	}
	done, status, err := reader.CommandResult(ctx, route.DeletionRequestID)
	if !done {
		return false
	}
	if err != nil || (status != http.StatusOK && status != http.StatusNoContent && status != http.StatusNotFound) {
		log.Printf("[REMOTE_DELETE] Manager deletion %s failed (status=%d): %v", route.DeletionRequestID, status, err)
		route.DeletionRequestID = ""
		route.Status = "delete_failed"
		route.StatusUpdatedAt = time.Now()
		_ = c.sessionRouteRepo.Save(ctx, route)
		return false
	}
	usageDimensions := c.resolveSessionUsageDimensions(ctx, route.SessionID, "direct_runtime_queued")
	if route.Status != "terminated" {
		route.Status = "terminated"
		route.StatusUpdatedAt = time.Now()
		if err := c.sessionRouteRepo.Save(ctx, route); err != nil {
			log.Printf("[REMOTE_DELETE] Failed to persist terminal route state %s: %v", route.SessionID, err)
			return false
		}
	}
	c.recordSessionTerminated(ctx, usageDimensions, "direct_runtime_queued", route.StatusUpdatedAt)
	if err := c.sessionRouteRepo.Delete(ctx, route.SessionID); err != nil {
		log.Printf("[REMOTE_DELETE] Failed to finalize route %s: %v", route.SessionID, err)
		return false
	}
	c.cleanupRemoteProvisionRequest(ctx, route.SessionID)
	c.cleanupSessionConfiguration(ctx, route.SessionID)
	log.Printf("[REMOTE_DELETE] Finalized queued deletion for session %s", route.SessionID)
	return true
}
