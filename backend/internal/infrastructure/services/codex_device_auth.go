package services

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/takutakahashi/agentapi-proxy/pkg/codexauth"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const codexDeviceAuthLabel = "agentapi.proxy/codex-device-auth"

// The auth worker holds the Codex device login open until the attempt expires
// and only then reports the outcome. The Pod must therefore outlive the
// attempt, otherwise Kubernetes kills the worker before it can deliver the
// final callback and the attempt is stuck until the API expires it.
const (
	codexAuthPodDeadlineGrace = 90 * time.Second
	codexAuthPodMaxDeadline   = 17 * time.Minute
)

func codexDeviceAuthResourceName(attemptID string) string {
	id := strings.TrimPrefix(attemptID, "cda-")
	id = strings.ToLower(id)
	if len(id) > 40 {
		id = id[:40]
	}
	return "codex-auth-" + id
}

// configureCodexDeviceAuthCLI injects the ccplant binary into the auth worker
// Pod the same way session Pods receive it: when a CLI image is configured, an
// init container copies the binary out of that image into a shared volume. The
// agent-assets image used for the worker does not bundle ccplant, so without
// this injection the container fails to start with "exec: ccplant:
// executable file not found in $PATH". Legacy single-image deployments keep
// running the binary directly from their image.
func (m *KubernetesSessionManager) configureCodexDeviceAuthCLI(pod *corev1.Pod) {
	if m.k8sConfig == nil || len(pod.Spec.Containers) == 0 {
		return
	}
	cliImage := strings.TrimSpace(m.k8sConfig.CLIImage)
	if cliImage == "" {
		return
	}
	pod.Spec.Volumes = append(pod.Spec.Volumes, corev1.Volume{
		Name:         "ccplant-cli",
		VolumeSource: corev1.VolumeSource{EmptyDir: &corev1.EmptyDirVolumeSource{}},
	})
	// Mount the destination outside /opt/ccplant/bin in the init container: the
	// compatibility image links /usr/local/bin/ccplant there, and mounting an
	// empty volume over it would hide the binary we need to copy.
	pod.Spec.InitContainers = append(pod.Spec.InitContainers, corev1.Container{
		Name:            "install-ccplant-cli",
		Image:           cliImage,
		ImagePullPolicy: corev1.PullPolicy(m.k8sConfig.ImagePullPolicy),
		Command:         []string{"/bin/sh", "-ec", "cp -f /usr/local/bin/ccplant /ccplant-cli/ccplant && chmod 0555 /ccplant-cli/ccplant"},
		VolumeMounts:    []corev1.VolumeMount{{Name: "ccplant-cli", MountPath: "/ccplant-cli"}},
	})
	worker := &pod.Spec.Containers[0]
	worker.Command = []string{sessionCLIPath, "codex-auth-worker", "--request-file", "/run/agentapi/request.json"}
	worker.VolumeMounts = append(worker.VolumeMounts, corev1.VolumeMount{Name: "ccplant-cli", MountPath: "/opt/ccplant/bin", ReadOnly: true})
}

// StartCodexDeviceAuth creates a purpose-built, short-lived Pod. It deliberately
// does not mount repositories, managed files, credentials, PVCs, or a service
// account token.
func (m *KubernetesSessionManager) StartCodexDeviceAuth(ctx context.Context, req codexauth.WorkloadRequest) error {
	if req.AttemptID == "" || req.CallbackURL == "" || req.Token == "" || req.ExpiresAt.IsZero() {
		return fmt.Errorf("attempt_id, callback_url, token, and expires_at are required")
	}
	name := codexDeviceAuthResourceName(req.AttemptID)
	log.Printf("[CODEX_AUTH_K8S] Preparing attempt %s resource=%s namespace=%s", req.AttemptID, name, m.namespace)
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
		log.Printf("[CODEX_AUTH_K8S] Failed to create Secret for attempt %s resource=%s: %v", req.AttemptID, name, err)
		if !apierrors.IsAlreadyExists(err) {
			return fmt.Errorf("create Codex auth Secret: %w", err)
		}
		return fmt.Errorf("Codex auth attempt already exists")
	}
	log.Printf("[CODEX_AUTH_K8S] Created Secret for attempt %s resource=%s", req.AttemptID, name)
	cleanup := func() {
		if err := m.client.CoreV1().Secrets(m.namespace).Delete(context.Background(), name, metav1.DeleteOptions{}); err != nil && !apierrors.IsNotFound(err) {
			log.Printf("[CODEX_AUTH_K8S] Failed to clean up Secret for attempt %s resource=%s: %v", req.AttemptID, name, err)
			return
		}
		log.Printf("[CODEX_AUTH_K8S] Cleaned up Secret for attempt %s resource=%s after Pod creation failure", req.AttemptID, name)
	}
	deadline := int64(codexAuthPodMaxDeadline.Seconds())
	switch remaining := req.ExpiresAt.Sub(metav1.Now().Time); {
	case remaining <= 0:
		deadline = 1
	case int64((remaining + codexAuthPodDeadlineGrace).Seconds()) < deadline:
		deadline = int64((remaining + codexAuthPodDeadlineGrace).Seconds())
	}
	noPrivilegeEscalation := false
	readOnlyRoot := true
	runAsNonRoot := true
	runtimeID := int64(999)
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
			SecurityContext: &corev1.PodSecurityContext{
				RunAsNonRoot: &runAsNonRoot,
				RunAsUser:    &runtimeID,
				RunAsGroup:   &runtimeID,
				FSGroup:      &runtimeID,
			},
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
	m.configureCodexDeviceAuthCLI(pod)
	log.Printf("[CODEX_AUTH_K8S] Creating Pod for attempt %s resource=%s image=%q cli_image=%q deadline_seconds=%d", req.AttemptID, name, m.k8sConfig.Image, m.k8sConfig.CLIImage, deadline)
	if _, err := m.client.CoreV1().Pods(m.namespace).Create(ctx, pod, metav1.CreateOptions{}); err != nil {
		log.Printf("[CODEX_AUTH_K8S] Failed to create Pod for attempt %s resource=%s: %v", req.AttemptID, name, err)
		cleanup()
		return fmt.Errorf("create Codex auth Pod: %w", err)
	}
	log.Printf("[CODEX_AUTH_K8S] Created Pod for attempt %s resource=%s", req.AttemptID, name)
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
