package services

import (
	"context"
	"testing"
	"time"

	"github.com/takutakahashi/agentapi-proxy/pkg/config"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func newStockIdleSuspendTestManager(t *testing.T) (*KubernetesSessionManager, *corev1.Service) {
	t.Helper()
	ctx := context.Background()
	manager := newWorkloadTestManager(t, true)
	manager.k8sConfig.PodStartTimeout = 600
	manager.config.SessionPersistence = config.SessionPersistenceConfig{Backend: "volume", SuspendAfter: "5m"}
	if err := manager.CreateStockSessionForPool(ctx, "test-pool", false); err != nil {
		t.Fatalf("CreateStockSessionForPool: %v", err)
	}
	services, err := manager.client.CoreV1().Services("test-ns").List(ctx, metav1.ListOptions{LabelSelector: "agentapi.proxy/session-pool=test-pool"})
	if err != nil || len(services.Items) != 1 {
		t.Fatalf("stock services=%d err=%v", len(services.Items), err)
	}
	svc := services.Items[0]
	svc.Labels["agentapi.proxy/stock"] = "true"
	updated, err := manager.client.CoreV1().Services("test-ns").Update(ctx, &svc, metav1.UpdateOptions{})
	if err != nil {
		t.Fatal(err)
	}
	return manager, updated
}

func getTestService(t *testing.T, manager *KubernetesSessionManager, name string) *corev1.Service {
	t.Helper()
	svc, err := manager.client.CoreV1().Services("test-ns").Get(context.Background(), name, metav1.GetOptions{})
	if err != nil {
		t.Fatal(err)
	}
	return svc
}

func TestStockRunnerIsNotIdleSuspended(t *testing.T) {
	ctx := context.Background()
	manager, svc := newStockIdleSuspendTestManager(t)
	workload := "agentapi-session-" + svc.Labels["agentapi.proxy/session-id"]

	manager.reconcileSessionSuspends(ctx)

	current := getTestService(t, manager, svc.Name)
	if deadline := current.Annotations[sessionSuspendAtAnnotation]; deadline != "" {
		t.Fatalf("stock runner got a suspend deadline %q", deadline)
	}

	current.Annotations[sessionSuspendAtAnnotation] = time.Now().Add(-time.Minute).UTC().Format(time.RFC3339Nano)
	current.Annotations[sessionAutoSuspendEnabledAnnotation] = "true"
	current.Annotations[sessionAutoSuspendIdleSecondsAnnotation] = "300"
	if _, err := manager.client.CoreV1().Services("test-ns").Update(ctx, current, metav1.UpdateOptions{}); err != nil {
		t.Fatal(err)
	}

	manager.reconcileSessionSuspends(ctx)

	if _, err := manager.client.AppsV1().Deployments("test-ns").Get(ctx, workload, metav1.GetOptions{}); err != nil {
		t.Fatalf("stock deployment was removed by idle suspend: %v", err)
	}
	after := getTestService(t, manager, svc.Name)
	for _, key := range []string{sessionSuspendAtAnnotation, sessionSuspendedAtAnnotation, sessionAutoSuspendEnabledAnnotation, sessionAutoSuspendIdleSecondsAnnotation} {
		if value, ok := after.Annotations[key]; ok {
			t.Fatalf("stock runner kept %s=%q", key, value)
		}
	}
	count, err := manager.CountStockSessionsForPool(ctx, "test-pool", false)
	if err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("CountStockSessionsForPool = %d, want 1", count)
	}
}

func TestScheduleSessionSuspendSkipsStock(t *testing.T) {
	ctx := context.Background()
	manager, svc := newStockIdleSuspendTestManager(t)

	if err := manager.ScheduleSessionSuspend(ctx, svc.Labels["agentapi.proxy/session-id"]); err != nil {
		t.Fatalf("ScheduleSessionSuspend: %v", err)
	}

	after := getTestService(t, manager, svc.Name)
	for _, key := range []string{sessionSuspendAtAnnotation, sessionAutoSuspendEnabledAnnotation, sessionAutoSuspendIdleSecondsAnnotation} {
		if value, ok := after.Annotations[key]; ok {
			t.Fatalf("stock runner got %s=%q", key, value)
		}
	}
}

func TestClaimedRunnerGetsSuspendTimer(t *testing.T) {
	ctx := context.Background()
	manager, svc := newStockIdleSuspendTestManager(t)
	sessionID := svc.Labels["agentapi.proxy/session-id"]

	delete(svc.Labels, "agentapi.proxy/stock")
	if _, err := manager.client.CoreV1().Services("test-ns").Update(ctx, svc, metav1.UpdateOptions{}); err != nil {
		t.Fatal(err)
	}

	manager.reconcileSessionSuspends(ctx)

	claimed := getTestService(t, manager, svc.Name)
	if claimed.Annotations[sessionSuspendAtAnnotation] == "" || claimed.Annotations[sessionAutoSuspendIdleSecondsAnnotation] != "300" {
		t.Fatalf("claimed runner missing default suspend timer: %#v", claimed.Annotations)
	}

	enabled := true
	if err := manager.ApplyRunnerAutoSuspendPolicy(ctx, sessionID, &enabled, 10); err != nil {
		t.Fatalf("ApplyRunnerAutoSuspendPolicy: %v", err)
	}

	applied := getTestService(t, manager, svc.Name)
	if applied.Annotations[sessionSuspendAtAnnotation] == "" || applied.Annotations[sessionAutoSuspendIdleSecondsAnnotation] != "600" {
		t.Fatalf("claimed runner missing allocation suspend timer: %#v", applied.Annotations)
	}
}
