package services

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"sort"
	"time"

	coreallocation "github.com/takutakahashi/agentapi-proxy/internal/core/sessionallocation"
	"github.com/takutakahashi/agentapi-proxy/internal/domain/entities"
	"github.com/takutakahashi/agentapi-proxy/pkg/sessionsettings"
	closeutils "github.com/takutakahashi/agentapi-proxy/pkg/utils"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// Kubernetes resources labeled as stock while serving a session, so the parent
// runner state is the durable authority that distinguishes them from idle stock.
func (m *KubernetesSessionManager) fetchAllocatedRunnerIDs(ctx context.Context) ([]string, error) {
	m.mutex.RLock()
	parentURL := m.runnerParentURL
	managerID := m.runnerManagerID
	managerToken := m.runnerManagerToken
	m.mutex.RUnlock()
	if parentURL == "" || managerID == "" || managerToken == "" {
		return nil, nil
	}

	endpoint := parentURL + "/internal/session-managers/" + url.PathEscape(managerID) + "/heartbeat"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+managerToken)
	resp, err := instrumentedHTTPClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer closeutils.SafeCloseResponseBody(resp.Body) //nolint:errcheck
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("parent heartbeat returned HTTP %d", resp.StatusCode)
	}
	var result struct {
		AllocatedRunnerIDs []string `json:"allocated_runner_ids"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("decode parent heartbeat: %w", err)
	}
	return result.AllocatedRunnerIDs, nil
}

// findStockSession lists Services labeled agentapi.proxy/stock=true and returns the
// oldest available one (by CreationTimestamp, ascending). Oldest sessions have been
// warmed up the longest and are the most ready to serve.
// Returns (nil, nil) when no stock is available.
// Note: Sandbox (network filter) is always enabled, so only DinD is queried.
func (m *KubernetesSessionManager) findStockSession(ctx context.Context, requirements coreallocation.Requirements) (*corev1.Service, error) {
	// Sandbox is always enabled (capability-sandbox=true)
	selector := fmt.Sprintf(
		"agentapi.proxy/stock=true,app.kubernetes.io/managed-by=agentapi-proxy,agentapi.proxy/capability-sandbox=true,agentapi.proxy/capability-dind=%t,!agentapi.proxy/session-pool",
		requirements.DinD,
	)
	selector = m.managerScopedSelector(selector)
	svcs, err := m.client.CoreV1().Services(m.namespace).List(ctx, metav1.ListOptions{LabelSelector: selector})
	if err != nil {
		return nil, fmt.Errorf("failed to list stock services: %w", err)
	}

	// Sort by creation time ascending so the oldest (longest-warmed) session is preferred.
	sort.Slice(svcs.Items, func(i, j int) bool {
		return svcs.Items[i].CreationTimestamp.Before(&svcs.Items[j].CreationTimestamp)
	})

	for i := range svcs.Items {
		svc := &svcs.Items[i]
		if svc.DeletionTimestamp != nil {
			continue
		}
		if svc.Labels["agentapi.proxy/session-id"] == "" {
			continue
		}
		return svc, nil
	}
	return nil, nil
}

// claimStockService atomically claims a stock Service by transitioning the
// agentapi.proxy/stock label from "true" → "claiming" via an Update (which uses
// Kubernetes' resourceVersion-based optimistic locking).
// Using "claiming" instead of deleting the label ensures that ListSessions
// (which excludes any Service with the agentapi.proxy/stock key) never restores
// this Service with an empty user-id during the adoption window, preventing 403
// errors and ghost-session appearances on other proxy replicas.
// If another replica claims the same stock concurrently, the Update returns a
// Conflict error and the caller should fall back to creating a new session.
func (m *KubernetesSessionManager) claimStockService(ctx context.Context, svc *corev1.Service) (*corev1.Service, error) {
	if svc.Labels["agentapi.proxy/stock"] != "true" {
		return nil, fmt.Errorf("service %s is not a stock service", svc.Name)
	}
	// Transition to "claiming" so findStockSession (which filters stock=true) no
	// longer picks it up, while ListSessions (which requires !agentapi.proxy/stock)
	// also skips it until adoptStockSession removes the label entirely.
	svc.Labels["agentapi.proxy/stock"] = "claiming"
	// Update uses the ResourceVersion already set on svc for optimistic locking.
	updated, err := m.client.CoreV1().Services(m.namespace).Update(ctx, svc, metav1.UpdateOptions{})
	if err != nil {
		return nil, fmt.Errorf("failed to claim stock service %s: %w", svc.Name, err)
	}
	return updated, nil
}

// adoptStockSession takes a claimed stock Service and turns it into a regular session.
// It updates Service/Deployment labels and annotations, builds the provision payload,
// and starts watchStockSession in the background.
func (m *KubernetesSessionManager) adoptStockSession(
	ctx context.Context,
	req *entities.RunServerRequest,
	webhookPayload []byte,
	stockSvc *corev1.Service,
) (entities.Session, error) {
	stockID := stockSvc.Labels["agentapi.proxy/session-id"]
	deploymentName := fmt.Sprintf("agentapi-session-%s", stockID)
	pvcName := fmt.Sprintf("agentapi-session-%s-pvc", stockID)

	sessionCtx, cancel := context.WithCancel(context.Background())

	// Build KubernetesSession reusing the stock's resource names.
	session := NewKubernetesSession(
		stockID,
		req,
		deploymentName,
		stockSvc.Name, // service name stays the same
		pvcName,
		m.namespace,
		m.k8sConfig.BasePort,
		cancel,
		webhookPayload,
	)

	// Cache initial message as description.
	if req.InitialMessage != "" {
		session.SetDescription(req.InitialMessage)
	}

	// Register proxy-wide status change broadcaster
	session.statusChangeCallback = m.broadcastStatusChange

	// Register session in memory.
	m.mutex.Lock()
	m.sessions[stockID] = session
	m.mutex.Unlock()

	log.Printf("[K8S_SESSION] Adopting stock session %s in namespace %s", stockID, m.namespace)

	effectiveSandbox := m.resolveSandboxParams(ctx, req)
	if err := applySandboxPolicyToProvisioner(ctx, stockSvc, effectiveSandbox); err != nil {
		if delErr := m.deleteSessionResources(ctx, session); delErr != nil {
			log.Printf("[K8S_SESSION] Failed to cleanup stock session after sandbox policy error: %v", delErr)
		}
		m.cleanupSession(stockID)
		cancel()
		return nil, fmt.Errorf("failed to apply sandbox policy to stock session: %w", err)
	}

	// Check whether the stock already has a PVC; if not, we leave it as EmptyDir.
	_, pvcErr := m.client.CoreV1().PersistentVolumeClaims(m.namespace).Get(ctx, pvcName, metav1.GetOptions{})
	switch {
	case pvcErr == nil:
		log.Printf("[K8S_SESSION] Stock session %s has existing PVC %s, reusing it", stockID, pvcName)
		if currentPVC, err := m.client.CoreV1().PersistentVolumeClaims(m.namespace).Get(ctx, pvcName, metav1.GetOptions{}); err == nil {
			currentPVC.OwnerReferences = m.sessionServiceOwnerReferences(ctx, stockID)
			if _, err := m.client.CoreV1().PersistentVolumeClaims(m.namespace).Update(ctx, currentPVC, metav1.UpdateOptions{}); err != nil {
				log.Printf("[K8S_SESSION] Warning: failed to update stock PVC owner reference for session %s: %v", stockID, err)
			}
		}
	case errors.IsNotFound(pvcErr):
		log.Printf("[K8S_SESSION] Stock session %s has no PVC, using EmptyDir", stockID)
	default:
		log.Printf("[K8S_SESSION] Warning: failed to check PVC for stock session %s: %v", stockID, pvcErr)
	}

	// Ensure service account for team-scoped sessions (best-effort).
	if req.Scope == entities.ScopeTeam && req.TeamID != "" && m.serviceAccountEnsurer != nil {
		if err := m.serviceAccountEnsurer.EnsureServiceAccount(ctx, req.TeamID); err != nil {
			log.Printf("[K8S_SESSION] Warning: failed to ensure service account for team %s: %v", req.TeamID, err)
		}
	}

	// Build session settings and create a provision request for the adopted pod.
	// External allocations already carry fully-resolved settings, including the
	// direct parent runtime bootstrap, and must not be re-resolved locally.
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
	if err := m.createSessionSettingsSecretFromSettings(ctx, session, req, sessionSettings); err != nil {
		m.cleanupSession(stockID)
		cancel()
		return nil, fmt.Errorf("failed to create session settings secret for stock session: %w", err)
	}
	if err := m.CreateProvisionRequest(ctx, session); err != nil {
		m.cleanupSession(stockID)
		cancel()
		return nil, fmt.Errorf("failed to create provision request for stock session: %w", err)
	}

	// Update Service labels and annotations to reflect the new owner.
	now := time.Now()
	newLabels := m.buildLabels(session)
	annotations := map[string]string{
		"agentapi.proxy/created-at":      now.Format(time.RFC3339),
		"agentapi.proxy/updated-at":      now.Format(time.RFC3339),
		"agentapi.proxy/team-id":         req.TeamID,
		"agentapi.proxy/last-message-at": now.UTC().Format(time.RFC3339),
	}
	if req.AgentType != "" {
		annotations["agentapi.proxy/agent-type"] = req.AgentType
	}
	// Store initial message in annotation so all proxy replicas can read it immediately.
	if req.InitialMessage != "" {
		annotations["agentapi.proxy/initial-message"] = req.InitialMessage
	}
	if req.SessionTTL != "" {
		annotations["agentapi.proxy/session-ttl"] = req.SessionTTL
	}

	currentSvc, err := m.client.CoreV1().Services(m.namespace).Get(ctx, stockSvc.Name, metav1.GetOptions{})
	if err != nil {
		m.cleanupSession(stockID)
		cancel()
		return nil, fmt.Errorf("failed to get stock service for label update: %w", err)
	}
	currentSvc.Labels = newLabels
	if currentSvc.Annotations == nil {
		currentSvc.Annotations = make(map[string]string)
	}
	for k, v := range annotations {
		currentSvc.Annotations[k] = v
	}
	if _, err := m.client.CoreV1().Services(m.namespace).Update(ctx, currentSvc, metav1.UpdateOptions{}); err != nil {
		log.Printf("[K8S_SESSION] Warning: failed to update stock service labels for session %s: %v", stockID, err)
	}

	// Update workload metadata labels only to reflect the new owner.
	// For Deployments, do not update spec.template.labels because that would
	// trigger a rolling update while the provision request is being claimed.
	if m.isPVCEnabled() {
		currentDep, err := m.client.AppsV1().Deployments(m.namespace).Get(ctx, deploymentName, metav1.GetOptions{})
		if err != nil {
			log.Printf("[K8S_SESSION] Warning: failed to get stock deployment for label update: %v", err)
		} else {
			currentDep.Labels = newLabels
			currentDep.OwnerReferences = m.sessionServiceOwnerReferences(ctx, stockID)
			if _, err := m.client.AppsV1().Deployments(m.namespace).Update(ctx, currentDep, metav1.UpdateOptions{}); err != nil {
				log.Printf("[K8S_SESSION] Warning: failed to update stock deployment labels for session %s: %v", stockID, err)
			}
		}
	} else {
		currentPod, err := m.client.CoreV1().Pods(m.namespace).Get(ctx, deploymentName, metav1.GetOptions{})
		if err != nil {
			log.Printf("[K8S_SESSION] Warning: failed to get stock pod for label update: %v", err)
		} else {
			currentPod.Labels = newLabels
			currentPod.OwnerReferences = m.sessionServiceOwnerReferences(ctx, stockID)
			if _, err := m.client.CoreV1().Pods(m.namespace).Update(ctx, currentPod, metav1.UpdateOptions{}); err != nil {
				log.Printf("[K8S_SESSION] Warning: failed to update stock pod labels for session %s: %v", stockID, err)
			}
		}
	}

	// Start background watch. The Pod is already running and will claim the provision request.
	go m.watchStockSession(sessionCtx, session)

	// Log session start.
	repository := ""
	if req.RepoInfo != nil {
		repository = req.RepoInfo.FullName
	}
	if err := m.logger.LogSessionStart(stockID, repository); err != nil {
		log.Printf("[K8S_SESSION] Failed to log session start for stock session %s: %v", stockID, err)
	}

	// Invalidate session-list cache so the adopted stock session appears
	// immediately in every label-selector scoped list it belongs to.
	if m.sessionListCacheRepo != nil {
		if err := m.sessionListCacheRepo.InvalidateSessionListCache(context.Background(), m.namespace); err != nil {
			log.Printf("[K8S_SESSION] Warning: failed to invalidate session list cache after stock adopt: %v", err)
		}
	}

	log.Printf("[K8S_SESSION] Stock session %s adopted successfully", stockID)
	return session, nil
}

// watchStockSession monitors a stock-adopted session.
// Unlike watchSession, it skips the ReadyReplicas wait (Pod is already running)
// and waits for the pre-created Pod to claim its provision request.
func (m *KubernetesSessionManager) watchStockSession(ctx context.Context, session *KubernetesSession) {
	defer func() {
		log.Printf("[K8S_SESSION] Stock session %s watch ended", session.id)
	}()

	session.SetStatus("starting")

	// Wait for the pod to be ready before waiting on its provision request.
	// Although stock sessions are pre-warmed, the pod may not yet be ready if
	// the session was adopted immediately after creation (e.g. inventory was
	// just replenished). Reuse the same ready-wait loop used by watchSession.
	timeout := time.After(time.Duration(m.k8sConfig.PodStartTimeout) * time.Second)
	readyTicker := time.NewTicker(2 * time.Second)
	podReady := false
	for !podReady {
		select {
		case <-ctx.Done():
			readyTicker.Stop()
			log.Printf("[K8S_SESSION] Stock session %s context cancelled while waiting for pod", session.id)
			return
		case <-timeout:
			readyTicker.Stop()
			log.Printf("[K8S_SESSION] Stock session %s startup timeout", session.id)
			session.SetStatusMessage("Timed out waiting for the session workload to become ready")
			session.SetStatus("timeout")
			return
		case <-readyTicker.C:
			ready, err := m.isSessionWorkloadReady(context.Background(), session)
			if err == nil && ready {
				podReady = true
			}
		}
	}
	readyTicker.Stop()
	log.Printf("[K8S_SESSION] Stock session %s: Pod is ready", session.id)

	log.Printf("[K8S_SESSION] Waiting for pull provision request to become ready for stock session %s", session.id)
	if err := m.waitForPullProvisioner(ctx, session); err != nil {
		log.Printf("[K8S_SESSION] Pull provisioner error for stock session %s: %v", session.id, err)
		session.SetStatusMessage(err.Error())
		session.SetStatus("error")
		return
	}

	// Don't downgrade to "active" if watchAgentAPIStatus already detected the
	// agent is running (e.g. provisioner's initial message is still in flight).
	if session.Status() != "running" {
		session.SetStatus("active")
		log.Printf("[K8S_SESSION] Stock session %s is now active", session.id)
	} else {
		log.Printf("[K8S_SESSION] Stock session %s skipped SetStatus(active): agent already running", session.id)
	}
	if err := m.ScheduleSessionSuspend(ctx, session.ID()); err != nil {
		log.Printf("[K8S_SESSION] Failed to schedule initial suspend for stock session %s: %v", session.id, err)
	}

	// Continue watching deployment health. Runtime status is pushed outbound.
	m.runDeploymentStatusWatcher(ctx, session)
}
