package services

import (
	"context"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
	"sigs.k8s.io/yaml"

	"github.com/takutakahashi/agentapi-proxy/internal/domain/entities"
	"github.com/takutakahashi/agentapi-proxy/pkg/config"
	"github.com/takutakahashi/agentapi-proxy/pkg/logger"
	"github.com/takutakahashi/agentapi-proxy/pkg/sessionsettings"
)

func TestEnsureSessionWorkloadRecreatesMissingDeployment(t *testing.T) {
	pvcEnabled := true
	cfg := &config.Config{KubernetesSession: config.KubernetesSessionConfig{
		Namespace: "test-ns", Image: "test-image:latest", BasePort: 9000,
		PVCEnabled: &pvcEnabled, CPURequest: "100m", CPULimit: "1",
		MemoryRequest: "128Mi", MemoryLimit: "512Mi",
	}, SessionPersistence: config.SessionPersistenceConfig{Backend: "volume", SuspendAfter: "1h"}}
	settingsData, err := sessionsettings.MarshalYAML(&sessionsettings.SessionSettings{Session: sessionsettings.SessionMeta{
		ID: "session-1", UserID: "user-1", AgentType: "codex-acp", PersistenceEnabled: true,
	}})
	if err != nil {
		t.Fatal(err)
	}
	client := fake.NewSimpleClientset(
		&corev1.Service{ObjectMeta: metav1.ObjectMeta{Name: "agentapi-session-session-1-svc", Namespace: "test-ns", Annotations: map[string]string{
			sessionSuspendedAtAnnotation: time.Now().UTC().Format(time.RFC3339Nano),
		}}},
		&corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: "agentapi-session-session-1-settings", Namespace: "test-ns"}, Data: map[string][]byte{"settings.yaml": settingsData}},
	)
	manager, err := NewKubernetesSessionManagerWithClient(cfg, false, logger.NewLogger(), client)
	if err != nil {
		t.Fatal(err)
	}
	if manager.suspendCancel != nil {
		manager.suspendCancel()
	}
	request := &entities.RunServerRequest{UserID: "user-1", AgentType: "codex-acp"}
	session := NewKubernetesSession(
		"session-1", request, "agentapi-session-session-1",
		"agentapi-session-session-1-svc", "agentapi-session-session-1-pvc",
		"test-ns", 9000, nil, nil,
	)
	manager.sessions[session.id] = session
	session.SetStatus("suspended")

	got, restoring, err := manager.EnsureSessionWorkload(context.Background(), session.id)
	if err != nil {
		t.Fatal(err)
	}
	if got == nil || !restoring {
		t.Fatalf("got session=%v restoring=%v", got, restoring)
	}
	if _, err := client.AppsV1().Deployments("test-ns").Get(context.Background(), session.DeploymentName(), metav1.GetOptions{}); err != nil {
		t.Fatalf("deployment was not recreated: %v", err)
	}
	deployment, err := client.AppsV1().Deployments("test-ns").Get(context.Background(), session.DeploymentName(), metav1.GetOptions{})
	if err != nil {
		t.Fatal(err)
	}
	deployment.Status.ReadyReplicas = 1
	if _, err := client.AppsV1().Deployments("test-ns").UpdateStatus(context.Background(), deployment, metav1.UpdateOptions{}); err != nil {
		t.Fatal(err)
	}
	service, err := client.CoreV1().Services("test-ns").Get(context.Background(), session.ServiceName(), metav1.GetOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if service.Annotations[sessionSuspendedAtAnnotation] != "" {
		t.Fatalf("suspended annotation was not cleared: %#v", service.Annotations)
	}
	secret, err := client.CoreV1().Secrets("test-ns").Get(context.Background(), "agentapi-session-session-1-settings", metav1.GetOptions{})
	if err != nil {
		t.Fatal(err)
	}
	var resumedSettings sessionsettings.SessionSettings
	if err := yaml.Unmarshal(secret.Data["settings.yaml"], &resumedSettings); err != nil {
		t.Fatal(err)
	}
	if resumedSettings.Session.ResumeFrom != session.ID() {
		t.Fatalf("resume_from = %q, want %q", resumedSettings.Session.ResumeFrom, session.ID())
	}
	deadline := time.Now().Add(3 * time.Second)
	for service.Annotations[sessionSuspendAtAnnotation] == "" && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
		service, err = client.CoreV1().Services("test-ns").Get(context.Background(), session.ServiceName(), metav1.GetOptions{})
		if err != nil {
			t.Fatal(err)
		}
	}
	if service.Annotations[sessionSuspendAtAnnotation] == "" {
		t.Fatalf("post-resume suspend deadline was not scheduled: %#v", service.Annotations)
	}
}
