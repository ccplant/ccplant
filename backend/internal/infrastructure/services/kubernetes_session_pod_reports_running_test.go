package services

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"

	"github.com/takutakahashi/agentapi-proxy/internal/domain/entities"
	"github.com/takutakahashi/agentapi-proxy/pkg/config"
	"github.com/takutakahashi/agentapi-proxy/pkg/logger"
	"github.com/takutakahashi/agentapi-proxy/pkg/sessionsettings"
)

type podStatusFixture struct {
	manager      *KubernetesSessionManager
	client       *fake.Clientset
	session      *KubernetesSession
	podStatus    string
	podErr       error
	checkpoints  int
	onCheckpoint func()
}

func newPodStatusFixture(t *testing.T, rememberedStatus, podStatus string) *podStatusFixture {
	t.Helper()
	fixture := &podStatusFixture{podStatus: podStatus}
	original := http.DefaultClient.Transport
	http.DefaultClient.Transport = restartRoundTripper(func(*http.Request) (*http.Response, error) {
		fixture.checkpoints++
		if fixture.onCheckpoint != nil {
			fixture.onCheckpoint()
		}
		return &http.Response{StatusCode: http.StatusNoContent, Body: io.NopCloser(strings.NewReader(""))}, nil
	})
	t.Cleanup(func() { http.DefaultClient.Transport = original })

	pvcEnabled := true
	cfg := &config.Config{KubernetesSession: config.KubernetesSessionConfig{
		Namespace: "test-ns", Image: "test-image:latest", BasePort: 9000, ProvisionerToken: "provisioner",
		PVCEnabled: &pvcEnabled, CPURequest: "100m", CPULimit: "1",
		MemoryRequest: "128Mi", MemoryLimit: "512Mi",
	}}
	settings := &sessionsettings.SessionSettings{
		Session:       sessionsettings.SessionMeta{ID: "public-1", UserID: "user-1", AgentType: "claude-acp", PersistenceEnabled: true},
		ParentRuntime: &sessionsettings.ParentRuntimeConfig{Enabled: true, Endpoint: "http://parent.invalid", SessionID: "public-1", Token: "runtime", Generation: 1},
	}
	settingsData, err := sessionsettings.MarshalYAML(settings)
	if err != nil {
		t.Fatal(err)
	}
	client := fake.NewSimpleClientset(
		&corev1.Service{ObjectMeta: metav1.ObjectMeta{Name: "agentapi-session-runner-1-svc", Namespace: "test-ns", Labels: map[string]string{"agentapi.proxy/session-id": "runner-1"}}},
		&corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: "agentapi-session-runner-1-settings", Namespace: "test-ns"}, Data: map[string][]byte{"settings.yaml": settingsData}},
		&appsv1.Deployment{
			ObjectMeta: metav1.ObjectMeta{Name: "agentapi-session-runner-1", Namespace: "test-ns"},
			Status:     appsv1.DeploymentStatus{Replicas: 1, ReadyReplicas: 1},
		},
	)
	manager, err := NewKubernetesSessionManagerWithClient(cfg, false, logger.NewLogger(), client)
	if err != nil {
		t.Fatal(err)
	}
	if manager.suspendCancel != nil {
		manager.suspendCancel()
	}
	manager.config.SessionPersistence = config.SessionPersistenceConfig{Backend: "volume", SuspendAfter: "1h"}
	manager.restartHTTPClient = &http.Client{Transport: restartRoundTripper(func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader("{}"))}, nil
	})}
	manager.podStatusHTTPClient = &http.Client{Transport: restartRoundTripper(func(req *http.Request) (*http.Response, error) {
		if req.URL.Path != "/status" {
			t.Errorf("pod status request path=%q, want /status", req.URL.Path)
		}
		if fixture.podErr != nil {
			return nil, fixture.podErr
		}
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(`{"status":"` + fixture.podStatus + `"}`))}, nil
	})}
	session := NewKubernetesSession(
		"runner-1", &entities.RunServerRequest{UserID: "user-1", AgentType: "claude-acp"}, "agentapi-session-runner-1",
		"agentapi-session-runner-1-svc", "agentapi-session-runner-1-pvc", "test-ns", 9000, nil, nil,
	)
	session.SetProvisionSettings(settings)
	session.SetStatus(rememberedStatus)
	manager.sessions[session.id] = session
	fixture.manager, fixture.client, fixture.session = manager, client, session
	return fixture
}

func (f *podStatusFixture) service(t *testing.T) *corev1.Service {
	t.Helper()
	svc, err := f.client.CoreV1().Services("test-ns").Get(context.Background(), "agentapi-session-runner-1-svc", metav1.GetOptions{})
	if err != nil {
		t.Fatal(err)
	}
	return svc
}

func (f *podStatusFixture) setSuspendAt(t *testing.T, at time.Time) {
	t.Helper()
	svc := f.service(t)
	if svc.Annotations == nil {
		svc.Annotations = map[string]string{}
	}
	svc.Annotations[sessionSuspendAtAnnotation] = at.UTC().Format(time.RFC3339Nano)
	svc.Annotations[sessionAutoSuspendEnabledAnnotation] = "true"
	if _, err := f.client.CoreV1().Services("test-ns").Update(context.Background(), svc, metav1.UpdateOptions{}); err != nil {
		t.Fatal(err)
	}
}

func (f *podStatusFixture) suspendAt(t *testing.T) time.Time {
	t.Helper()
	raw := f.service(t).Annotations[sessionSuspendAtAnnotation]
	at, err := time.Parse(time.RFC3339Nano, raw)
	if err != nil {
		t.Fatalf("suspend-at=%q: %v", raw, err)
	}
	return at
}

func (f *podStatusFixture) deploymentExists(t *testing.T) bool {
	t.Helper()
	_, err := f.client.AppsV1().Deployments("test-ns").Get(context.Background(), "agentapi-session-runner-1", metav1.GetOptions{})
	return err == nil
}

func TestIdleSuspendKeepsSessionThePodReportsRunning(t *testing.T) {
	for _, remembered := range []string{"active", "stopped"} {
		t.Run(remembered, func(t *testing.T) {
			fixture := newPodStatusFixture(t, remembered, "running")
			fixture.setSuspendAt(t, time.Now().Add(-time.Minute))

			fixture.manager.reconcileSessionSuspends(context.Background())

			if !fixture.deploymentExists(t) {
				t.Fatal("a replica that did not remember the running turn suspended the busy session")
			}
			if fixture.checkpoints != 0 {
				t.Fatalf("checkpoints=%d, want 0", fixture.checkpoints)
			}
			if at := fixture.suspendAt(t); !at.After(time.Now().Add(30 * time.Minute)) {
				t.Fatalf("suspend-at=%s, want postponed by the idle timeout", at)
			}
		})
	}
}

func TestIdleSuspendTrustsPodReportingStableOverRememberedRunning(t *testing.T) {
	fixture := newPodStatusFixture(t, "running", "stable")
	fixture.setSuspendAt(t, time.Now().Add(-time.Minute))

	fixture.manager.reconcileSessionSuspends(context.Background())

	if fixture.deploymentExists(t) {
		t.Fatal("idle session was kept because of a stale running status")
	}
	if got := fixture.service(t).Annotations[sessionSuspendedAtAnnotation]; got == "" {
		t.Fatal("suspended-at annotation was not recorded")
	}
}

func TestIdleSuspendWaitsBrieflyWhenPodDoesNotAnswer(t *testing.T) {
	fixture := newPodStatusFixture(t, "active", "")
	fixture.podErr = errors.New("connection refused")
	deadline := time.Now().Add(-time.Minute)
	fixture.setSuspendAt(t, deadline)

	fixture.manager.reconcileSessionSuspends(context.Background())

	if !fixture.deploymentExists(t) {
		t.Fatal("session was suspended without confirming the pod status inside the grace period")
	}
	if at := fixture.suspendAt(t); at.Sub(deadline).Abs() > time.Second {
		t.Fatalf("suspend-at=%s, want unchanged %s", at, deadline)
	}

	fixture.setSuspendAt(t, time.Now().Add(-podRuntimeStatusUnknownGrace-time.Minute))
	fixture.manager.reconcileSessionSuspends(context.Background())

	if fixture.deploymentExists(t) {
		t.Fatal("idle session with a silent pod was never suspended")
	}
}

func TestIdleSuspendCancelledWhenPodStartsRunningDuringCheckpoint(t *testing.T) {
	fixture := newPodStatusFixture(t, "active", "stable")
	fixture.onCheckpoint = func() { fixture.podStatus = "running" }
	fixture.setSuspendAt(t, time.Now().Add(-time.Minute))

	fixture.manager.reconcileSessionSuspends(context.Background())

	if fixture.checkpoints != 1 {
		t.Fatalf("checkpoints=%d, want 1", fixture.checkpoints)
	}
	if !fixture.deploymentExists(t) {
		t.Fatal("suspend removed the workload of a session that started running during checkpoint")
	}
	if owner := fixture.service(t).Annotations[sessionSuspendOwnerAnnotation]; owner != "" {
		t.Fatalf("suspend ownership was not released: %q", owner)
	}
}

func TestExplicitSuspendRefusedWhenPodReportsRunning(t *testing.T) {
	fixture := newPodStatusFixture(t, "active", "running")

	err := fixture.manager.SuspendSession(context.Background(), "runner-1")

	if err == nil || !strings.Contains(err.Error(), "session is busy") {
		t.Fatalf("err=%v, want session is busy", err)
	}
	if !fixture.deploymentExists(t) {
		t.Fatal("explicit suspend removed the workload of a running session")
	}
}

func TestPodStatusClientDoesNotFollowRedirects(t *testing.T) {
	if err := defaultPodStatusHTTPClient.CheckRedirect(nil, nil); !errors.Is(err, http.ErrUseLastResponse) {
		t.Fatalf("CheckRedirect err=%v, want http.ErrUseLastResponse", err)
	}

	fixture := newPodStatusFixture(t, "active", "")
	fixture.manager.podStatusHTTPClient = &http.Client{Transport: restartRoundTripper(func(*http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusFound,
			Header:     http.Header{"Location": []string{"http://internal.invalid/"}},
			Body:       io.NopCloser(strings.NewReader("")),
		}, nil
	}), CheckRedirect: defaultPodStatusHTTPClient.CheckRedirect}

	running, confirmed := fixture.manager.podReportsRunning(context.Background(), fixture.session)

	if running || confirmed {
		t.Fatalf("running=%v confirmed=%v, want an unconfirmed answer for a redirect", running, confirmed)
	}
}

func TestPodReportsRunningFallsBackToMemoryWithoutReadyWorkload(t *testing.T) {
	fixture := newPodStatusFixture(t, "running", "stable")
	deployment, err := fixture.client.AppsV1().Deployments("test-ns").Get(context.Background(), "agentapi-session-runner-1", metav1.GetOptions{})
	if err != nil {
		t.Fatal(err)
	}
	deployment.Status = appsv1.DeploymentStatus{}
	if _, err := fixture.client.AppsV1().Deployments("test-ns").Update(context.Background(), deployment, metav1.UpdateOptions{}); err != nil {
		t.Fatal(err)
	}

	running, confirmed := fixture.manager.podReportsRunning(context.Background(), fixture.session)

	if !running || !confirmed {
		t.Fatalf("running=%v confirmed=%v, want the remembered status without asking the pod", running, confirmed)
	}
}
