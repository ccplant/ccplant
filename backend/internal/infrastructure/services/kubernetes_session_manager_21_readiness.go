package services

import (
	"context"
	"fmt"
	"time"

	"github.com/takutakahashi/agentapi-proxy/internal/domain/entities"
	portrepos "github.com/takutakahashi/agentapi-proxy/internal/usecases/ports/repositories"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// SetPersonalAPIKeyRepository sets the personal API key repository
func (m *KubernetesSessionManager) SetPersonalAPIKeyRepository(repo portrepos.PersonalAPIKeyRepository) {
	m.personalAPIKeyRepo = repo
}

// GetPersonalAPIKeyRepository returns the personal API key repository
func (m *KubernetesSessionManager) GetPersonalAPIKeyRepository() portrepos.PersonalAPIKeyRepository {
	return m.personalAPIKeyRepo
}

// getSessionStatusFromDeployment determines session status from Deployment state
func (m *KubernetesSessionManager) getSessionStatusFromDeployment(sessionID string) string {
	deploymentName := fmt.Sprintf("agentapi-session-%s", sessionID)
	if !m.isPVCEnabled() {
		pod, err := m.client.CoreV1().Pods(m.namespace).Get(
			context.Background(), deploymentName, metav1.GetOptions{})
		if err != nil {
			if errors.IsNotFound(err) {
				return "stopped"
			}
			return "unknown"
		}
		if isPodReady(pod) {
			return "active"
		}
		if pod.Status.Phase == corev1.PodFailed || pod.Status.Phase == corev1.PodSucceeded {
			return "stopped"
		}
		return "starting"
	}

	deployment, err := m.client.AppsV1().Deployments(m.namespace).Get(
		context.Background(), deploymentName, metav1.GetOptions{})

	if err != nil {
		if errors.IsNotFound(err) {
			return "stopped"
		}
		return "unknown"
	}

	if deployment.Status.ReadyReplicas > 0 {
		return "active"
	}
	if deployment.Status.Replicas > 0 {
		return "starting"
	}
	return "unhealthy"
}

func (m *KubernetesSessionManager) isSessionWorkloadReady(ctx context.Context, session *KubernetesSession) (bool, error) {
	if m.isPVCEnabled() {
		deployment, err := m.client.AppsV1().Deployments(m.namespace).Get(ctx, session.DeploymentName(), metav1.GetOptions{})
		if err != nil {
			return false, err
		}
		if deployment.Status.ReadyReplicas > 0 {
			return true, nil
		}

		// Deployment status can lag behind the Pod status while a node or the
		// deployment controller is briefly unavailable. Stock adoption only needs
		// the provisioner and sidecars in the selected Pod to be ready, so consult
		// the Pod directly before continuing to wait. This also keeps the stock
		// creation request within the session-manager client's timeout.
		pods, err := m.client.CoreV1().Pods(m.namespace).List(ctx, metav1.ListOptions{
			LabelSelector: "agentapi.proxy/session-id=" + session.ID(),
		})
		if err != nil {
			return false, err
		}
		for i := range pods.Items {
			if pods.Items[i].DeletionTimestamp == nil && isPodReady(&pods.Items[i]) {
				return true, nil
			}
		}
		return false, nil
	}

	pod, err := m.client.CoreV1().Pods(m.namespace).Get(ctx, session.DeploymentName(), metav1.GetOptions{})
	if err != nil {
		return false, err
	}
	return isPodReady(pod), nil
}

func (m *KubernetesSessionManager) waitForSessionWorkloadReady(ctx context.Context, session *KubernetesSession) error {
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()

	for {
		ready, err := m.isSessionWorkloadReady(ctx, session)
		if err == nil && ready {
			return nil
		}

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
	}
}

func isPodReady(pod *corev1.Pod) bool {
	if pod.Status.Phase != corev1.PodRunning {
		return false
	}
	for _, condition := range pod.Status.Conditions {
		if condition.Type == corev1.PodReady && condition.Status == corev1.ConditionTrue {
			return true
		}
	}
	return false
}

// buildMainContainerVolumeMounts builds the volume mounts for the main container
func (m *KubernetesSessionManager) buildMainContainerVolumeMounts(session *KubernetesSession, req *entities.RunServerRequest) []corev1.VolumeMount {
	volumeMounts := []corev1.VolumeMount{
		{
			Name:      "workdir",
			MountPath: "/home/agentapi/workdir",
		},
		// dot-claude EmptyDir – used by main container for Claude Code settings
		{
			Name:      "dot-claude",
			MountPath: "/home/agentapi/.claude",
		},
		{
			Name:      "dot-docker",
			MountPath: "/home/agentapi/.docker",
		},
		// notification subscriptions source – read by setup on startup
		{
			Name:      "notification-subscriptions-source",
			MountPath: "/notification-subscriptions-source",
			ReadOnly:  true,
		},
	}
	if m.config.SessionPersistence.Backend == "volume" {
		volumeMounts = append(volumeMounts,
			corev1.VolumeMount{Name: "workdir", MountPath: "/home/agentapi/.claude/projects", SubPath: ".agentapi/persistent-home/claude-projects"},
			corev1.VolumeMount{Name: "workdir", MountPath: "/home/agentapi/.codex", SubPath: ".agentapi/persistent-home/codex"},
			corev1.VolumeMount{Name: "workdir", MountPath: "/home/agentapi/.pi/agent/sessions", SubPath: ".agentapi/persistent-home/pi-sessions"},
			corev1.VolumeMount{Name: "workdir", MountPath: "/home/agentapi/.local/state/herdr-core", SubPath: ".agentapi/persistent-home/herdr-core"},
		)
	}

	// session-settings Secret – optional mount for Pod restart auto-provisioning.
	volumeMounts = append(volumeMounts, corev1.VolumeMount{
		Name:      "session-settings",
		MountPath: "/session-settings",
		ReadOnly:  true,
	})
	for index, storage := range req.Storages {
		if storage.Managed {
			continue
		}
		volumeMounts = append(volumeMounts, corev1.VolumeMount{
			Name: storageVolumeName(index), MountPath: storage.MountPath, ReadOnly: storage.ReadOnly,
		})
	}

	if m.sciaSessionSidecarEnabled(req) {
		volumeMounts = append(volumeMounts, corev1.VolumeMount{
			Name:      "scia-mitm-ca",
			MountPath: "/etc/scia/mitm",
			ReadOnly:  true,
		})
	}

	return volumeMounts
}

func storageVolumeName(index int) string { return fmt.Sprintf("shared-storage-%d", index) }

// restoreSessionFromService restores a session from Kubernetes Service
