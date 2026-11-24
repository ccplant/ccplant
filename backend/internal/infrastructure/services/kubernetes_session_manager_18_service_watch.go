package services

import (
	"context"
	"fmt"
	"log"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func (m *KubernetesSessionManager) createService(ctx context.Context, session *KubernetesSession) error {
	return m.createServiceWithLabels(ctx, session, m.buildLabels(session))
}

func (m *KubernetesSessionManager) createServiceWithLabels(ctx context.Context, session *KubernetesSession, labels map[string]string) error {
	annotations := map[string]string{
		"agentapi.proxy/created-at": session.startedAt.Format(time.RFC3339),
		"agentapi.proxy/updated-at": session.startedAt.Format(time.RFC3339),
		"agentapi.proxy/team-id":    session.Request().TeamID, // Store original team_id (unsanitized)
	}
	if session.Request().AgentType != "" {
		annotations["agentapi.proxy/agent-type"] = session.Request().AgentType
	}
	// Store initial message in annotation so all proxy replicas can read it immediately,
	// without waiting for the settings Secret (which is created asynchronously).
	if session.Request().InitialMessage != "" {
		annotations["agentapi.proxy/initial-message"] = session.Request().InitialMessage
	}
	// Record the initial message time as the last message time for all sessions.
	// This annotation is updated by SendMessage when follow-up messages arrive.
	annotations["agentapi.proxy/last-message-at"] = session.startedAt.UTC().Format(time.RFC3339)
	if session.Request().SessionTTL != "" {
		annotations["agentapi.proxy/session-ttl"] = session.Request().SessionTTL
	}

	service := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:        session.ServiceName(),
			Namespace:   m.namespace,
			Labels:      labels,
			Annotations: annotations,
		},
		Spec: corev1.ServiceSpec{
			Type:      corev1.ServiceTypeClusterIP,
			ClusterIP: corev1.ClusterIPNone,
			Selector: map[string]string{
				"agentapi.proxy/session-id": session.id,
			},
			Ports: m.buildServicePorts(session),
		},
	}

	created, err := m.client.CoreV1().Services(m.namespace).Create(ctx, service, metav1.CreateOptions{})
	if err == nil {
		session.setServiceUID(string(created.UID))
	}
	return err
}

func (m *KubernetesSessionManager) sessionServiceOwnerReferences(ctx context.Context, sessionID string) []metav1.OwnerReference {
	if m == nil || m.client == nil {
		return nil
	}
	svcName := fmt.Sprintf("agentapi-session-%s-svc", sessionID)
	svc, err := m.client.CoreV1().Services(m.namespace).Get(ctx, svcName, metav1.GetOptions{})
	if err != nil {
		log.Printf("[K8S_SESSION] Warning: failed to get Service %s for owner reference: %v", svcName, err)
		return nil
	}
	return []metav1.OwnerReference{
		{
			APIVersion: "v1",
			Kind:       "Service",
			Name:       svc.Name,
			UID:        svc.UID,
		},
	}
}

// watchSession monitors the session deployment status
func (m *KubernetesSessionManager) watchSession(ctx context.Context, session *KubernetesSession) {
	defer func() {
		log.Printf("[K8S_SESSION] Session %s watch ended", session.id)
	}()
	// Wait for deployment to be ready
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()

	timeout := time.After(time.Duration(m.k8sConfig.PodStartTimeout) * time.Second)

	for {
		select {
		case <-ctx.Done():
			log.Printf("[K8S_SESSION] Session %s context cancelled", session.id)
			return

		case <-timeout:
			log.Printf("[K8S_SESSION] Session %s startup timeout", session.id)
			session.SetStatusMessage("Timed out waiting for the session workload to become ready")
			session.SetStatus("timeout")
			return

		case <-ticker.C:
			ready, err := m.isSessionWorkloadReady(context.Background(), session)
			if err != nil {
				if errors.IsNotFound(err) {
					log.Printf("[K8S_SESSION] Workload %s not found, session may have been deleted", session.DeploymentName())
					return
				}
				log.Printf("[K8S_SESSION] Error getting workload: %v", err)
				continue
			}

			// Check workload status
			if ready {
				session.SetStatus("starting")
				log.Printf("[K8S_SESSION] Session %s Pod is ready", session.id)

				log.Printf("[K8S_SESSION] Waiting for pull provision request to become ready for session %s", session.id)
				if err := m.waitForPullProvisioner(ctx, session); err != nil {
					log.Printf("[K8S_SESSION] Pull provisioner error for session %s: %v", session.id, err)
					session.SetStatusMessage(err.Error())
					session.SetStatus("error")
					return
				}

				session.SetStatus("active")
				log.Printf("[K8S_SESSION] Session %s is now active", session.id)
				if err := m.ScheduleSessionSuspend(ctx, session.ID()); err != nil {
					log.Printf("[K8S_SESSION] Failed to schedule initial suspend for session %s: %v", session.id, err)
				}

				// Continue watching deployment health. Runtime status is pushed outbound.
				m.runDeploymentStatusWatcher(ctx, session)
				return
			}

			session.SetStatus("starting")
		}
	}
}

// watchDeploymentStatus continuously watches the deployment status after it becomes ready
func (m *KubernetesSessionManager) watchDeploymentStatus(ctx context.Context, session *KubernetesSession) {
	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return

		case <-ticker.C:
			ready, err := m.isSessionWorkloadReady(context.Background(), session)
			if err != nil {
				if errors.IsNotFound(err) {
					if session.Status() != "suspended" {
						session.SetStatus("stopped")
					}
					return
				}
				continue
			}

			if !ready {
				session.SetStatus("unhealthy")
			} else {
				// Kubernetes readiness can recover an infrastructure-health failure,
				// but it must not erase terminal application/provisioning failures.
				// In particular, a Pod remains Ready after clone/provisioning errors.
				current := session.Status()
				if statusCanRecoverFromWorkloadReadiness(current) {
					session.SetStatus("active")
				}
			}
		}
	}
}

func (m *KubernetesSessionManager) startDeploymentStatusWatcher(ctx context.Context, session *KubernetesSession) bool {
	if !session.deploymentWatcherActive.CompareAndSwap(false, true) {
		return false
	}
	go func() {
		defer session.deploymentWatcherActive.Store(false)
		m.watchDeploymentStatus(ctx, session)
	}()
	return true
}

func (m *KubernetesSessionManager) runDeploymentStatusWatcher(ctx context.Context, session *KubernetesSession) {
	if !session.deploymentWatcherActive.CompareAndSwap(false, true) {
		return
	}
	defer session.deploymentWatcherActive.Store(false)
	m.watchDeploymentStatus(ctx, session)
}

func (m *KubernetesSessionManager) startWorkloadStatusWatchers(ctx context.Context, session *KubernetesSession) {
	m.startDeploymentStatusWatcher(ctx, session)
}

func statusWatcherTerminal(status string) bool {
	switch status {
	case "stopped", "suspended", "error", "timeout":
		return true
	default:
		return false
	}
}

func statusCanRecoverFromWorkloadReadiness(status string) bool {
	return status == "unhealthy" || status == "stopped"
}

// cleanupStockSessionResources deliberately does not inherit the request
// context. Stock creation failures commonly happen because that context has
// expired; reusing it would make every cleanup call fail immediately and leak
// the workload's CPU and memory reservations.
func (m *KubernetesSessionManager) cleanupStockSessionResources(session *KubernetesSession) error {
	timeout := time.Duration(m.k8sConfig.PodStopTimeout) * time.Second
	if timeout <= 0 {
		timeout = 30 * time.Second
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	return m.deleteSessionResources(ctx, session)
}

// deleteSessionResources deletes all Kubernetes resources for a session
func (m *KubernetesSessionManager) deleteSessionResources(ctx context.Context, session *KubernetesSession) error {
	deletePolicy := metav1.DeletePropagationForeground
	deleteOptions := metav1.DeleteOptions{
		PropagationPolicy: &deletePolicy,
	}

	var errs []string

	// Delete Service
	err := m.client.CoreV1().Services(m.namespace).Delete(ctx, session.ServiceName(), deleteOptions)
	if err != nil && !errors.IsNotFound(err) {
		errs = append(errs, fmt.Sprintf("service: %v", err))
	}

	// Delete workload. Try both kinds so sessions created before a PVC setting
	// change are cleaned up correctly.
	err = m.client.AppsV1().Deployments(m.namespace).Delete(ctx, session.DeploymentName(), deleteOptions)
	if err != nil && !errors.IsNotFound(err) {
		errs = append(errs, fmt.Sprintf("deployment: %v", err))
	}
	err = m.client.CoreV1().Pods(m.namespace).Delete(ctx, session.DeploymentName(), deleteOptions)
	if err != nil && !errors.IsNotFound(err) {
		errs = append(errs, fmt.Sprintf("pod: %v", err))
	}

	// Delete PVC if present. Do not depend on the current PVC setting because
	// old sessions may predate the setting.
	err = m.client.CoreV1().PersistentVolumeClaims(m.namespace).Delete(ctx, session.PVCName(), deleteOptions)
	if err != nil && !errors.IsNotFound(err) {
		errs = append(errs, fmt.Sprintf("pvc: %v", err))
	}

	// Delete webhook payload Secret
	if err := m.deleteWebhookPayloadSecret(ctx, session); err != nil {
		errs = append(errs, fmt.Sprintf("webhook-payload-secret: %v", err))
	}

	// Delete session settings Secret
	if err := m.deleteSessionSettingsSecret(ctx, session); err != nil {
		errs = append(errs, fmt.Sprintf("session-settings-secret: %v", err))
	}

	// Delete oneshot settings Secret (bug fix - was not being deleted before)
	if err := m.deleteOneshotSettingsSecret(ctx, session); err != nil {
		errs = append(errs, fmt.Sprintf("oneshot-settings-secret: %v", err))
	}

	if err := m.deleteProvisionRequest(ctx, session.id); err != nil {
		errs = append(errs, fmt.Sprintf("provision-request-secret: %v", err))
	}

	// Delete every Secret associated with the session, including allocation,
	// runner credential, and any resource types added in the future. Owner
	// references normally let Kubernetes garbage-collect these with the Service,
	// but they can be absent when creation only partially completed. Keeping this
	// label-based sweep makes explicit session deletion deterministic even for
	// those partially-created resources.
	if err := m.deleteSessionSecrets(ctx, session.id); err != nil {
		errs = append(errs, fmt.Sprintf("session-secrets: %v", err))
	}

	if len(errs) > 0 {
		return fmt.Errorf("failed to delete resources: %s", strings.Join(errs, ", "))
	}

	return nil
}

// deleteSessionSecrets removes all Secrets carrying this session's identity label.
// Session-scoped Secret creators must set agentapi.proxy/session-id.
func (m *KubernetesSessionManager) deleteSessionSecrets(ctx context.Context, sessionID string) error {
	secrets, err := m.client.CoreV1().Secrets(m.namespace).List(ctx, metav1.ListOptions{
		LabelSelector: "agentapi.proxy/session-id=" + sessionID,
	})
	if err != nil {
		return fmt.Errorf("list session secrets: %w", err)
	}

	var errs []string
	for i := range secrets.Items {
		name := secrets.Items[i].Name
		if err := m.client.CoreV1().Secrets(m.namespace).Delete(ctx, name, metav1.DeleteOptions{}); err != nil && !errors.IsNotFound(err) {
			errs = append(errs, fmt.Sprintf("%s: %v", name, err))
		}
	}
	if len(errs) > 0 {
		return fmt.Errorf("delete session secrets: %s", strings.Join(errs, ", "))
	}
	return nil
}

// cleanupSession removes a session from the internal map and releases its message subscribers.
func (m *KubernetesSessionManager) cleanupSession(id string) {
	m.mutex.Lock()
	delete(m.sessions, id)
	m.mutex.Unlock()
	// Close all per-session message subscribers to unblock any waiting long-poll handlers.
	// Called after releasing m.mutex to avoid holding two locks simultaneously.
	m.cleanupMessageSubs(id)
}

// buildLabelSelector builds a Kubernetes label selector string from entities.SessionFilter
