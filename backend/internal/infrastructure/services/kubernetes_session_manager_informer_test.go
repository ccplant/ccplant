package services

import (
	"context"
	"testing"
	"time"

	"github.com/takutakahashi/agentapi-proxy/internal/domain/entities"
	"github.com/takutakahashi/agentapi-proxy/pkg/config"
	"github.com/takutakahashi/agentapi-proxy/pkg/logger"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
	ktesting "k8s.io/client-go/testing"
)

func TestListSessionsUsesInformerWatchWithoutRelisting(t *testing.T) {
	t.Setenv("LOG_DIR", t.TempDir())
	cfg := config.DefaultConfig()
	cfg.KubernetesSession.Namespace = "test-ns"
	cfg.KubernetesSession.PVCEnabled = boolPtrForTest(false)
	client := fake.NewSimpleClientset()
	manager, err := NewKubernetesSessionManagerWithClient(cfg, false, logger.NewLogger(), client)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = manager.Shutdown(time.Second) })
	client.ClearActions()

	if sessions := manager.ListSessions(entities.SessionFilter{}); len(sessions) != 0 {
		t.Fatalf("initial sessions = %d, want 0", len(sessions))
	}
	assertListActionCount(t, client.Actions(), "services", 1)
	assertListActionCount(t, client.Actions(), "pods", 1)
	assertListActionCount(t, client.Actions(), "secrets", 1)

	labels := map[string]string{
		"app.kubernetes.io/name":       "agentapi-session",
		"app.kubernetes.io/managed-by": "agentapi-proxy",
		"agentapi.proxy/session-id":    "session-1",
		"agentapi.proxy/user-id":       "user-1",
		"agentapi.proxy/scope":         string(entities.ScopeUser),
	}
	if _, err := client.CoreV1().Services("test-ns").Create(context.Background(), &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:        "agentapi-session-session-1-svc",
			Namespace:   "test-ns",
			Labels:      labels,
			Annotations: map[string]string{"agentapi.proxy/initial-message": "hello"},
		},
		Spec: corev1.ServiceSpec{Ports: []corev1.ServicePort{{Port: 9000}}},
	}, metav1.CreateOptions{}); err != nil {
		t.Fatal(err)
	}
	if _, err := client.CoreV1().Pods("test-ns").Create(context.Background(), &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: "agentapi-session-session-1", Namespace: "test-ns", Labels: labels},
	}, metav1.CreateOptions{}); err != nil {
		t.Fatal(err)
	}

	deadline := time.Now().Add(2 * time.Second)
	for {
		sessions := manager.ListSessions(entities.SessionFilter{UserID: "user-1"})
		if len(sessions) == 1 && sessions[0].ID() == "session-1" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("informer did not observe created session")
		}
		time.Sleep(10 * time.Millisecond)
	}

	assertListActionCount(t, client.Actions(), "services", 1)
	assertListActionCount(t, client.Actions(), "pods", 1)
	assertListActionCount(t, client.Actions(), "secrets", 1)
}

func assertListActionCount(t *testing.T, actions []ktesting.Action, resource string, want int) {
	t.Helper()
	got := 0
	for _, action := range actions {
		if action.GetVerb() == "list" && action.GetResource().Resource == resource {
			got++
		}
	}
	if got != want {
		t.Fatalf("%s LIST calls = %d, want %d", resource, got, want)
	}
}
