package controllers

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log"
	"net/http"
	"sync"
	"time"

	"github.com/takutakahashi/agentapi-proxy/internal/domain/entities"
	"github.com/takutakahashi/agentapi-proxy/internal/usecases/ports/repositories"
)

func routedSessionStatus(route *repositories.SessionRoute, allocatedSessions map[string]entities.Session) string {
	if route.Transport == repositories.SessionRouteTransportDirectRuntime && route.Status != "" {
		return route.Status
	}
	if route.RemoteSessionID == "" {
		return "creating"
	}
	if allocatedSession := allocatedSessions[route.RemoteSessionID]; allocatedSession != nil {
		return allocatedSession.Status()
	}
	if route.Status != "" {
		return route.Status
	}
	return "starting"
}

// RecordRemoteSessionStatus persists a status pushed by a direct runtime and
// immediately fans it out to the proxy-wide SSE subscribers.
func (c *SessionController) RecordRemoteSessionStatus(ctx context.Context, route *repositories.SessionRoute, runtimeStatus string) error {
	if route == nil || runtimeStatus == "" {
		return nil
	}
	if route.DeletionRequestID != "" {
		// A late status push from a terminating Pod must not resurrect the route.
		return nil
	}
	status := publicSessionStatus(runtimeStatus)
	previous := route.Status
	// StatusUpdatedAt is the completion timestamp used by session TTL cleanup.
	// Repeated /status reads and runtime retries must not move that deadline.
	if previous == status && !route.StatusUpdatedAt.IsZero() {
		return nil
	}
	route.Status, route.StatusUpdatedAt = status, time.Now()
	if err := c.sessionRouteRepo.Save(ctx, route); err != nil {
		return err
	}
	if previous != status {
		event := repositories.SessionStatusEvent{SessionID: route.SessionID, Status: status, Timestamp: route.StatusUpdatedAt}
		c.recordSessionStatusUsage(ctx, event)
		c.publishRemoteStatusEvent(event)
	}
	return nil
}

func (c *SessionController) recordSessionStatusUsage(ctx context.Context, event repositories.SessionStatusEvent) {
	if c.sessionStatusRecorder == nil {
		return
	}
	if err := c.sessionStatusRecorder.RecordStatus(ctx, event); err != nil {
		log.Printf("[SESSION_COUNT_EVENTS] Failed to record status for %s: %v", event.SessionID, err)
	}
}

func (c *SessionController) resolveSessionUsageDimensions(ctx context.Context, sessionID, deletePath string) *repositories.SessionUsageDimensions {
	if c.sessionStatusRecorder == nil {
		return nil
	}
	dimensions, err := c.sessionStatusRecorder.ResolveDimensions(ctx, sessionID)
	if err != nil {
		log.Printf("[SESSION_COUNT_EVENTS] session_usage_dimensions_resolve_failed session=%s delete_path=%s: %v", sessionID, deletePath, err)
		return nil
	}
	return &dimensions
}

func (c *SessionController) recordSessionTerminated(ctx context.Context, dimensions *repositories.SessionUsageDimensions, deletePath string, timestamp time.Time) {
	if c.sessionStatusRecorder == nil || dimensions == nil {
		return
	}
	if !dimensions.LastStatusAt.IsZero() && !timestamp.After(dimensions.LastStatusAt) {
		timestamp = dimensions.LastStatusAt.Add(time.Nanosecond)
	}
	event := repositories.SessionStatusEvent{SessionID: dimensions.SessionID, Status: "terminated", Timestamp: timestamp}
	if err := c.sessionStatusRecorder.RecordResolvedStatus(ctx, *dimensions, event); err != nil {
		log.Printf("[SESSION_COUNT_EVENTS] session_usage_terminal_event_failed session=%s delete_path=%s: %v", dimensions.SessionID, deletePath, err)
		return
	}
	log.Printf("[SESSION_COUNT_EVENTS] session_usage_terminal_event_recorded session=%s pool=%s scope=%s delete_path=%s", dimensions.SessionID, dimensions.Pool, dimensions.Scope, deletePath)
}

func (c *SessionController) rememberRemoteSessionStatus(ctx context.Context, route *repositories.SessionRoute, resp *http.Response) {
	if c.sessionRouteRepo == nil || resp == nil || resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 64<<10))
	if err != nil {
		return
	}
	resp.Body = io.NopCloser(bytes.NewReader(body))
	var payload struct {
		Status string `json:"status"`
	}
	if json.Unmarshal(body, &payload) != nil || payload.Status == "" {
		return
	}
	if err := c.RecordRemoteSessionStatus(ctx, route, payload.Status); err != nil {
		log.Printf("[ROUTE] Failed to persist status for %s: %v", route.SessionID, err)
	}
}

func publicSessionStatus(runtimeStatus string) string {
	if runtimeStatus == "stable" {
		return "active"
	}
	return runtimeStatus
}

func (c *SessionController) subscribeRemoteStatusEvents() (<-chan repositories.SessionStatusEvent, func()) {
	c.statusSubscribersMu.Lock()
	c.nextStatusSubscriberID++
	id := c.nextStatusSubscriberID
	ch := make(chan repositories.SessionStatusEvent, 32)
	c.statusSubscribers[id] = ch
	c.statusSubscribersMu.Unlock()

	var once sync.Once
	return ch, func() {
		once.Do(func() {
			c.statusSubscribersMu.Lock()
			delete(c.statusSubscribers, id)
			c.statusSubscribersMu.Unlock()
		})
	}
}

func (c *SessionController) publishRemoteStatusEvent(evt repositories.SessionStatusEvent) {
	c.deliverRemoteStatusEventLocal(evt)
	c.enqueueRouteStatusEvent(evt)
}

func (c *SessionController) deliverRemoteStatusEventLocal(evt repositories.SessionStatusEvent) {
	c.statusSubscribersMu.RLock()
	defer c.statusSubscribersMu.RUnlock()
	for _, ch := range c.statusSubscribers {
		select {
		case ch <- evt:
		default:
		}
	}
}

func indexAllocatedSessions(sessions []entities.Session, routes []*repositories.SessionRoute) map[string]entities.Session {
	allocatedIDs := make(map[string]struct{}, len(routes))
	for _, route := range routes {
		if route.RemoteSessionID != "" && route.RemoteSessionID != route.SessionID {
			allocatedIDs[route.RemoteSessionID] = struct{}{}
		}
	}

	allocatedSessions := make(map[string]entities.Session, len(allocatedIDs))
	for _, session := range sessions {
		if _, allocated := allocatedIDs[session.ID()]; allocated {
			allocatedSessions[session.ID()] = session
		}
	}
	return allocatedSessions
}

func excludeAllocatedSessions(sessions []entities.Session, routes []*repositories.SessionRoute) []entities.Session {
	allocatedIDs := make(map[string]struct{}, len(routes))
	for _, route := range routes {
		if route.RemoteSessionID != "" && route.RemoteSessionID != route.SessionID {
			allocatedIDs[route.RemoteSessionID] = struct{}{}
		}
	}

	filtered := make([]entities.Session, 0, len(sessions))
	for _, session := range sessions {
		if _, allocated := allocatedIDs[session.ID()]; !allocated {
			filtered = append(filtered, session)
		}
	}
	return filtered
}

func excludeTemplatedSessions(sessions []entities.Session, routes []*repositories.SessionRoute) []entities.Session {
	templatedIDs := make(map[string]struct{})
	for _, route := range routes {
		if route.Status != "templated" {
			continue
		}
		templatedIDs[route.SessionID] = struct{}{}
		if route.RemoteSessionID != "" {
			templatedIDs[route.RemoteSessionID] = struct{}{}
		}
	}
	if len(templatedIDs) == 0 {
		return sessions
	}

	filtered := make([]entities.Session, 0, len(sessions))
	for _, session := range sessions {
		if _, templated := templatedIDs[session.ID()]; !templated {
			filtered = append(filtered, session)
		}
	}
	return filtered
}

func getSessionAnnotations(session entities.Session) entities.SessionAnnotations {
	if annotated, ok := session.(sessionAnnotationsProvider); ok {
		return annotated.Annotations()
	}
	return entities.SessionAnnotations{}
}

// UpdateSessionAnnotations handles PATCH /sessions/:sessionId/annotations.
