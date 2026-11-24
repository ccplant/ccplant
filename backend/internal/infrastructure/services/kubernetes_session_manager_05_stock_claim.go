package services

import (
	"context"
	"crypto/subtle"
	"fmt"
	"log"
	"strings"

	"github.com/takutakahashi/agentapi-proxy/internal/domain/entities"
	"github.com/takutakahashi/agentapi-proxy/pkg/sessionsettings"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/util/retry"
)

// PurgeStaleStockSessions removes available stock created by a different
// effective session Pod template. Stock without a recorded hash is treated as
// stale so it is upgraded once after this behavior is deployed.
func (m *KubernetesSessionManager) PurgeStaleStockSessions(ctx context.Context) error {
	m.refreshConfig()
	svcs, err := m.client.CoreV1().Services(m.namespace).List(ctx, metav1.ListOptions{
		// Pool inventory has its own lifecycle. The local stock reconciler must
		// never refresh pool runners, even when their pod template is stale.
		LabelSelector: m.managerScopedSelector("app.kubernetes.io/managed-by=agentapi-proxy,!agentapi.proxy/session-pool"),
	})
	if err != nil {
		return fmt.Errorf("failed to list stock services for template reconciliation: %w", err)
	}

	var purgeErrs []string
	desiredHashes := make(map[bool]string, 2)
	for i := range svcs.Items {
		errs, err := m.purgeStaleStockService(ctx, &svcs.Items[i], desiredHashes)
		if err != nil {
			return err
		}
		purgeErrs = append(purgeErrs, errs...)
	}
	if len(purgeErrs) > 0 {
		return fmt.Errorf("purge stale stock errors: %s", strings.Join(purgeErrs, "; "))
	}
	return nil
}

func (m *KubernetesSessionManager) purgeStaleStockService(ctx context.Context, svc *corev1.Service, desiredHashes map[bool]string) ([]string, error) {
	stockState := svc.Labels["agentapi.proxy/stock"]
	// Startup time is unbounded for stock sessions. A slow image pull or a
	// constrained cluster cannot be repaired by recreating the same workload.
	if stockState == "creating" || stockState != "true" {
		return nil, nil
	}
	dind := svc.Labels["agentapi.proxy/capability-dind"] == "true"
	desiredHash, ok := desiredHashes[dind]
	if !ok {
		var err error
		desiredHash, err = m.stockPodTemplateHash(ctx, dind)
		if err != nil {
			return nil, fmt.Errorf("calculate stock pod template hash (dind=%t): %w", dind, err)
		}
		desiredHashes[dind] = desiredHash
	}
	stockHash := svc.Labels[stockPodTemplateHashLabel]
	if stockHash == desiredHash {
		return nil, nil
	}
	sessionID := svc.Labels["agentapi.proxy/session-id"]
	if sessionID == "" {
		return []string{fmt.Sprintf("service %s has no session-id", svc.Name)}, nil
	}
	log.Printf("[STOCK_INVENTORY] Recreating stock session %s after pod template hash changed (%q -> %q, dind=%t)", sessionID, stockHash, desiredHash, dind)
	return m.deleteStockSessionResources(ctx, svc.Name, sessionID), nil
}

func (m *KubernetesSessionManager) deleteStockSessionResources(ctx context.Context, serviceName, sessionID string) []string {
	deletePolicy := metav1.DeletePropagationForeground
	deleteOptions := metav1.DeleteOptions{PropagationPolicy: &deletePolicy}
	var purgeErrs []string
	if err := m.client.CoreV1().Services(m.namespace).Delete(ctx, serviceName, deleteOptions); err != nil && !errors.IsNotFound(err) {
		purgeErrs = append(purgeErrs, fmt.Sprintf("service %s: %v", serviceName, err))
	}
	workloadName := fmt.Sprintf("agentapi-session-%s", sessionID)
	if err := m.client.AppsV1().Deployments(m.namespace).Delete(ctx, workloadName, deleteOptions); err != nil && !errors.IsNotFound(err) {
		purgeErrs = append(purgeErrs, fmt.Sprintf("deployment %s: %v", workloadName, err))
	}
	if err := m.client.CoreV1().Pods(m.namespace).Delete(ctx, workloadName, deleteOptions); err != nil && !errors.IsNotFound(err) {
		purgeErrs = append(purgeErrs, fmt.Sprintf("pod %s: %v", workloadName, err))
	}
	pvcName := fmt.Sprintf("agentapi-session-%s-pvc", sessionID)
	if err := m.client.CoreV1().PersistentVolumeClaims(m.namespace).Delete(ctx, pvcName, deleteOptions); err != nil && !errors.IsNotFound(err) {
		purgeErrs = append(purgeErrs, fmt.Sprintf("pvc %s: %v", pvcName, err))
	}
	return purgeErrs
}

// ClaimPoolStockSession removes the stock marker from a pool runner's Service
// after the runner accepts an allocation. The Service is the durable local
// source of truth because every session workload owns one, and removing the
// marker keeps active runners out of all stock selectors across manager restarts.
func (m *KubernetesSessionManager) ClaimPoolStockSession(ctx context.Context, sessionID, runnerToken string, settings *sessionsettings.SessionSettings) error {
	var parentRuntime *sessionsettings.ParentRuntimeConfig
	if settings != nil {
		parentRuntime = settings.ParentRuntime
	}
	if parentRuntime == nil || !parentRuntime.Enabled || parentRuntime.Endpoint == "" || parentRuntime.SessionID == "" || parentRuntime.ManagerID == "" || parentRuntime.Token == "" || parentRuntime.Generation <= 0 {
		return fmt.Errorf("invalid parent runtime")
	}
	credential, err := m.client.CoreV1().Secrets(m.namespace).Get(ctx, runnerCredentialSecretName(sessionID), metav1.GetOptions{})
	if err != nil {
		return ErrInvalidRunnerToken
	}
	expectedToken := credential.Data["runner-token"]
	if len(expectedToken) == 0 || subtle.ConstantTimeCompare(expectedToken, []byte(runnerToken)) != 1 {
		return ErrInvalidRunnerToken
	}
	if err := m.PrepareSessionResume(ctx, sessionID, settings); err != nil {
		return fmt.Errorf("persist claimed runner settings: %w", err)
	}
	name := fmt.Sprintf("agentapi-session-%s-svc", sessionID)
	if err := retry.RetryOnConflict(retry.DefaultRetry, func() error {
		svc, err := m.client.CoreV1().Services(m.namespace).Get(ctx, name, metav1.GetOptions{})
		if err != nil {
			return err
		}
		if svc.Labels["agentapi.proxy/session-pool"] == "" {
			return fmt.Errorf("session %s is not a pool runner", sessionID)
		}
		delete(svc.Labels, "agentapi.proxy/stock")
		svc.Labels = claimedRunnerIdentityLabels(svc.Labels, settings)
		updated, err := m.client.CoreV1().Services(m.namespace).Update(ctx, svc, metav1.UpdateOptions{})
		if err == nil && updated == nil {
			return fmt.Errorf("service update returned no object")
		}
		return err
	}); err != nil {
		return err
	}
	if err := m.updateClaimedRunnerIdentityLabels(ctx, sessionID, settings); err != nil {
		return err
	}
	// Ready may have been reported while this Service still carried the stock
	// label. In that race ScheduleSessionSuspend intentionally does nothing, so
	// schedule once more after the claim is durable. The reconciler also repairs
	// this state if the best-effort call fails during a transient API outage.
	if err := m.ScheduleSessionSuspend(ctx, sessionID); err != nil {
		log.Printf("[K8S_SESSION] Failed to schedule suspend after claiming stock session %s: %v", sessionID, err)
	}
	return nil
}

func claimedRunnerIdentityLabels(labels map[string]string, settings *sessionsettings.SessionSettings) map[string]string {
	if labels == nil {
		labels = make(map[string]string)
	}
	if settings == nil {
		return labels
	}
	labels["agentapi.proxy/user-id"] = sanitizeLabelValue(settings.Session.UserID)
	scope := settings.Session.Scope
	if scope == "" {
		scope = string(entities.ScopeUser)
	}
	labels["agentapi.proxy/scope"] = scope
	if settings.Session.TeamID == "" {
		delete(labels, "agentapi.proxy/team-id-hash")
	} else {
		labels["agentapi.proxy/team-id-hash"] = hashTeamID(settings.Session.TeamID)
	}
	return labels
}

func (m *KubernetesSessionManager) updateClaimedRunnerIdentityLabels(ctx context.Context, sessionID string, settings *sessionsettings.SessionSettings) error {
	name := fmt.Sprintf("agentapi-session-%s", sessionID)
	if m.isPVCEnabled() {
		if err := retry.RetryOnConflict(retry.DefaultRetry, func() error {
			deployment, err := m.client.AppsV1().Deployments(m.namespace).Get(ctx, name, metav1.GetOptions{})
			if err != nil {
				return err
			}
			deployment.Labels = claimedRunnerIdentityLabels(deployment.Labels, settings)
			updated, err := m.client.AppsV1().Deployments(m.namespace).Update(ctx, deployment, metav1.UpdateOptions{})
			if err == nil && updated == nil {
				return fmt.Errorf("deployment update returned no object")
			}
			return err
		}); err != nil && !errors.IsNotFound(err) {
			return fmt.Errorf("update claimed runner deployment identity labels: %w", err)
		}
	} else if err := retry.RetryOnConflict(retry.DefaultRetry, func() error {
		pod, err := m.client.CoreV1().Pods(m.namespace).Get(ctx, name, metav1.GetOptions{})
		if err != nil {
			return err
		}
		pod.Labels = claimedRunnerIdentityLabels(pod.Labels, settings)
		updated, err := m.client.CoreV1().Pods(m.namespace).Update(ctx, pod, metav1.UpdateOptions{})
		if err == nil && updated == nil {
			return fmt.Errorf("pod update returned no object")
		}
		return err
	}); err != nil && !errors.IsNotFound(err) {
		return fmt.Errorf("update claimed runner pod identity labels: %w", err)
	}

	if err := retry.RetryOnConflict(retry.DefaultRetry, func() error {
		secret, err := m.client.CoreV1().Secrets(m.namespace).Get(ctx, runnerCredentialSecretName(sessionID), metav1.GetOptions{})
		if err != nil {
			return err
		}
		secret.Labels = claimedRunnerIdentityLabels(secret.Labels, settings)
		updated, err := m.client.CoreV1().Secrets(m.namespace).Update(ctx, secret, metav1.UpdateOptions{})
		if err == nil && updated == nil {
			return fmt.Errorf("secret update returned no object")
		}
		return err
	}); err != nil && !errors.IsNotFound(err) {
		return fmt.Errorf("update claimed runner credential identity labels: %w", err)
	}
	return nil
}
