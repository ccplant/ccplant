package services

import (
	"context"
	"testing"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"

	"github.com/takutakahashi/agentapi-proxy/internal/domain/entities"
	"github.com/takutakahashi/agentapi-proxy/pkg/config"
	"github.com/takutakahashi/agentapi-proxy/pkg/logger"
)

func newWatcherTestSession() *KubernetesSession {
	return NewKubernetesSession(
		"session-1", &entities.RunServerRequest{UserID: "user-1", AgentType: "claude-acp"}, "agentapi-session-session-1",
		"agentapi-session-session-1-svc", "agentapi-session-session-1-pvc",
		"test-ns", 9000, nil, nil,
	)
}

func waitForWatcherFlag(t *testing.T, flag func() bool, want bool) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for flag() != want && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if flag() != want {
		t.Fatalf("watcher active = %v, want %v", flag(), want)
	}
}

func TestStartAgentAPIStatusWatcherRunsOnePerSession(t *testing.T) {
	manager := &KubernetesSessionManager{}
	session := newWatcherTestSession()
	session.SetStatus("starting")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if !manager.startAgentAPIStatusWatcher(ctx, session) {
		t.Fatal("first watcher did not start")
	}
	if manager.startAgentAPIStatusWatcher(ctx, session) {
		t.Fatal("a second watcher started for the same session")
	}

	cancel()
	waitForWatcherFlag(t, session.agentStatusWatcherActive.Load, false)
	if !manager.startAgentAPIStatusWatcher(context.Background(), session) {
		t.Fatal("watcher did not restart after the previous one ended")
	}
	session.SetStatus("suspended")
}

func TestStartDeploymentStatusWatcherRunsOnePerSession(t *testing.T) {
	manager := &KubernetesSessionManager{}
	session := newWatcherTestSession()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if !manager.startDeploymentStatusWatcher(ctx, session) {
		t.Fatal("first watcher did not start")
	}
	if manager.startDeploymentStatusWatcher(ctx, session) {
		t.Fatal("a second watcher started for the same session")
	}
	cancel()
	waitForWatcherFlag(t, session.deploymentWatcherActive.Load, false)
}

func TestEnsureSessionWorkloadWatchesWorkloadRecreatedByAnotherReplica(t *testing.T) {
	pvcEnabled := true
	cfg := &config.Config{KubernetesSession: config.KubernetesSessionConfig{
		Namespace: "test-ns", Image: "test-image:latest", BasePort: 9000,
		PVCEnabled: &pvcEnabled, CPURequest: "100m", CPULimit: "1",
		MemoryRequest: "128Mi", MemoryLimit: "512Mi",
	}, SessionPersistence: config.SessionPersistenceConfig{Backend: "volume", SuspendAfter: "1h"}}
	client := fake.NewSimpleClientset(
		&corev1.Service{ObjectMeta: metav1.ObjectMeta{Name: "agentapi-session-session-1-svc", Namespace: "test-ns"}},
		&appsv1.Deployment{ObjectMeta: metav1.ObjectMeta{Name: "agentapi-session-session-1", Namespace: "test-ns"}},
	)
	manager, err := NewKubernetesSessionManagerWithClient(cfg, false, logger.NewLogger(), client)
	if err != nil {
		t.Fatal(err)
	}
	if manager.suspendCancel != nil {
		manager.suspendCancel()
	}
	session := newWatcherTestSession()
	manager.sessions[session.id] = session
	session.SetStatus("suspended")

	got, restoring, err := manager.EnsureSessionWorkload(context.Background(), session.id)
	if err != nil {
		t.Fatal(err)
	}
	if got == nil || !restoring {
		t.Fatalf("got session=%v restoring=%v", got, restoring)
	}
	if session.Status() != "starting" {
		t.Fatalf("status = %q, want starting", session.Status())
	}
	if !session.deploymentWatcherActive.Load() {
		t.Fatal("deployment status watcher was not started")
	}
	if !session.agentStatusWatcherActive.Load() {
		t.Fatal("agent status watcher was not started")
	}
	session.SetStatus("suspended")
}
