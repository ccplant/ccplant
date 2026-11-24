package services

import (
	"context"
	"fmt"
	"log"

	"github.com/takutakahashi/agentapi-proxy/internal/domain/entities"
	"github.com/takutakahashi/agentapi-proxy/pkg/sessionsettings"
	"github.com/takutakahashi/agentapi-proxy/pkg/telemetry"
)

// It first attempts to use a pre-warmed stock session (labeled agentapi.proxy/stock=true).
// If no stock is available, a new session is created from scratch.
func (m *KubernetesSessionManager) allocateSessionDirect(ctx context.Context, id string, req *entities.RunServerRequest, webhookPayload []byte) (entities.Session, error) {
	return telemetry.Operation(ctx, "services.KubernetesSessionManager.allocateSessionDirect", func(ctx context.Context) (entities.Session, error) {
		return m.allocateSessionResources(ctx, id, req, webhookPayload)
	})
}

func (m *KubernetesSessionManager) allocateSessionResources(ctx context.Context, id string, req *entities.RunServerRequest, webhookPayload []byte) (entities.Session, error) {
	m.refreshConfig()
	if err := m.prepareModelConnections(ctx, req); err != nil {
		return nil, err
	}
	req.AgentType = m.resolveAutoAgentType(ctx, req)
	req.AgentType = supportedAgentTypeOrDefault(req.AgentType)
	req.Storages = filterManagedStoragesForAgent(req.Storages, req.AgentType)
	applySelectedAgentDefaultModel(req)
	applySandboxDefaults(req)

	// Storage volumes cannot be added to an already-running stock Pod. Only
	// storage-free requests are eligible for stock adoption.
	if len(req.Storages) == 0 {
		if stockSvc, err := m.findStockSession(ctx, sessionRequirements(req)); err != nil {
			log.Printf("[K8S_SESSION] Warning: failed to search for stock sessions: %v", err)
		} else if stockSvc != nil {
			claimedSvc, claimErr := m.claimStockService(ctx, stockSvc)
			if claimErr != nil {
				log.Printf("[K8S_SESSION] Stock session claim failed (concurrent claim?), falling back to new session creation: %v", claimErr)
			} else {
				log.Printf("[K8S_SESSION] Found stock session %s, adopting for new request", claimedSvc.Labels["agentapi.proxy/session-id"])
				return m.adoptStockSession(ctx, req, webhookPayload, claimedSvc)
			}
		}
	}

	// Create session context
	sessionCtx, cancel := context.WithCancel(context.Background())

	// Generate resource names
	deploymentName := fmt.Sprintf("agentapi-session-%s", id)
	serviceName := fmt.Sprintf("agentapi-session-%s-svc", id)
	pvcName := fmt.Sprintf("agentapi-session-%s-pvc", id)

	// Create KubernetesSession using constructor
	session := NewKubernetesSession(
		id,
		req,
		deploymentName,
		serviceName,
		pvcName,
		m.namespace,
		m.k8sConfig.BasePort,
		cancel,
		webhookPayload,
	)
	// Register proxy-wide status change broadcaster
	session.statusChangeCallback = m.broadcastStatusChange

	// Store session
	m.mutex.Lock()
	m.sessions[id] = session
	m.mutex.Unlock()

	log.Printf("[K8S_SESSION] Creating session %s in namespace %s", id, m.namespace)

	// Create Service first. It is the canonical session resource and owns every
	// other per-session Kubernetes resource through ownerReferences.
	if err := m.createService(ctx, session); err != nil {
		m.cleanupSession(id)
		return nil, fmt.Errorf("failed to create Service: %w", err)
	}
	log.Printf("[K8S_SESSION] Created Service %s for session %s", serviceName, id)

	// Create PVC if enabled
	if m.isPVCEnabled() {
		if err := m.createPVC(ctx, session); err != nil {
			if delErr := m.deleteSessionResources(ctx, session); delErr != nil {
				log.Printf("[K8S_SESSION] Failed to cleanup resources after PVC creation failure: %v", delErr)
			}
			m.cleanupSession(id)
			return nil, fmt.Errorf("failed to create PVC: %w", err)
		}
		log.Printf("[K8S_SESSION] Created PVC %s for session %s", pvcName, id)
	} else {
		log.Printf("[K8S_SESSION] PVC disabled, using EmptyDir for session %s", id)
	}
	// Cache initial message as description
	if req.InitialMessage != "" {
		session.SetDescription(req.InitialMessage)
	}

	// Ensure service account exists for team-scoped sessions (best-effort)
	if req.Scope == entities.ScopeTeam && req.TeamID != "" && m.serviceAccountEnsurer != nil {
		if err := m.serviceAccountEnsurer.EnsureServiceAccount(ctx, req.TeamID); err != nil {
			log.Printf("[K8S_SESSION] Warning: failed to ensure service account for team %s: %v", req.TeamID, err)
			// Continue anyway - session will work without service account
		}
	}

	// Build session settings once for the provision request and restart Secret.
	// When req.ProvisionSettings is provided (small-cluster / forwarding mode), use it
	// directly instead of resolving secrets from this cluster.
	var sessionSettings *sessionsettings.SessionSettings
	if req.ProvisionSettings != nil {
		sessionSettings = m.normalizeProvisionSettings(req.ProvisionSettings)
	} else {
		sessionSettings = m.buildSessionSettings(ctx, session, req, webhookPayload)
	}
	if len(webhookPayload) > 0 {
		sessionSettings.WebhookPayload = string(webhookPayload)
	}

	session.SetProvisionSettings(sessionSettings)
	// Persist restart settings before creating the provision request or workload.
	// A Pod can be evicted at any point after the workload is created; waiting for
	// the first provisioning cycle to finish leaves such a replacement unable to
	// auto-provision itself.
	if err := m.createSessionSettingsSecretFromSettings(ctx, session, req, sessionSettings); err != nil {
		if delErr := m.deleteSessionResources(ctx, session); delErr != nil {
			log.Printf("[K8S_SESSION] Failed to cleanup resources after settings secret creation failure: %v", delErr)
		}
		m.cleanupSession(id)
		return nil, fmt.Errorf("failed to create session settings secret: %w", err)
	}
	if err := m.CreateProvisionRequest(ctx, session); err != nil {
		if delErr := m.deleteSessionResources(ctx, session); delErr != nil {
			log.Printf("[K8S_SESSION] Failed to cleanup resources after provision request creation failure: %v", delErr)
		}
		m.cleanupSession(id)
		return nil, fmt.Errorf("failed to create provision request: %w", err)
	}

	// Create workload. PVC-backed sessions use a Deployment for restart recovery;
	// ephemeral EmptyDir sessions use a Pod with restartPolicy=Never.
	if err := m.createSessionWorkload(ctx, session, req); err != nil {
		if delErr := m.deleteSessionResources(ctx, session); delErr != nil {
			log.Printf("[K8S_SESSION] Failed to cleanup resources after workload creation failure: %v", delErr)
		}
		m.cleanupSession(id)
		return nil, fmt.Errorf("failed to create session workload: %w", err)
	}
	log.Printf("[K8S_SESSION] Created workload %s for session %s", deploymentName, id)

	// Start watching session in background
	go m.watchSession(sessionCtx, session)

	// Log session start
	repository := ""
	if req.RepoInfo != nil {
		repository = req.RepoInfo.FullName
	}
	if err := m.logger.LogSessionStart(id, repository); err != nil {
		log.Printf("[K8S_SESSION] Failed to log session start: %v", err)
	}

	// Invalidate session-list cache so the new session appears immediately in
	// every label-selector scoped list it belongs to.
	if m.sessionListCacheRepo != nil {
		if err := m.sessionListCacheRepo.InvalidateSessionListCache(context.Background(), m.namespace); err != nil {
			log.Printf("[K8S_SESSION] Warning: failed to invalidate session list cache after create: %v", err)
		}
	}

	log.Printf("[K8S_SESSION] Session %s created successfully", id)
	return session, nil
}
