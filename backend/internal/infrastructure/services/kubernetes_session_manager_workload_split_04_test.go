package services

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/takutakahashi/agentapi-proxy/internal/domain/entities"
	"github.com/takutakahashi/agentapi-proxy/pkg/sessionsettings"
)

func TestApplyRunnerAllocationMetadataPersistsAgentType(t *testing.T) {
	manager := newWorkloadTestManager(t, false)
	ctx := context.Background()
	const sessionID = "allocated-runner"
	session := NewKubernetesSession(sessionID, &entities.RunServerRequest{}, "agentapi-session-"+sessionID, "agentapi-session-"+sessionID+"-svc", "", "test-ns", 9000, nil, nil)
	manager.mutex.Lock()
	manager.sessions[sessionID] = session
	manager.mutex.Unlock()
	_, err := manager.client.CoreV1().Services("test-ns").Create(ctx, &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{Name: session.ServiceName(), Namespace: "test-ns"},
	}, metav1.CreateOptions{})
	if err != nil {
		t.Fatal(err)
	}

	if err := manager.ApplyRunnerAllocationMetadata(ctx, sessionID, sessionsettings.SessionMeta{AgentType: "claude-acp"}); err != nil {
		t.Fatal(err)
	}
	if session.Request().AgentType != "claude-acp" {
		t.Fatalf("agent type = %q", session.Request().AgentType)
	}
	svc, err := manager.client.CoreV1().Services("test-ns").Get(ctx, session.ServiceName(), metav1.GetOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if svc.Annotations["agentapi.proxy/agent-type"] != "claude-acp" {
		t.Fatalf("agent type annotation = %q", svc.Annotations["agentapi.proxy/agent-type"])
	}
}

func TestRefreshRunnerSessionStatusUsesCanonicalSuspendState(t *testing.T) {
	manager := newWorkloadTestManager(t, false)
	ctx := context.Background()
	const sessionID = "suspended-runner"
	session := NewKubernetesSession(sessionID, &entities.RunServerRequest{}, "agentapi-session-"+sessionID, "agentapi-session-"+sessionID+"-svc", "", "test-ns", 9000, nil, nil)
	session.SetStatus("active")
	manager.mutex.Lock()
	manager.sessions[sessionID] = session
	manager.mutex.Unlock()
	_, err := manager.client.CoreV1().Services("test-ns").Create(ctx, &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{Name: session.ServiceName(), Namespace: "test-ns", Annotations: map[string]string{sessionSuspendedAtAnnotation: time.Now().Format(time.RFC3339Nano)}},
	}, metav1.CreateOptions{})
	if err != nil {
		t.Fatal(err)
	}

	if err := manager.RefreshRunnerSessionStatus(ctx, sessionID); err != nil {
		t.Fatal(err)
	}
	if session.Status() != "suspended" {
		t.Fatalf("status = %q, want suspended", session.Status())
	}
}

func TestRefreshRunnerSessionStatusUsesCanonicalProvisionError(t *testing.T) {
	manager := newWorkloadTestManager(t, false)
	ctx := context.Background()
	const sessionID = "failed-provision-runner"
	session := NewKubernetesSession(sessionID, &entities.RunServerRequest{}, "agentapi-session-"+sessionID, "agentapi-session-"+sessionID+"-svc", "", "test-ns", 9000, nil, nil)
	session.SetStatus("starting")
	manager.mutex.Lock()
	manager.sessions[sessionID] = session
	manager.mutex.Unlock()
	_, err := manager.client.CoreV1().Services("test-ns").Create(ctx, &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{Name: session.ServiceName(), Namespace: "test-ns"},
	}, metav1.CreateOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if err := manager.saveProvisionRequest(ctx, &ProvisionRequest{
		RequestID: "request-1", SessionID: sessionID, Status: "error",
		Message: "setup failed: repository not found", ErrorCode: "setup_failed", UpdatedAt: time.Now(),
	}); err != nil {
		t.Fatal(err)
	}

	if err := manager.RefreshRunnerSessionStatus(ctx, sessionID); err != nil {
		t.Fatal(err)
	}
	if session.Status() != "error" {
		t.Fatalf("status = %q, want error", session.Status())
	}
	if session.StatusMessage() != "setup failed: repository not found" {
		t.Fatalf("status message = %q", session.StatusMessage())
	}
	if session.ErrorCode() != "setup_failed" {
		t.Fatalf("error code = %q", session.ErrorCode())
	}
}

func TestRefreshRunnerSessionStatusCorrectsStoppedObservedFromOtherReplicaSuspend(t *testing.T) {
	manager := newWorkloadTestManager(t, false)
	ctx := context.Background()
	const sessionID = "suspended-by-other-replica"
	session := NewKubernetesSession(sessionID, &entities.RunServerRequest{}, "agentapi-session-"+sessionID, "agentapi-session-"+sessionID+"-svc", "", "test-ns", 9000, nil, nil)
	session.SetStatus("stopped")
	manager.mutex.Lock()
	manager.sessions[sessionID] = session
	manager.mutex.Unlock()
	_, err := manager.client.CoreV1().Services("test-ns").Create(ctx, &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{Name: session.ServiceName(), Namespace: "test-ns", Annotations: map[string]string{sessionSuspendedAtAnnotation: time.Now().Format(time.RFC3339Nano)}},
	}, metav1.CreateOptions{})
	if err != nil {
		t.Fatal(err)
	}

	if err := manager.RefreshRunnerSessionStatus(ctx, sessionID); err != nil {
		t.Fatal(err)
	}
	if session.Status() != "suspended" {
		t.Fatalf("status = %q, want suspended", session.Status())
	}
}

func TestRefreshRunnerSessionStatusDetectsNewReadyPod(t *testing.T) {
	manager := newWorkloadTestManager(t, false)
	ctx := context.Background()
	const sessionID = "resumed-runner"
	session := NewKubernetesSession(sessionID, &entities.RunServerRequest{}, "agentapi-session-"+sessionID, "agentapi-session-"+sessionID+"-svc", "", "test-ns", 9000, nil, nil)
	session.SetStatus("stopped")
	session.SetUpdatedAt(time.Now().Add(-time.Minute))
	manager.mutex.Lock()
	manager.sessions[sessionID] = session
	manager.mutex.Unlock()
	_, err := manager.client.CoreV1().Services("test-ns").Create(ctx, &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{Name: session.ServiceName(), Namespace: "test-ns"},
	}, metav1.CreateOptions{})
	if err != nil {
		t.Fatal(err)
	}
	pod, err := manager.client.CoreV1().Pods("test-ns").Create(ctx, &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: session.DeploymentName(), Namespace: "test-ns", Labels: map[string]string{"agentapi.proxy/session-id": sessionID}, CreationTimestamp: metav1.NewTime(time.Now().Add(-2 * time.Minute))},
		Status:     corev1.PodStatus{Phase: corev1.PodRunning, Conditions: []corev1.PodCondition{{Type: corev1.PodReady, Status: corev1.ConditionTrue}}},
	}, metav1.CreateOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if err := manager.RefreshRunnerSessionStatus(ctx, sessionID); err != nil {
		t.Fatal(err)
	}
	if session.Status() != "stopped" {
		t.Fatalf("status with old ready pod = %q, want stopped", session.Status())
	}
	pod.CreationTimestamp = metav1.NewTime(time.Now())
	if _, err := manager.client.CoreV1().Pods("test-ns").Update(ctx, pod, metav1.UpdateOptions{}); err != nil {
		t.Fatal(err)
	}

	if err := manager.RefreshRunnerSessionStatus(ctx, sessionID); err != nil {
		t.Fatal(err)
	}
	if session.Status() != "starting" {
		t.Fatalf("status = %q, want starting", session.Status())
	}
	session.SetStatus("suspended")
}

func TestPurgeStaleStockSessionsKeepsCreatingStockRegardlessOfAge(t *testing.T) {
	manager := newWorkloadTestManager(t, false)
	manager.k8sConfig.PodStartTimeout = 120
	ctx := context.Background()

	for _, tc := range []struct {
		id      string
		created time.Time
	}{
		{id: "expired-creating", created: time.Now().Add(-10 * time.Minute)},
		{id: "current-creating", created: time.Now()},
	} {
		labels := map[string]string{
			"app.kubernetes.io/managed-by": "agentapi-proxy",
			"agentapi.proxy/stock":         "creating",
			"agentapi.proxy/session-id":    tc.id,
		}
		name := "agentapi-session-" + tc.id
		_, err := manager.client.CoreV1().Services("test-ns").Create(ctx, &corev1.Service{
			ObjectMeta: metav1.ObjectMeta{Name: name + "-svc", Namespace: "test-ns", Labels: labels, CreationTimestamp: metav1.NewTime(tc.created)},
		}, metav1.CreateOptions{})
		if err != nil {
			t.Fatalf("create service %s: %v", tc.id, err)
		}
	}

	if err := manager.PurgeStaleStockSessions(ctx); err != nil {
		t.Fatalf("PurgeStaleStockSessions failed: %v", err)
	}
	for _, id := range []string{"expired-creating", "current-creating"} {
		if _, err := manager.client.CoreV1().Services("test-ns").Get(ctx, "agentapi-session-"+id+"-svc", metav1.GetOptions{}); err != nil {
			t.Fatalf("expected %s stock to remain, got %v", id, err)
		}
	}
}

func TestCountStockSessionsIncludesCreatingReservations(t *testing.T) {
	manager := newWorkloadTestManager(t, false)
	ctx := context.Background()

	for _, tc := range []struct {
		id    string
		state string
		dind  string
	}{
		{id: "ready-plain", state: "true", dind: "false"},
		{id: "creating-plain", state: "creating", dind: "false"},
		{id: "claiming-plain", state: "claiming", dind: "false"},
		{id: "creating-dind", state: "creating", dind: "true"},
	} {
		labels := map[string]string{
			"app.kubernetes.io/managed-by":      "agentapi-proxy",
			"agentapi.proxy/stock":              tc.state,
			"agentapi.proxy/capability-sandbox": "true",
			"agentapi.proxy/capability-dind":    tc.dind,
		}
		_, err := manager.client.CoreV1().Services("test-ns").Create(ctx, &corev1.Service{
			ObjectMeta: metav1.ObjectMeta{Name: "agentapi-session-" + tc.id + "-svc", Namespace: "test-ns", Labels: labels},
		}, metav1.CreateOptions{})
		if err != nil {
			t.Fatalf("create service %s: %v", tc.id, err)
		}
	}

	plain, err := manager.CountStockSessions(ctx, false)
	if err != nil {
		t.Fatalf("CountStockSessions(false): %v", err)
	}
	if plain != 2 {
		t.Fatalf("CountStockSessions(false) = %d, want 2", plain)
	}
	dind, err := manager.CountStockSessions(ctx, true)
	if err != nil {
		t.Fatalf("CountStockSessions(true): %v", err)
	}
	if dind != 1 {
		t.Fatalf("CountStockSessions(true) = %d, want 1", dind)
	}
}

func TestCountStockSessionsExcludesAllocatedDirectRunners(t *testing.T) {
	manager := newWorkloadTestManager(t, false)
	parent := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/internal/session-managers/manager-a/heartbeat" {
			t.Errorf("heartbeat path = %q", r.URL.Path)
		}
		_, _ = w.Write([]byte(`{"allocated_runner_ids":["allocated-stock"]}`))
	}))
	t.Cleanup(parent.Close)
	manager.ConfigureSessionRunnerPool(parent.URL, "manager-a", "manager-token", "test-pool")

	for _, id := range []string{"allocated-stock", "idle-stock"} {
		_, err := manager.client.CoreV1().Services("test-ns").Create(context.Background(), &corev1.Service{
			ObjectMeta: metav1.ObjectMeta{Name: "agentapi-session-" + id + "-svc", Namespace: "test-ns", Labels: map[string]string{
				"app.kubernetes.io/managed-by":      "agentapi-proxy",
				"agentapi.proxy/stock":              "true",
				"agentapi.proxy/session-id":         id,
				"agentapi.proxy/session-pool":       "test-pool",
				"agentapi.proxy/capability-sandbox": "true",
				"agentapi.proxy/capability-dind":    "false",
			}}}, metav1.CreateOptions{})
		if err != nil {
			t.Fatalf("create service %s: %v", id, err)
		}
	}

	count, err := manager.CountStockSessionsForPool(context.Background(), "test-pool", false)
	if err != nil {
		t.Fatalf("CountStockSessionsForPool: %v", err)
	}
	if count != 1 {
		t.Fatalf("CountStockSessionsForPool = %d, want 1", count)
	}
}

func TestLegacyStockSelectionAndCountExcludePoolRunners(t *testing.T) {
	manager := newWorkloadTestManager(t, false)
	ctx := context.Background()
	baseLabels := map[string]string{
		"app.kubernetes.io/managed-by":      "agentapi-proxy",
		"agentapi.proxy/stock":              "true",
		"agentapi.proxy/capability-sandbox": "true",
		"agentapi.proxy/capability-dind":    "false",
	}
	for _, tc := range []struct {
		id   string
		pool string
	}{{id: "pool-stock", pool: "managed"}, {id: "legacy-stock"}} {
		labels := make(map[string]string, len(baseLabels)+2)
		for key, value := range baseLabels {
			labels[key] = value
		}
		labels["agentapi.proxy/session-id"] = tc.id
		if tc.pool != "" {
			labels["agentapi.proxy/session-pool"] = tc.pool
		}
		if _, err := manager.client.CoreV1().Services("test-ns").Create(ctx, &corev1.Service{
			ObjectMeta: metav1.ObjectMeta{Name: "agentapi-session-" + tc.id + "-svc", Namespace: "test-ns", Labels: labels},
		}, metav1.CreateOptions{}); err != nil {
			t.Fatal(err)
		}
	}

	count, err := manager.CountStockSessions(ctx, false)
	if err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("legacy stock count = %d, want 1", count)
	}
	stock, err := manager.findStockSession(ctx, sessionRequirements(&entities.RunServerRequest{}))
	if err != nil {
		t.Fatal(err)
	}
	if stock == nil || stock.Labels["agentapi.proxy/session-id"] != "legacy-stock" {
		t.Fatalf("selected stock = %#v, want legacy-stock", stock)
	}
}

func TestBuildEnvVarsDeduplicatesControlPlaneURL(t *testing.T) {
	manager := newWorkloadTestManager(t, false)
	session := newWorkloadTestSession()
	session.Request().Environment = map[string]string{"PROVISIONER_PROXY_URL": "https://parent.example.com"}

	env := manager.buildEnvVars(session, session.Request())
	count := 0
	for _, item := range env {
		if item.Name == "PROVISIONER_PROXY_URL" {
			count++
			if item.Value != "https://parent.example.com" {
				t.Fatalf("PROVISIONER_PROXY_URL = %q", item.Value)
			}
		}
	}
	if count != 1 {
		t.Fatalf("PROVISIONER_PROXY_URL count = %d, want 1", count)
	}
	for _, item := range env {
		if item.Name == "PROVISIONER_LOCAL_PROXY_URL" && item.Value == "https://parent.example.com" {
			t.Fatal("local provisioner URL was overridden by the pool parent URL")
		}
	}
}
