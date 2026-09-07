package services

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/takutakahashi/agentapi-proxy/pkg/codexauth"
	"github.com/takutakahashi/agentapi-proxy/pkg/config"
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
		AttemptID: "cda_0123456789abcdef", CallbackURL: "https://proxy.example/internal/codex-device-auth",
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
	request := codexauth.WorkloadRequest{AttemptID: "cda_deadbeef", CallbackURL: "https://proxy.example/internal/codex-device-auth", Token: "token", ExpiresAt: time.Now().Add(time.Minute)}
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
