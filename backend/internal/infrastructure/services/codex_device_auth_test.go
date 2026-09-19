package services

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/takutakahashi/agentapi-proxy/pkg/codexauth"
	"github.com/takutakahashi/agentapi-proxy/pkg/config"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
)

func TestStartCodexDeviceAuthCreatesIsolatedPod(t *testing.T) {
	client := fake.NewSimpleClientset()
	manager := &KubernetesSessionManager{
		client: client, namespace: "test",
		k8sConfig: &config.KubernetesSessionConfig{Image: "example/session:dev", ImagePullPolicy: "IfNotPresent"},
	}
	request := codexauth.WorkloadRequest{
		AttemptID: "cda-0123456789abcdef", CallbackURL: "https://proxy.example/internal/codex-device-auth",
		Token: "secret-token", ExpiresAt: time.Now().Add(5 * time.Minute),
	}
	if err := manager.StartCodexDeviceAuth(context.Background(), request); err != nil {
		t.Fatal(err)
	}
	name := codexDeviceAuthResourceName(request.AttemptID)
	pod, err := client.CoreV1().Pods("test").Get(context.Background(), name, metav1.GetOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if pod.Spec.AutomountServiceAccountToken == nil || *pod.Spec.AutomountServiceAccountToken {
		t.Fatal("service account token must not be mounted")
	}
	if pod.Spec.SecurityContext == nil || pod.Spec.SecurityContext.RunAsUser == nil || *pod.Spec.SecurityContext.RunAsUser != 999 ||
		pod.Spec.SecurityContext.RunAsGroup == nil || *pod.Spec.SecurityContext.RunAsGroup != 999 ||
		pod.Spec.SecurityContext.FSGroup == nil || *pod.Spec.SecurityContext.FSGroup != 999 {
		t.Fatalf("auth pod must run with the image runtime UID/GID: %#v", pod.Spec.SecurityContext)
	}
	if len(pod.Spec.Containers) != 1 || pod.Spec.Containers[0].Image != "example/session:dev" {
		t.Fatalf("unexpected containers: %#v", pod.Spec.Containers)
	}
	if len(pod.Spec.Volumes) != 3 || pod.Spec.Volumes[1].EmptyDir == nil || pod.Spec.Volumes[2].EmptyDir == nil {
		t.Fatalf("auth pod must only have request, ephemeral Codex home, and tmp volumes: %#v", pod.Spec.Volumes)
	}
	secret, err := client.CoreV1().Secrets("test").Get(context.Background(), name, metav1.GetOptions{})
	if err != nil {
		t.Fatal(err)
	}
	var stored codexauth.WorkloadRequest
	if err := json.Unmarshal(secret.Data["request.json"], &stored); err != nil {
		t.Fatal(err)
	}
	if stored.Token != request.Token {
		t.Fatal("worker token was not stored in the Secret")
	}
	if _, present := pod.Labels["secret-token"]; present {
		t.Fatal("token leaked into labels")
	}
}

func TestCancelCodexDeviceAuthDeletesResources(t *testing.T) {
	client := fake.NewSimpleClientset()
	manager := &KubernetesSessionManager{client: client, namespace: "test", k8sConfig: &config.KubernetesSessionConfig{Image: "session"}}
	request := codexauth.WorkloadRequest{AttemptID: "cda-deadbeef", CallbackURL: "https://proxy.example/internal/codex-device-auth", Token: "token", ExpiresAt: time.Now().Add(time.Minute)}
	if err := manager.StartCodexDeviceAuth(context.Background(), request); err != nil {
		t.Fatal(err)
	}
	if err := manager.CancelCodexDeviceAuth(context.Background(), request.AttemptID); err != nil {
		t.Fatal(err)
	}
	name := codexDeviceAuthResourceName(request.AttemptID)
	if _, err := client.CoreV1().Pods("test").Get(context.Background(), name, metav1.GetOptions{}); err == nil {
		t.Fatal("pod was not deleted")
	}
	if _, err := client.CoreV1().Secrets("test").Get(context.Background(), name, metav1.GetOptions{}); err == nil {
		t.Fatal("secret was not deleted")
	}
}

// TestStartCodexDeviceAuthInjectsCLIImage guards the split-image contract: the
// agent-assets image no longer contains ccplant, so the worker Pod must get the
// binary from the CLI image through an init container exactly like session Pods.
func TestStartCodexDeviceAuthInjectsCLIImage(t *testing.T) {
	client := fake.NewSimpleClientset()
	manager := &KubernetesSessionManager{
		client: client, namespace: "test",
		k8sConfig: &config.KubernetesSessionConfig{Image: "example/agent:assets", CLIImage: "example/cli:v1", ImagePullPolicy: "IfNotPresent"},
	}
	request := codexauth.WorkloadRequest{
		AttemptID: "cda-0123456789abcdef", CallbackURL: "https://proxy.example/internal/codex-device-auth",
		Token: "secret-token", ExpiresAt: time.Now().Add(5 * time.Minute),
	}
	if err := manager.StartCodexDeviceAuth(context.Background(), request); err != nil {
		t.Fatal(err)
	}
	pod, err := client.CoreV1().Pods("test").Get(context.Background(), codexDeviceAuthResourceName(request.AttemptID), metav1.GetOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(pod.Spec.Containers) != 1 || pod.Spec.Containers[0].Image != "example/agent:assets" {
		t.Fatalf("auth pod must keep the session image: %#v", pod.Spec.Containers)
	}
	if len(pod.Spec.InitContainers) != 1 || pod.Spec.InitContainers[0].Image != "example/cli:v1" {
		t.Fatalf("auth pod must install the CLI from the CLI image: %#v", pod.Spec.InitContainers)
	}
	if init := pod.Spec.InitContainers[0]; init.VolumeMounts[0].MountPath != "/ccplant-cli" {
		t.Fatalf("init container must not shadow the CLI source: %#v", init.VolumeMounts)
	}
	if len(pod.Spec.Containers[0].Command) == 0 || pod.Spec.Containers[0].Command[0] != sessionCLIPath {
		t.Fatalf("auth pod must invoke the injected CLI: %#v", pod.Spec.Containers[0].Command)
	}
	var cliMount *corev1.VolumeMount
	for i := range pod.Spec.Containers[0].VolumeMounts {
		if pod.Spec.Containers[0].VolumeMounts[i].Name == "ccplant-cli" {
			cliMount = &pod.Spec.Containers[0].VolumeMounts[i]
		}
	}
	if cliMount == nil || cliMount.MountPath != "/opt/ccplant/bin" || !cliMount.ReadOnly {
		t.Fatalf("auth pod must mount the injected CLI read-only: %#v", pod.Spec.Containers[0].VolumeMounts)
	}
}

// TestStartCodexDeviceAuthPodOutlivesAttempt guards the reporting window: the
// worker only reports after the Codex device login finishes or the attempt
// expires, so the Pod deadline must be later than the attempt expiry.
func TestStartCodexDeviceAuthPodOutlivesAttempt(t *testing.T) {
	client := fake.NewSimpleClientset()
	manager := &KubernetesSessionManager{client: client, namespace: "test", k8sConfig: &config.KubernetesSessionConfig{Image: "session"}}
	request := codexauth.WorkloadRequest{
		AttemptID: "cda-0123456789abcdef", CallbackURL: "https://proxy.example/internal/codex-device-auth",
		Token: "token", ExpiresAt: time.Now().Add(15 * time.Minute),
	}
	if err := manager.StartCodexDeviceAuth(context.Background(), request); err != nil {
		t.Fatal(err)
	}
	pod, err := client.CoreV1().Pods("test").Get(context.Background(), codexDeviceAuthResourceName(request.AttemptID), metav1.GetOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if pod.Spec.ActiveDeadlineSeconds == nil {
		t.Fatal("auth pod must carry an active deadline")
	}
	if got := time.Duration(*pod.Spec.ActiveDeadlineSeconds) * time.Second; got <= 15*time.Minute {
		t.Fatalf("auth pod deadline %s must outlive the 15m attempt", got)
	}
}

func TestStartCodexDeviceAuthExpiredAttemptKeepsShortDeadline(t *testing.T) {
	client := fake.NewSimpleClientset()
	manager := &KubernetesSessionManager{client: client, namespace: "test", k8sConfig: &config.KubernetesSessionConfig{Image: "session"}}
	request := codexauth.WorkloadRequest{
		AttemptID: "cda-0123456789abcdef", CallbackURL: "https://proxy.example/internal/codex-device-auth",
		Token: "token", ExpiresAt: time.Now().Add(-time.Minute),
	}
	if err := manager.StartCodexDeviceAuth(context.Background(), request); err != nil {
		t.Fatal(err)
	}
	pod, err := client.CoreV1().Pods("test").Get(context.Background(), codexDeviceAuthResourceName(request.AttemptID), metav1.GetOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if pod.Spec.ActiveDeadlineSeconds == nil || *pod.Spec.ActiveDeadlineSeconds != 1 {
		t.Fatalf("expired attempt must use the minimal pod deadline: %#v", pod.Spec.ActiveDeadlineSeconds)
	}
}
