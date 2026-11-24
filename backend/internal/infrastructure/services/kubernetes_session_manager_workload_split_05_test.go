package services

import (
	"context"
	stderrors "errors"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/takutakahashi/agentapi-proxy/internal/domain/entities"
)

func TestPurgeStockSessionsForPoolOnlyDeletesRequestedPool(t *testing.T) {
	manager := newWorkloadTestManager(t, false)
	ctx := context.Background()
	for _, pool := range []string{"disabled", "enabled"} {
		id := pool + "-stock"
		labels := map[string]string{
			"app.kubernetes.io/name":         "agentapi-session",
			"app.kubernetes.io/managed-by":   "agentapi-proxy",
			"agentapi.proxy/session-id":      id,
			"agentapi.proxy/session-pool":    pool,
			"agentapi.proxy/stock":           "true",
			"agentapi.proxy/capability-dind": "false",
		}
		if _, err := manager.client.CoreV1().Services("test-ns").Create(ctx, &corev1.Service{
			ObjectMeta: metav1.ObjectMeta{Name: "agentapi-session-" + id + "-svc", Namespace: "test-ns", Labels: labels},
		}, metav1.CreateOptions{}); err != nil {
			t.Fatal(err)
		}
	}

	if err := manager.PurgeStockSessionsForPool(ctx, "disabled"); err != nil {
		t.Fatal(err)
	}
	if _, err := manager.client.CoreV1().Services("test-ns").Get(ctx, "agentapi-session-disabled-stock-svc", metav1.GetOptions{}); !errors.IsNotFound(err) {
		t.Fatalf("disabled pool stock still exists: %v", err)
	}
	if _, err := manager.client.CoreV1().Services("test-ns").Get(ctx, "agentapi-session-enabled-stock-svc", metav1.GetOptions{}); err != nil {
		t.Fatalf("enabled pool stock was deleted: %v", err)
	}
}

func TestCreateStockSessionReturnsBeforeWorkloadIsReady(t *testing.T) {
	manager := newWorkloadTestManager(t, false)
	manager.k8sConfig.PodStartTimeout = 1

	started := time.Now()
	if err := manager.CreateStockSession(context.Background(), false); err != nil {
		t.Fatalf("CreateStockSession: %v", err)
	}
	if elapsed := time.Since(started); elapsed > 500*time.Millisecond {
		t.Fatalf("CreateStockSession waited %s for workload readiness", elapsed)
	}

	services, err := manager.client.CoreV1().Services("test-ns").List(context.Background(), metav1.ListOptions{
		LabelSelector: "agentapi.proxy/stock=creating",
	})
	if err != nil {
		t.Fatalf("list creating stock services: %v", err)
	}
	if len(services.Items) != 1 {
		t.Fatalf("creating stock services = %d, want 1", len(services.Items))
	}
}

func TestWaitForStockSessionWorkloadReadyHasNoStartupTimeout(t *testing.T) {
	manager := newWorkloadTestManager(t, false)
	manager.k8sConfig.PodStartTimeout = 0
	session := NewKubernetesSession(
		"slow-stock",
		&entities.RunServerRequest{},
		"agentapi-session-slow-stock",
		"agentapi-session-slow-stock-svc",
		"agentapi-session-slow-stock-pvc",
		"test-ns",
		9000,
		func() {},
		nil,
	)

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		done <- manager.waitForSessionWorkloadReady(ctx, session)
	}()

	select {
	case err := <-done:
		t.Fatalf("readiness wait returned before cancellation: %v", err)
	case <-time.After(50 * time.Millisecond):
	}
	cancel()
	if err := <-done; !stderrors.Is(err, context.Canceled) {
		t.Fatalf("readiness wait error = %v, want context cancellation", err)
	}
}

func TestCleanupStockSessionResourcesUsesIndependentContext(t *testing.T) {
	manager := newWorkloadTestManager(t, false)

	session := NewKubernetesSession(
		"cleanup-stock",
		&entities.RunServerRequest{},
		"agentapi-session-cleanup-stock",
		"agentapi-session-cleanup-stock-svc",
		"agentapi-session-cleanup-stock-pvc",
		"test-ns",
		9000,
		func() {},
		nil,
	)
	_, err := manager.client.CoreV1().Services("test-ns").Create(context.Background(), &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{Name: session.ServiceName(), Namespace: "test-ns"},
	}, metav1.CreateOptions{})
	if err != nil {
		t.Fatalf("create service: %v", err)
	}

	// This helper intentionally accepts no caller context, so a timeout in the
	// stock creation request cannot cancel these deletion calls.
	if err := manager.cleanupStockSessionResources(session); err != nil {
		t.Fatalf("cleanupStockSessionResources failed: %v", err)
	}
	if _, err := manager.client.CoreV1().Services("test-ns").Get(context.Background(), session.ServiceName(), metav1.GetOptions{}); !errors.IsNotFound(err) {
		t.Fatalf("expected stock service to be deleted, got %v", err)
	}
}

func TestStockPodTemplateHashTracksEffectiveSessionTemplate(t *testing.T) {
	manager := newWorkloadTestManager(t, false)
	ctx := context.Background()

	baseHash, err := manager.stockPodTemplateHash(ctx, false)
	if err != nil {
		t.Fatalf("Failed to calculate base stock template hash: %v", err)
	}
	repeatedHash, err := manager.stockPodTemplateHash(ctx, false)
	if err != nil {
		t.Fatalf("Failed to recalculate stock template hash: %v", err)
	}
	if repeatedHash != baseHash {
		t.Fatalf("Stock template hash is not stable: %q != %q", repeatedHash, baseHash)
	}

	dindHash, err := manager.stockPodTemplateHash(ctx, true)
	if err != nil {
		t.Fatalf("Failed to calculate DinD stock template hash: %v", err)
	}
	if dindHash == baseHash {
		t.Fatalf("DinD and non-DinD stock templates have the same hash %q", baseHash)
	}

	manager.k8sConfig.Image = "changed-session-image:latest"
	changedHash, err := manager.stockPodTemplateHash(ctx, false)
	if err != nil {
		t.Fatalf("Failed to calculate changed stock template hash: %v", err)
	}
	if changedHash == baseHash {
		t.Fatalf("Session image change did not alter stock template hash %q", baseHash)
	}
}
