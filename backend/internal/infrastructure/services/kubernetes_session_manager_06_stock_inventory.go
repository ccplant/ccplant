package services

import (
	"context"
	"fmt"
	"log"
	"sort"
	"strings"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// CountStockSessions returns the number of available or currently creating
// (and not being deleted) stock sessions. Creating sessions count as reserved
// inventory so the replenisher does not start another target-sized batch on
// every reconciliation tick while Pods are becoming ready.
// Note: Sandbox (network filter) is always enabled, so only DinD capability is queried.
func (m *KubernetesSessionManager) CountStockSessions(ctx context.Context, dind bool) (int, error) {
	return m.CountStockSessionsForPool(ctx, "", dind)
}

func (m *KubernetesSessionManager) CountStockSessionsForPool(ctx context.Context, pool string, dind bool) (int, error) {
	// Sandbox is always enabled (capability-sandbox=true)
	selector := fmt.Sprintf(
		"agentapi.proxy/stock in (true,creating),app.kubernetes.io/managed-by=agentapi-proxy,agentapi.proxy/capability-sandbox=true,agentapi.proxy/capability-dind=%t",
		dind,
	)
	if pool != "" {
		selector += ",agentapi.proxy/session-pool=" + pool
	} else {
		selector += ",!agentapi.proxy/session-pool"
	}
	selector = m.managerScopedSelector(selector)
	svcs, err := m.client.CoreV1().Services(m.namespace).List(ctx, metav1.ListOptions{
		LabelSelector: selector,
	})
	if err != nil {
		return 0, fmt.Errorf("failed to list stock services: %w", err)
	}
	allocatedRunnerIDs, err := m.fetchAllocatedRunnerIDs(ctx)
	if err != nil {
		return 0, fmt.Errorf("failed to resolve allocated runners while counting stock: %w", err)
	}
	allocated := make(map[string]struct{}, len(allocatedRunnerIDs))
	for _, id := range allocatedRunnerIDs {
		allocated[id] = struct{}{}
	}
	count := 0
	for i := range svcs.Items {
		if svcs.Items[i].DeletionTimestamp != nil {
			continue
		}
		// Direct-runtime runners intentionally retain their local stock labels
		// after allocation. The parent runner registry is authoritative for their
		// allocation state; counting these Services as idle suppresses pool
		// replenishment even though they cannot accept another session.
		if _, isAllocated := allocated[svcs.Items[i].Labels["agentapi.proxy/session-id"]]; !isAllocated {
			count++
		}
	}
	return count, nil
}

// CountRunnerSessionsForPool counts all live runner Services, including claimed
// sessions. It is used to enforce a pool's total concurrency limit.
func (m *KubernetesSessionManager) CountRunnerSessionsForPool(ctx context.Context, pool string) (int, error) {
	selector := m.managerScopedSelector("app.kubernetes.io/managed-by=agentapi-proxy,agentapi.proxy/session-pool=" + pool)
	svcs, err := m.client.CoreV1().Services(m.namespace).List(ctx, metav1.ListOptions{LabelSelector: selector})
	if err != nil {
		return 0, fmt.Errorf("failed to list runner services: %w", err)
	}
	count := 0
	for i := range svcs.Items {
		if svcs.Items[i].DeletionTimestamp == nil {
			count++
		}
	}
	return count, nil
}

// ListRunnerSessionIDs returns the IDs of all runner workloads currently backed
// by a live Service in this manager's namespace.
func (m *KubernetesSessionManager) ListRunnerSessionIDs(ctx context.Context) ([]string, error) {
	selector := m.managerScopedSelector("app.kubernetes.io/managed-by=agentapi-proxy,app.kubernetes.io/name=agentapi-session,agentapi.proxy/session-pool")
	svcs, err := m.client.CoreV1().Services(m.namespace).List(ctx, metav1.ListOptions{LabelSelector: selector})
	if err != nil {
		return nil, fmt.Errorf("failed to list runner services: %w", err)
	}
	ids := make([]string, 0, len(svcs.Items))
	for i := range svcs.Items {
		if svcs.Items[i].DeletionTimestamp != nil {
			continue
		}
		if id := svcs.Items[i].Labels["agentapi.proxy/session-id"]; id != "" {
			ids = append(ids, id)
		}
	}
	sort.Strings(ids)
	return ids, nil
}

// RunnerStatusCounts returns the local runner inventory grouped into the
// operator-facing idle, running and offline states.
func (m *KubernetesSessionManager) RunnerStatusCounts(ctx context.Context) (map[string]int, error) {
	selector := m.managerScopedSelector("app.kubernetes.io/managed-by=agentapi-proxy,app.kubernetes.io/name=agentapi-session,agentapi.proxy/session-pool")
	services, err := m.client.CoreV1().Services(m.namespace).List(ctx, metav1.ListOptions{LabelSelector: selector})
	if err != nil {
		return nil, fmt.Errorf("list runner services for metrics: %w", err)
	}
	pods, err := m.client.CoreV1().Pods(m.namespace).List(ctx, metav1.ListOptions{LabelSelector: selector})
	if err != nil {
		return nil, fmt.Errorf("list runner pods for metrics: %w", err)
	}
	live := make(map[string]bool, len(pods.Items))
	for i := range pods.Items {
		pod := &pods.Items[i]
		if pod.DeletionTimestamp == nil && pod.Status.Phase == corev1.PodRunning {
			live[pod.Labels["agentapi.proxy/session-id"]] = true
		}
	}
	counts := map[string]int{"idle": 0, "running": 0, "offline": 0}
	for i := range services.Items {
		service := &services.Items[i]
		if service.DeletionTimestamp != nil {
			continue
		}
		id := service.Labels["agentapi.proxy/session-id"]
		switch {
		case !live[id]:
			counts["offline"]++
		case service.Labels["agentapi.proxy/stock"] == "true":
			counts["idle"]++
		default:
			counts["running"]++
		}
	}
	return counts, nil
}

// DeleteRunnerSessionsNotRegistered removes local runner workloads whose
// durable registration no longer exists on the parent. A runner is registered
// before its Service is created, so a newly-created workload cannot be removed
// by this reconciliation window.
func (m *KubernetesSessionManager) DeleteRunnerSessionsNotRegistered(ctx context.Context, registered map[string]struct{}) error {
	selector := m.managerScopedSelector("app.kubernetes.io/managed-by=agentapi-proxy,app.kubernetes.io/name=agentapi-session,agentapi.proxy/session-pool")
	svcs, err := m.client.CoreV1().Services(m.namespace).List(ctx, metav1.ListOptions{LabelSelector: selector})
	if err != nil {
		return fmt.Errorf("failed to list runner services for orphan cleanup: %w", err)
	}
	var cleanupErrs []string
	for i := range svcs.Items {
		id := svcs.Items[i].Labels["agentapi.proxy/session-id"]
		if id == "" {
			continue
		}
		if _, ok := registered[id]; ok {
			continue
		}
		retired, err := m.retireStockRunner(ctx, id)
		if err != nil {
			cleanupErrs = append(cleanupErrs, fmt.Sprintf("retire %s: %v", id, err))
			continue
		}
		if !retired {
			continue
		}
		if err := m.DeleteSession(id); err != nil {
			cleanupErrs = append(cleanupErrs, fmt.Sprintf("%s: %v", id, err))
		}
	}
	if len(cleanupErrs) > 0 {
		return fmt.Errorf("delete orphaned runner sessions: %s", strings.Join(cleanupErrs, "; "))
	}
	return nil
}

// PurgeStockSessions deletes all existing pre-warmed stock sessions (Service,
// Deployment, PVC). Called by the stock inventory worker on startup to ensure
// that stale pods built from an old image are replaced with fresh ones.
// This also purges sessions stuck in the "claiming" state (stock=claiming) that
// were abandoned mid-adoption due to a crash or restart.
func (m *KubernetesSessionManager) PurgeStockSessions(ctx context.Context) error {
	return m.purgeStockSessions(ctx, "")
}

// PurgeStockSessionsForPool retires and deletes idle stock belonging to pool.
// Allocation artifacts and the parent runner registry protect claimed runners.
func (m *KubernetesSessionManager) PurgeStockSessionsForPool(ctx context.Context, pool string) error {
	if strings.TrimSpace(pool) == "" {
		return fmt.Errorf("pool is required")
	}
	return m.purgeStockSessions(ctx, pool)
}

func (m *KubernetesSessionManager) purgeStockSessions(ctx context.Context, pool string) error {
	allocatedRunnerIDs, err := m.fetchAllocatedRunnerIDs(ctx)
	if err != nil {
		return fmt.Errorf("failed to resolve allocated runners for purge protection: %w", err)
	}
	// Use a set-based selector to match both stock=true (unclaimed) and
	// stock=claiming (abandoned mid-adoption). Collect session IDs from every
	// resource kind so a previous partial purge cannot leave orphaned stock
	// Deployments/PVCs behind.
	selector := "agentapi.proxy/stock in (true, claiming, creating),app.kubernetes.io/managed-by=agentapi-proxy"
	if pool != "" {
		selector += ",agentapi.proxy/session-pool=" + pool
	}
	selector = m.managerScopedSelector(selector)
	svcs, err := m.client.CoreV1().Services(m.namespace).List(ctx, metav1.ListOptions{
		LabelSelector: selector,
	})
	if err != nil {
		return fmt.Errorf("failed to list stock services for purge: %w", err)
	}
	allSessionSvcs, err := m.client.CoreV1().Services(m.namespace).List(ctx, metav1.ListOptions{
		LabelSelector: m.managerScopedSelector("app.kubernetes.io/managed-by=agentapi-proxy,app.kubernetes.io/name=agentapi-session"),
	})
	if err != nil {
		return fmt.Errorf("failed to list session services for purge protection: %w", err)
	}
	deployments, err := m.client.AppsV1().Deployments(m.namespace).List(ctx, metav1.ListOptions{
		LabelSelector: selector,
	})
	if err != nil {
		return fmt.Errorf("failed to list stock deployments for purge: %w", err)
	}
	pods, err := m.client.CoreV1().Pods(m.namespace).List(ctx, metav1.ListOptions{
		LabelSelector: selector,
	})
	if err != nil {
		return fmt.Errorf("failed to list stock pods for purge: %w", err)
	}
	pvcs, err := m.client.CoreV1().PersistentVolumeClaims(m.namespace).List(ctx, metav1.ListOptions{
		LabelSelector: selector,
	})
	if err != nil {
		return fmt.Errorf("failed to list stock pvcs for purge: %w", err)
	}
	settingsSecrets, err := m.client.CoreV1().Secrets(m.namespace).List(ctx, metav1.ListOptions{
		LabelSelector: "agentapi.proxy/resource=session-settings,agentapi.proxy/session-id",
	})
	if err != nil {
		return fmt.Errorf("failed to list session settings for purge protection: %w", err)
	}
	provisionRequests, err := m.client.CoreV1().Secrets(m.namespace).List(ctx, metav1.ListOptions{
		LabelSelector: "agentapi.proxy/provision-request=true,agentapi.proxy/session-id",
	})
	if err != nil {
		return fmt.Errorf("failed to list provision requests for purge protection: %w", err)
	}

	deletePolicy := metav1.DeletePropagationForeground
	deleteOptions := metav1.DeleteOptions{PropagationPolicy: &deletePolicy}

	var purgeErrs []string
	sessionIDs := make(map[string]struct{})
	allocatedSessionIDs := make(map[string]struct{})
	for _, sessionID := range allocatedRunnerIDs {
		if sessionID != "" {
			allocatedSessionIDs[sessionID] = struct{}{}
		}
	}
	for i := range allSessionSvcs.Items {
		svc := &allSessionSvcs.Items[i]
		sessionID := svc.Labels["agentapi.proxy/session-id"]
		if sessionID == "" || svc.DeletionTimestamp != nil {
			continue
		}
		stockState := svc.Labels["agentapi.proxy/stock"]
		if stockState != "true" && stockState != "claiming" {
			allocatedSessionIDs[sessionID] = struct{}{}
		}
	}
	// An allocation creates durable session settings and a provision request
	// before it updates the stock Service labels. Treat either Secret as proof
	// that the stock has been allocated. This closes the adoption window where
	// a manager restart could otherwise purge a running session whose Service is
	// still labeled stock=true or stock=claiming.
	for i := range settingsSecrets.Items {
		if sessionID := settingsSecrets.Items[i].Labels["agentapi.proxy/session-id"]; sessionID != "" {
			allocatedSessionIDs[sessionID] = struct{}{}
		}
	}
	for i := range provisionRequests.Items {
		if sessionID := provisionRequests.Items[i].Labels["agentapi.proxy/session-id"]; sessionID != "" {
			allocatedSessionIDs[sessionID] = struct{}{}
		}
	}
	for i := range svcs.Items {
		svc := &svcs.Items[i]
		sessionID := svc.Labels["agentapi.proxy/session-id"]
		if sessionID == "" {
			continue
		}
		if _, allocated := allocatedSessionIDs[sessionID]; allocated {
			log.Printf("[STOCK_INVENTORY] Skipping allocated session %s during stock purge (matched allocation artifact)", sessionID)
			continue
		}
		sessionIDs[sessionID] = struct{}{}

	}
	for i := range deployments.Items {
		if sessionID := deployments.Items[i].Labels["agentapi.proxy/session-id"]; sessionID != "" {
			if _, adopted := allocatedSessionIDs[sessionID]; adopted {
				log.Printf("[STOCK_INVENTORY] Skipping adopted session %s during stock purge (matched stale deployment label)", sessionID)
				continue
			}
			sessionIDs[sessionID] = struct{}{}
		}
	}
	for i := range pods.Items {
		if sessionID := pods.Items[i].Labels["agentapi.proxy/session-id"]; sessionID != "" {
			if _, adopted := allocatedSessionIDs[sessionID]; adopted {
				log.Printf("[STOCK_INVENTORY] Skipping adopted session %s during stock purge (matched stale pod label)", sessionID)
				continue
			}
			sessionIDs[sessionID] = struct{}{}
		}
	}
	for i := range pvcs.Items {
		if sessionID := pvcs.Items[i].Labels["agentapi.proxy/session-id"]; sessionID != "" {
			if _, adopted := allocatedSessionIDs[sessionID]; adopted {
				log.Printf("[STOCK_INVENTORY] Skipping adopted session %s during stock purge (matched stale pvc label)", sessionID)
				continue
			}
			sessionIDs[sessionID] = struct{}{}
		}
	}

	for sessionID := range sessionIDs {
		retired, err := m.retireStockRunner(ctx, sessionID)
		if err != nil {
			purgeErrs = append(purgeErrs, fmt.Sprintf("retire runner %s: %v", sessionID, err))
			continue
		}
		if !retired {
			continue
		}
		if err := m.client.CoreV1().Services(m.namespace).Delete(ctx, "agentapi-session-"+sessionID+"-svc", deleteOptions); err != nil && !errors.IsNotFound(err) {
			purgeErrs = append(purgeErrs, fmt.Sprintf("service %s: %v", sessionID, err))
			continue
		}
		log.Printf("[STOCK_INVENTORY] Purging stock session %s", sessionID)

		// Delete workload. Try both workload kinds so stock sessions created
		// before a PVC setting change are also purged.
		workloadName := fmt.Sprintf("agentapi-session-%s", sessionID)
		if err := m.client.AppsV1().Deployments(m.namespace).Delete(ctx, workloadName, deleteOptions); err != nil && !errors.IsNotFound(err) {
			purgeErrs = append(purgeErrs, fmt.Sprintf("deployment %s: %v", workloadName, err))
		}
		if err := m.client.CoreV1().Pods(m.namespace).Delete(ctx, workloadName, deleteOptions); err != nil && !errors.IsNotFound(err) {
			purgeErrs = append(purgeErrs, fmt.Sprintf("pod %s: %v", workloadName, err))
		}

		// Delete PVC if present. This intentionally does not depend on the
		// current PVC setting because stale stock PVCs can remain after toggling
		// PVC support off.
		pvcName := fmt.Sprintf("agentapi-session-%s-pvc", sessionID)
		if err := m.client.CoreV1().PersistentVolumeClaims(m.namespace).Delete(ctx, pvcName, deleteOptions); err != nil && !errors.IsNotFound(err) {
			purgeErrs = append(purgeErrs, fmt.Sprintf("pvc %s: %v", pvcName, err))
		}
	}

	if len(purgeErrs) > 0 {
		return fmt.Errorf("purge errors: %s", strings.Join(purgeErrs, "; "))
	}
	log.Printf("[STOCK_INVENTORY] Purged %d stock session(s)", len(sessionIDs))
	return nil
}

// fetchAllocatedRunnerIDs asks the parent control plane which local runners
// have already accepted an allocation. Direct-runtime runners keep their local
