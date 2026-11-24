package services

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"time"

	"github.com/google/uuid"
	coresessioncontrol "github.com/takutakahashi/agentapi-proxy/internal/core/sessioncontrol"
	"github.com/takutakahashi/agentapi-proxy/internal/domain/entities"
	"github.com/takutakahashi/agentapi-proxy/pkg/sessionsettings"
	"github.com/takutakahashi/agentapi-proxy/pkg/telemetry"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// DeleteSession stops and removes a session
// If the session is not in memory, it attempts to restore from Kubernetes Service first
func (m *KubernetesSessionManager) DeleteSession(id string) error {
	// First, check memory
	m.mutex.RLock()
	session, exists := m.sessions[id]
	m.mutex.RUnlock()

	// If not in memory, try to get from GetSession (which will restore from Service)
	if !exists {
		if restored := m.GetSession(id); restored != nil {
			m.mutex.RLock()
			session, exists = m.sessions[id]
			m.mutex.RUnlock()
		}
	}

	if !exists || session == nil {
		return fmt.Errorf("session not found: %s", id)
	}

	log.Printf("[K8S_SESSION] Deleting session %s", id)

	// Invoke registered handlers BEFORE cancelling the context or removing Kubernetes resources.
	// At this point the session's Service endpoint is still reachable (e.g. for GetMessages).
	m.handlersMutex.RLock()
	handlers := make([]SessionDeletedHandler, len(m.onSessionDeletedHandlers))
	copy(handlers, m.onSessionDeletedHandlers)
	m.handlersMutex.RUnlock()

	if len(handlers) > 0 {
		handlerCtx, handlerCancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer handlerCancel()
		for _, h := range handlers {
			h(handlerCtx, session)
		}
	}

	// Cancel context to trigger cleanup
	if session != nil {
		session.Cancel()
	}

	// Delete Kubernetes resources
	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(m.k8sConfig.PodStopTimeout)*time.Second)
	defer cancel()

	if err := m.deleteSessionResources(ctx, session); err != nil {
		log.Printf("[K8S_SESSION] Warning: failed to delete session resources: %v", err)
	}

	// Remove session from map
	m.cleanupSession(id)

	// Clean up Redis status key.
	if m.statusEventRepo != nil {
		delCtx, delCancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer delCancel()
		if err := m.statusEventRepo.DeleteStatus(delCtx, id); err != nil {
			log.Printf("[K8S_SESSION] Warning: failed to delete Redis status for session=%s: %v", id, err)
		}
	}

	// Remove the deleted session from cache instead of invalidating all entries.
	// This is more efficient and preserves cache hits for other sessions.
	if m.sessionListCacheRepo != nil {
		invCtx, invCancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer invCancel()
		if err := m.sessionListCacheRepo.DeleteSessionFromCache(invCtx, m.namespace, id, redisSessionListCacheTTL); err != nil {
			log.Printf("[K8S_SESSION] Warning: failed to delete session from cache: %v", err)
		}
	}

	// Log session end
	if err := m.logger.LogSessionEnd(id, 0); err != nil {
		log.Printf("[K8S_SESSION] Failed to log session end: %v", err)
	}

	log.Printf("[K8S_SESSION] Session %s deleted successfully", id)
	return nil
}

// Shutdown gracefully stops all sessions
// Note: This does NOT delete Kubernetes resources (Deployment, Service, PVC, Secret).
// Resources are preserved so sessions can be restored when the proxy restarts.
// Use DeleteSession to explicitly delete a session and its resources.
func (m *KubernetesSessionManager) Shutdown(timeout time.Duration) error {
	if m.sessionInformerStop != nil {
		m.sessionInformerStop()
	}
	if m.suspendCancel != nil {
		m.suspendCancel()
	}
	m.mutex.Lock()
	sessionCount := len(m.sessions)
	// Clear in-memory sessions (resources remain in Kubernetes)
	m.sessions = make(map[string]*KubernetesSession)
	m.mutex.Unlock()

	log.Printf("[K8S_SESSION] Shutting down, preserving %d session(s) in Kubernetes for recovery", sessionCount)
	return nil
}

// SendMessage sends a message to an existing session.
// For ACP sessions (claude-acp, codex-acp, pi, cursor, antigravity) it uses the ACP JSON-RPC 2.0
// POST /rpc endpoint with session/prompt; for standard agentapi sessions it
// uses the agentapi-compatible POST /message endpoint.
func (m *KubernetesSessionManager) SendMessage(ctx context.Context, id string, message string) error {
	if ks, ok := m.GetSession(id).(*KubernetesSession); ok && ks != nil {
		svc, err := telemetry.Operation(ctx, "sessionmanager.SendMessage.GetService", func(operationCtx context.Context) (*corev1.Service, error) {
			return m.client.CoreV1().Services(m.namespace).Get(operationCtx, ks.ServiceName(), metav1.GetOptions{})
		}, telemetry.String("session.id", id), telemetry.String("k8s.service.name", ks.ServiceName()))
		if err != nil {
			return err
		}
		if svc.Annotations[restartHoldAnnotation] == "true" {
			return fmt.Errorf("session is paused or restarting")
		}
	}

	session := m.GetSession(id)
	if session == nil {
		return fmt.Errorf("session not found: %s", id)
	}

	status := session.Status()
	if status != "active" && status != "starting" && status != "running" {
		return fmt.Errorf("session is not active: status=%s", status)
	}
	store := m.connectedSessionControlStore(ctx, id)
	if store == nil {
		return fmt.Errorf("session outbound control connection is unavailable")
	}
	telemetry.SetAttributes(ctx, telemetry.String("session.message_transport", "control_store"))
	payload, err := json.Marshal(map[string]string{"content": message})
	if err != nil {
		return fmt.Errorf("marshal session control prompt: %w", err)
	}
	_, err = telemetry.Operation(ctx, "sessionmanager.SendMessage.EnqueueControlPrompt", func(operationCtx context.Context) (string, error) {
		return store.EnqueueCommand(operationCtx, id, coresessioncontrol.Command{ID: uuid.NewString(), Type: "prompt", Payload: payload, CreatedAt: time.Now().UTC()})
	}, telemetry.String("session.id", id))
	if err != nil {
		return fmt.Errorf("enqueue session control prompt: %w", err)
	}
	return nil
}

// StopAgent queues a cancellation for the session's outbound control client.
func (m *KubernetesSessionManager) StopAgent(ctx context.Context, id string) error {
	// Get session
	session := m.GetSession(id)
	if session == nil {
		return fmt.Errorf("session not found: %s", id)
	}

	// Check session status
	status := session.Status()
	if status != "active" && status != "starting" && status != "running" {
		return fmt.Errorf("session is not active: status=%s", status)
	}
	store := m.connectedSessionControlStore(ctx, id)
	if store == nil {
		return fmt.Errorf("session outbound control connection is unavailable")
	}
	_, err := store.EnqueueCommand(ctx, id, coresessioncontrol.Command{ID: uuid.NewString(), Type: "cancel", CreatedAt: time.Now().UTC()})
	if err != nil {
		return fmt.Errorf("enqueue session control cancel: %w", err)
	}
	return nil
}

func (m *KubernetesSessionManager) getSessionControlStore() coresessioncontrol.Store {
	m.mutex.RLock()
	defer m.mutex.RUnlock()
	return m.sessionControlStore
}

func (m *KubernetesSessionManager) connectedSessionControlStore(ctx context.Context, sessionID string) coresessioncontrol.Store {
	store := m.getSessionControlStore()
	if store == nil {
		return nil
	}
	connectionCtx, cancel := context.WithTimeout(ctx, sessionControlConnectionTimeout)
	defer cancel()
	connected, err := telemetry.Operation(connectionCtx, "sessionmanager.CheckControlConnection", func(operationCtx context.Context) (bool, error) {
		connected, err := store.IsConnected(operationCtx, sessionID)
		telemetry.SetAttributes(operationCtx, telemetry.Bool("session.control_connected", connected))
		return connected, err
	}, telemetry.String("session.id", sessionID))
	if err != nil {
		log.Printf("[SESSION_CONTROL] Failed to check outbound connection for session %s: %v", sessionID, err)
		return nil
	}
	if !connected {
		return nil
	}
	return store
}

func isACPAgentType(agentType string) bool {
	return agentType == "claude-acp" || agentType == "codex-acp" || agentType == "pi" || agentType == "cursor" || agentType == "antigravity"
}

func supportedAgentTypeOrDefault(agentType string) string {
	switch agentType {
	case "claude-acp", "codex-acp", "pi", "cursor", "antigravity":
		return agentType
	default:
		return ""
	}
}

func credentialOwnersForRequest(req *entities.RunServerRequest) []string {
	if req.SettingsTeamID != "" {
		return []string{req.SettingsTeamID}
	}
	owners := make([]string, 0, 2)
	switch req.CredentialSource {
	case "session_user":
		owners = append(owners, req.UserID)
	case "triggered_user", "github_sender":
		if req.TriggeredUserID != "" {
			owners = append(owners, req.TriggeredUserID)
		}
		if req.Scope == entities.ScopeTeam && req.TeamID != "" {
			owners = append(owners, req.TeamID)
		}
	case "team":
		owners = append(owners, req.TeamID)
	case "none":
	case "":
		if req.Scope == entities.ScopeUser || req.Scope == "" {
			owners = append(owners, req.UserID)
		}
	}
	return owners
}

// resolveAutoAgentType selects Codex only when the credentials that will be
// mounted into the session contain ~/.codex/auth.json. All other cases use Claude.
func (m *KubernetesSessionManager) resolveAutoAgentType(ctx context.Context, req *entities.RunServerRequest) string {
	if req.AgentType == "" {
		req.AgentType = m.defaultAgentTypeForRequest(ctx, req)
	}
	if req.AgentType != "auto" {
		return req.AgentType
	}
	if req.CodexConnection.Compatible() {
		return "codex-acp"
	}
	for _, owner := range credentialOwnersForRequest(req) {
		if owner == "" {
			continue
		}
		files, _ := m.loadCredentialFiles(ctx, owner)
		if len(files) == 0 {
			continue
		}
		for _, file := range files {
			if file.Path == sessionsettings.ManagedFileTypes[sessionsettings.FileTypeCodexAuth] {
				log.Printf("[K8S_SESSION] Resolved auto agent type to codex-acp for credential owner %s", owner)
				return "codex-acp"
			}
		}
		// buildSessionSettings mounts the first owner's valid file set only, so a
		// later fallback owner's Codex credentials would not be available.
		break
	}
	log.Printf("[K8S_SESSION] Resolved auto agent type to claude-acp (Codex auth.json not found)")
	return "claude-acp"
}

func (m *KubernetesSessionManager) defaultAgentTypeForRequest(ctx context.Context, req *entities.RunServerRequest) string {
	settingsName := ""
	if req.SettingsTeamID != "" {
		settingsName = req.SettingsTeamID
	} else if req.Scope == entities.ScopeTeam {
		settingsName = req.TeamID
	} else {
		settingsName = req.UserID
	}
	if settingsName != "" && m.settingsRepo != nil {
		settings, err := m.settingsRepo.FindByName(ctx, settingsName)
		if err != nil {
			log.Printf("[K8S_SESSION] Warning: failed to read default agent type for %s: %v", settingsName, err)
		} else if agentType := settings.DefaultAgentType(); agentType != "" {
			return agentType
		}
	}
	return "auto"
}

func restoreAgentTypeFromService(svc *corev1.Service) string {
	if svc == nil {
		return ""
	}
	if agentType := svc.Annotations["agentapi.proxy/agent-type"]; agentType != "" {
		return agentType
	}
	return svc.Labels["agentapi.proxy/agent-type"]
}
