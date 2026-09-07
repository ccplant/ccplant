package services

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/takutakahashi/agentapi-proxy/pkg/codexauth"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const codexDeviceAuthLabel = "agentapi.proxy/codex-device-auth"

func codexDeviceAuthResourceName(attemptID string) string {
	id := strings.TrimPrefix(attemptID, "cda_")
	id = strings.ToLower(strings.ReplaceAll(id, "_", "-"))
	if len(id) > 40 {
		id = id[:40]
	}
	return "codex-auth-" + id
}

// StartCodexDeviceAuth creates a purpose-built, short-lived Pod. It deliberately
// does not mount repositories, managed files, credentials, PVCs, or a service
// account token.
func (m *KubernetesSessionManager) StartCodexDeviceAuth(ctx context.Context, req codexauth.WorkloadRequest) error {
	if req.AttemptID == "" || req.CallbackURL == "" || req.Token == "" || req.ExpiresAt.IsZero() {
		return fmt.Errorf("attempt_id, callback_url, token, and expires_at are required")
	}
	name := codexDeviceAuthResourceName(req.AttemptID)
	payload, err := json.Marshal(req)
	if err != nil {
		return fmt.Errorf("encode Codex auth request: %w", err)
	}
	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: m.namespace, Labels: map[string]string{codexDeviceAuthLabel: "true"}},
		Type:       corev1.SecretTypeOpaque,
		Data:       map[string][]byte{"request.json": payload},
	}
	if _, err := m.client.CoreV1().Secrets(m.namespace).Create(ctx, secret, metav1.CreateOptions{}); err != nil {
		if !apierrors.IsAlreadyExists(err) {
			return fmt.Errorf("create Codex auth Secret: %w", err)
		}
		return fmt.Errorf("Codex auth attempt already exists")
	}
	cleanup := func() {
		_ = m.client.CoreV1().Secrets(m.namespace).Delete(context.Background(), name, metav1.DeleteOptions{})
	}
	deadline := int64(600)
	if seconds := int64(req.ExpiresAt.Sub(metav1.Now().Time).Seconds()); seconds > 0 && seconds < deadline {
		deadline = seconds
	}
	noPrivilegeEscalation := false
	readOnlyRoot := true
	runAsNonRoot := true
	zero := int64(0)
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name: name, Namespace: m.namespace,
			Labels: map[string]string{"app.kubernetes.io/managed-by": "agentapi-proxy", codexDeviceAuthLabel: "true"},
		},
		Spec: corev1.PodSpec{
			RestartPolicy:                 corev1.RestartPolicyNever,
			AutomountServiceAccountToken:  &noPrivilegeEscalation,
			ActiveDeadlineSeconds:         &deadline,
			TerminationGracePeriodSeconds: &zero,
			SecurityContext:               &corev1.PodSecurityContext{RunAsNonRoot: &runAsNonRoot},
			Volumes: []corev1.Volume{
				{Name: "request", VolumeSource: corev1.VolumeSource{Secret: &corev1.SecretVolumeSource{SecretName: name, DefaultMode: int32Ptr(0400)}}},
				{Name: "home", VolumeSource: corev1.VolumeSource{EmptyDir: &corev1.EmptyDirVolumeSource{}}},
				{Name: "tmp", VolumeSource: corev1.VolumeSource{EmptyDir: &corev1.EmptyDirVolumeSource{}}},
			},
			Containers: []corev1.Container{{
				Name: "codex-auth", Image: m.k8sConfig.Image, ImagePullPolicy: corev1.PullPolicy(m.k8sConfig.ImagePullPolicy),
				Command: []string{"ccplant", "codex-auth-worker", "--request-file", "/run/agentapi/request.json"},
				VolumeMounts: []corev1.VolumeMount{
					{Name: "request", MountPath: "/run/agentapi", ReadOnly: true},
					{Name: "home", MountPath: "/home/agentapi/.codex"},
					{Name: "tmp", MountPath: "/tmp"},
				},
				SecurityContext: &corev1.SecurityContext{
					AllowPrivilegeEscalation: &noPrivilegeEscalation, ReadOnlyRootFilesystem: &readOnlyRoot,
					RunAsNonRoot: &runAsNonRoot, Capabilities: &corev1.Capabilities{Drop: []corev1.Capability{"ALL"}},
				},
				Resources: corev1.ResourceRequirements{
					Requests: corev1.ResourceList{corev1.ResourceCPU: resource.MustParse("100m"), corev1.ResourceMemory: resource.MustParse("128Mi")},
					Limits:   corev1.ResourceList{corev1.ResourceCPU: resource.MustParse("500m"), corev1.ResourceMemory: resource.MustParse("512Mi")},
				},
			}},
		},
	}
	if _, err := m.client.CoreV1().Pods(m.namespace).Create(ctx, pod, metav1.CreateOptions{}); err != nil {
		cleanup()
		return fmt.Errorf("create Codex auth Pod: %w", err)
	}
	return nil
}

func (m *KubernetesSessionManager) CancelCodexDeviceAuth(ctx context.Context, attemptID string) error {
	name := codexDeviceAuthResourceName(attemptID)
	policy := metav1.DeletePropagationBackground
	err := m.client.CoreV1().Pods(m.namespace).Delete(ctx, name, metav1.DeleteOptions{PropagationPolicy: &policy})
	if err != nil && !apierrors.IsNotFound(err) {
		return err
	}
	err = m.client.CoreV1().Secrets(m.namespace).Delete(ctx, name, metav1.DeleteOptions{})
	if err != nil && !apierrors.IsNotFound(err) {
		return err
	}
	return nil
}

func int32Ptr(value int32) *int32 { return &value }

var _ codexauth.WorkloadLauncher = (*KubernetesSessionManager)(nil)
