package services

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
	"sigs.k8s.io/yaml"

	"github.com/takutakahashi/agentapi-proxy/internal/domain/entities"
	"github.com/takutakahashi/agentapi-proxy/pkg/config"
	"github.com/takutakahashi/agentapi-proxy/pkg/logger"
	"github.com/takutakahashi/agentapi-proxy/pkg/sessionsettings"
)

type snapshotMarkerFixture struct {
	manager *KubernetesSessionManager
	client  *fake.Clientset
	session *KubernetesSession
}

func newSnapshotMarkerFixture(t *testing.T, checkpointStatus int) *snapshotMarkerFixture {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(checkpointStatus)
	}))
	t.Cleanup(server.Close)
	pvcEnabled := true
	cfg := &config.Config{KubernetesSession: config.KubernetesSessionConfig{
		Namespace: "test-ns", Image: "test-image:latest", BasePort: 9000, ProvisionerToken: "provisioner",
		PVCEnabled: &pvcEnabled, CPURequest: "100m", CPULimit: "1",
		MemoryRequest: "128Mi", MemoryLimit: "512Mi",
	}, SessionPersistence: config.SessionPersistenceConfig{Backend: "volume", SuspendAfter: "1h"}}
	settings := &sessionsettings.SessionSettings{
		Session:        sessionsettings.SessionMeta{ID: "public-1", UserID: "user-1", AgentType: "claude-acp", PersistenceEnabled: true},
		InitialMessage: "original",
		ParentRuntime:  &sessionsettings.ParentRuntimeConfig{Enabled: true, Endpoint: server.URL, SessionID: "public-1", Token: "runtime", Generation: 1},
	}
	settingsData, err := sessionsettings.MarshalYAML(settings)
	if err != nil {
		t.Fatal(err)
	}
	client := fake.NewSimpleClientset(
		&corev1.Service{ObjectMeta: metav1.ObjectMeta{Name: "agentapi-session-runner-1-svc", Namespace: "test-ns"}},
		&corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: "agentapi-session-runner-1-settings", Namespace: "test-ns"}, Data: map[string][]byte{"settings.yaml": settingsData}},
		&appsv1.Deployment{ObjectMeta: metav1.ObjectMeta{Name: "agentapi-session-runner-1", Namespace: "test-ns"}},
	)
	manager, err := NewKubernetesSessionManagerWithClient(cfg, false, logger.NewLogger(), client)
	if err != nil {
		t.Fatal(err)
	}
	if manager.suspendCancel != nil {
		manager.suspendCancel()
	}
	manager.restartHTTPClient = &http.Client{Transport: restartRoundTripper(func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader("{}"))}, nil
	})}
	session := NewKubernetesSession(
		"runner-1", &entities.RunServerRequest{UserID: "user-1", AgentType: "claude-acp"}, "agentapi-session-runner-1",
		"agentapi-session-runner-1-svc", "agentapi-session-runner-1-pvc", "test-ns", 9000, nil, nil,
	)
	session.SetProvisionSettings(settings)
	session.SetStatus("active")
	manager.sessions[session.id] = session
	return &snapshotMarkerFixture{manager: manager, client: client, session: session}
}

func (f *snapshotMarkerFixture) stored(t *testing.T) sessionsettings.SessionSettings {
	t.Helper()
	secret, err := f.client.CoreV1().Secrets("test-ns").Get(context.Background(), "agentapi-session-runner-1-settings", metav1.GetOptions{})
	if err != nil {
		t.Fatal(err)
	}
	var settings sessionsettings.SessionSettings
	if err := yaml.Unmarshal(secret.Data["settings.yaml"], &settings); err != nil {
		t.Fatal(err)
	}
	return settings
}

func (f *snapshotMarkerFixture) deploymentExists(t *testing.T) bool {
	t.Helper()
	_, err := f.client.AppsV1().Deployments("test-ns").Get(context.Background(), "agentapi-session-runner-1", metav1.GetOptions{})
	return err == nil
}

func TestIdleSuspendRecordsOwnResumeSourceAfterCheckpoint(t *testing.T) {
	fixture := newSnapshotMarkerFixture(t, http.StatusNoContent)

	if err := fixture.manager.SuspendSession(context.Background(), "runner-1"); err != nil {
		t.Fatal(err)
	}
	if got := fixture.stored(t).Session.ResumeFrom; got != "public-1" {
		t.Fatalf("resume_from=%q, want own snapshot public-1", got)
	}
	if fixture.deploymentExists(t) {
		t.Fatal("suspend kept the workload")
	}
}

func TestIdleSuspendWritesNothingWhenCheckpointFails(t *testing.T) {
	fixture := newSnapshotMarkerFixture(t, http.StatusInternalServerError)

	if err := fixture.manager.SuspendSession(context.Background(), "runner-1"); err == nil {
		t.Fatal("suspend succeeded without a checkpoint")
	}
	if got := fixture.stored(t).Session.ResumeFrom; got != "" {
		t.Fatalf("resume_from=%q, want unchanged", got)
	}
	if !fixture.deploymentExists(t) {
		t.Fatal("failed suspend removed the workload")
	}
}

func TestTerminationSuspendRecordsOwnResumeSource(t *testing.T) {
	fixture := newSnapshotMarkerFixture(t, http.StatusNoContent)

	if err := fixture.manager.SuspendSessionOnTermination(context.Background(), "runner-1"); err != nil {
		t.Fatal(err)
	}
	if got := fixture.stored(t).Session.ResumeFrom; got != "public-1" {
		t.Fatalf("resume_from=%q, want own snapshot public-1", got)
	}
}

func TestPauseWithParentSettingsRecordsOwnResumeSourceAfterCheckpoint(t *testing.T) {
	fixture := newSnapshotMarkerFixture(t, http.StatusNoContent)
	current := fixture.session.ProvisionSettings()
	next := *current
	next.InitialMessage = "replacement"

	if err := fixture.manager.PauseSessionWithSettings(context.Background(), "runner-1", &next); err != nil {
		t.Fatal(err)
	}
	stored := fixture.stored(t)
	if stored.Session.ResumeFrom != "public-1" || !stored.Paused || stored.InitialMessage != "replacement" {
		t.Fatalf("paused settings resume_from=%q paused=%v initial_message=%q", stored.Session.ResumeFrom, stored.Paused, stored.InitialMessage)
	}
}

func TestPauseWithParentSettingsWritesNothingWhenCheckpointFails(t *testing.T) {
	fixture := newSnapshotMarkerFixture(t, http.StatusInternalServerError)
	next := *fixture.session.ProvisionSettings()
	next.InitialMessage = "replacement"

	if err := fixture.manager.PauseSessionWithSettings(context.Background(), "runner-1", &next); err == nil {
		t.Fatal("pause succeeded without a checkpoint")
	}
	stored := fixture.stored(t)
	if stored.InitialMessage != "original" || stored.Session.ResumeFrom != "" {
		t.Fatalf("failed pause rewrote settings: initial_message=%q resume_from=%q", stored.InitialMessage, stored.Session.ResumeFrom)
	}
}
