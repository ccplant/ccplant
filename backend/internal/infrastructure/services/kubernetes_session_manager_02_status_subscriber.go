package services

import (
	"context"
	"log"
	"os"
	"strings"
	"time"

	portrepos "github.com/takutakahashi/agentapi-proxy/internal/usecases/ports/repositories"
)

func resolveKubernetesNamespace(candidates ...string) string {
	for _, candidate := range candidates {
		if namespace := strings.TrimSpace(candidate); namespace != "" {
			return namespace
		}
	}
	if data, err := os.ReadFile("/var/run/secrets/kubernetes.io/serviceaccount/namespace"); err == nil {
		if namespace := strings.TrimSpace(string(data)); namespace != "" {
			return namespace
		}
	}
	return "default"
}

// SetStatusEventRepository injects a StatusEventRepository for cross-pod status
// synchronisation.  It must be called before the manager starts handling traffic.
// If repo is nil the existing no-op fallback is kept.
func (m *KubernetesSessionManager) SetStatusEventRepository(repo portrepos.StatusEventRepository) {
	if repo == nil {
		return
	}
	m.statusEventRepo = repo
	log.Printf("[K8S_SESSION] StatusEventRepository configured (podID=%s)", m.podID)

	// Start the Redis subscriber goroutine that fans out cross-pod events to
	// local SSE subscribers.
	go m.runStatusSubscriber(m.statusSubCtx)
}

// SetSessionListCacheRepository injects a SessionListCacheRepository for
// short-lived session-list caching.  Must be called before the manager starts
// handling traffic.  If repo is nil the call is ignored.
func (m *KubernetesSessionManager) SetSessionListCacheRepository(repo portrepos.SessionListCacheRepository) {
	if repo == nil {
		return
	}
	m.sessionListCacheRepo = repo
	log.Printf("[K8S_SESSION] SessionListCacheRepository configured")
}

// runStatusSubscriber subscribes to the global status-change channel and
// forwards incoming events to local in-process SSE subscribers.
// Events that originated from this pod are ignored (self-dedup).
func (m *KubernetesSessionManager) runStatusSubscriber(ctx context.Context) {
	log.Printf("[K8S_SESSION] Starting cross-pod status subscriber (podID=%s)", m.podID)
	for {
		ch, err := m.statusEventRepo.SubscribeGlobal(ctx)
		if err != nil {
			if ctx.Err() != nil {
				log.Printf("[K8S_SESSION] Status subscriber context cancelled, stopping")
				return
			}
			log.Printf("[K8S_SESSION] Failed to subscribe to global status channel, retrying in 5s: %v", err)
			select {
			case <-ctx.Done():
				return
			case <-time.After(5 * time.Second):
			}
			continue
		}
		log.Printf("[K8S_SESSION] Cross-pod status subscriber connected")
		m.consumeStatusEvents(ctx, ch)
		if ctx.Err() != nil {
			return
		}
		// Channel closed — reconnect after a short back-off.
		log.Printf("[K8S_SESSION] Status subscriber channel closed, reconnecting in 3s")
		select {
		case <-ctx.Done():
			return
		case <-time.After(3 * time.Second):
		}
	}
}

// consumeStatusEvents reads events from ch until it is closed or ctx is cancelled.
// Events from this pod are silently discarded to avoid duplicate broadcasts.
func (m *KubernetesSessionManager) consumeStatusEvents(ctx context.Context, ch <-chan portrepos.StatusChangeEvent) {
	for {
		select {
		case <-ctx.Done():
			return
		case evt, ok := <-ch:
			if !ok {
				return
			}
			// Skip events that were published by this pod — we already broadcast
			// those locally at the time of the status change.
			if evt.PodID == m.podID {
				continue
			}
			log.Printf("[K8S_SESSION] Cross-pod status event: session=%s status=%s from=%s",
				evt.SessionID, evt.Status, evt.PodID)

			// Update in-memory status if the session is loaded on this pod.
			m.mutex.RLock()
			session, exists := m.sessions[evt.SessionID]
			m.mutex.RUnlock()
			if exists {
				session.SetStatusSilentAt(evt.Status, evt.UpdatedAt)
			}

			// Forward to local SSE subscribers only.
			// IMPORTANT: do NOT call broadcastStatusChange here because that
			// function also re-publishes to Redis, which would create an infinite
			// feedback loop across pods (Pod B re-publishes → Pod C processes →
			// Pod C re-publishes → Pod A processes → ...).
			m.broadcastStatusChangeLocal(evt.SessionID, evt.Status)
		}
	}
}

// StopStatusSubscriber cancels the background Redis subscriber goroutine.
// Should be called on graceful shutdown.
func (m *KubernetesSessionManager) StopStatusSubscriber() {
	if m.statusSubCancel != nil {
		m.statusSubCancel()
	}
}

// allocateSessionDirect creates or adopts Kubernetes resources for a session.
