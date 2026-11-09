package sessionmanager

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/labstack/echo/v4"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	k8sruntime "k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes/fake"
	"sigs.k8s.io/yaml"

	"github.com/takutakahashi/agentapi-proxy/internal/infrastructure/services"
	"github.com/takutakahashi/agentapi-proxy/pkg/config"
	"github.com/takutakahashi/agentapi-proxy/pkg/hmacutil"
	"github.com/takutakahashi/agentapi-proxy/pkg/logger"
	"github.com/takutakahashi/agentapi-proxy/pkg/sessionsettings"
)

const (
	resumeTestNamespace = "test-ns"
	resumeTestRunnerID  = "runner-1"
	resumeTestPublicID  = "public-1"
	resumeTestSecret    = "test-secret"
)

type resumePathOptions struct {
	resumeFrom     string
	initialMessage string
	running        bool
	parentRuntime  *sessionsettings.ParentRuntimeConfig
}

type resumePathFixture struct {
	client *fake.Clientset
	echo   *echo.Echo
}

func newResumePathFixture(t *testing.T, options resumePathOptions) *resumePathFixture {
	t.Helper()
	pvcEnabled := true
	cfg := &config.Config{KubernetesSession: config.KubernetesSessionConfig{
		Namespace: resumeTestNamespace, Image: "test-image:latest", BasePort: 9000,
		PVCEnabled: &pvcEnabled, CPURequest: "100m", CPULimit: "1",
		MemoryRequest: "128Mi", MemoryLimit: "512Mi",
	}, SessionPersistence: config.SessionPersistenceConfig{Backend: "volume", SuspendAfter: "1h"}}
	settingsData, err := sessionsettings.MarshalYAML(&sessionsettings.SessionSettings{
		Session: sessionsettings.SessionMeta{
			ID: resumeTestPublicID, UserID: "user-1", AgentType: "claude-acp", PersistenceEnabled: true, ResumeFrom: options.resumeFrom,
		},
		InitialMessage: options.initialMessage,
		ParentRuntime:  options.parentRuntime,
	})
	if err != nil {
		t.Fatal(err)
	}
	annotations := map[string]string{"agentapi.proxy/agent-type": "claude-acp"}
	if !options.running {
		annotations["agentapi.proxy/suspended-at"] = time.Now().UTC().Format(time.RFC3339Nano)
	}
	objects := []k8sruntime.Object{
		&corev1.Service{ObjectMeta: metav1.ObjectMeta{
			Name: "agentapi-session-" + resumeTestRunnerID + "-svc", Namespace: resumeTestNamespace,
			Labels:      map[string]string{"agentapi.proxy/session-id": resumeTestRunnerID, "agentapi.proxy/user-id": "user-1"},
			Annotations: annotations,
		}, Spec: corev1.ServiceSpec{Ports: []corev1.ServicePort{{Port: 9000}}}},
		&corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: "agentapi-session-" + resumeTestRunnerID + "-settings", Namespace: resumeTestNamespace}, Data: map[string][]byte{"settings.yaml": settingsData}},
	}
	if options.running {
		objects = append(objects, &appsv1.Deployment{ObjectMeta: metav1.ObjectMeta{Name: "agentapi-session-" + resumeTestRunnerID, Namespace: resumeTestNamespace}})
	}
	client := fake.NewSimpleClientset(objects...)
	manager, err := services.NewKubernetesSessionManagerWithClient(cfg, false, logger.NewLogger(), client)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = manager.Shutdown(time.Second) })
	e := echo.New()
	if err := NewHandlers(manager, resumeTestSecret).RegisterRoutes(e); err != nil {
		t.Fatal(err)
	}
	return &resumePathFixture{client: client, echo: e}
}

func (f *resumePathFixture) serve(t *testing.T, method, path string, body []byte) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, path, bytes.NewReader(body))
	ts := hmacutil.NowTimestamp()
	req.Header.Set(hmacutil.TimestampHeader, ts)
	if len(body) != 0 {
		req.Header.Set("Content-Type", "application/json")
	}
	req.Header.Set("X-Hub-Signature-256", hmacutil.Sign([]byte(resumeTestSecret), hmacutil.BuildMessage(method, path, ts, body)))
	rec := httptest.NewRecorder()
	f.echo.ServeHTTP(rec, req)
	return rec
}

func (f *resumePathFixture) lifecycle(t *testing.T, action string, settings sessionsettings.SessionSettings) *httptest.ResponseRecorder {
	t.Helper()
	body, err := json.Marshal(&settings)
	if err != nil {
		t.Fatal(err)
	}
	return f.serve(t, http.MethodPost, "/api/v1/sessions/"+resumeTestRunnerID+"/"+action, body)
}

func (f *resumePathFixture) storedSettings(t *testing.T) sessionsettings.SessionSettings {
	t.Helper()
	secret, err := f.client.CoreV1().Secrets(resumeTestNamespace).Get(context.Background(), "agentapi-session-"+resumeTestRunnerID+"-settings", metav1.GetOptions{})
	if err != nil {
		t.Fatal(err)
	}
	var settings sessionsettings.SessionSettings
	if err := yaml.Unmarshal(secret.Data["settings.yaml"], &settings); err != nil {
		t.Fatal(err)
	}
	return settings
}

func (f *resumePathFixture) workloadCount(t *testing.T) int {
	t.Helper()
	deployments, err := f.client.AppsV1().Deployments(resumeTestNamespace).List(context.Background(), metav1.ListOptions{})
	if err != nil {
		t.Fatal(err)
	}
	return len(deployments.Items)
}

func parentSettings(resumeFrom string) sessionsettings.SessionSettings {
	return sessionsettings.SessionSettings{Session: sessionsettings.SessionMeta{
		ID: resumeTestPublicID, UserID: "user-1", AgentType: "claude-acp", ResumeFrom: resumeFrom,
	}}
}

func parentCheckpointServer(t *testing.T, status int) (*sessionsettings.ParentRuntimeConfig, *int) {
	t.Helper()
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls++
		w.WriteHeader(status)
	}))
	t.Cleanup(server.Close)
	return &sessionsettings.ParentRuntimeConfig{Enabled: true, Endpoint: server.URL, SessionID: resumeTestPublicID, Token: "runtime-token", Generation: 1}, &calls
}

func TestRepeatedParentResumeKeepsSavedResumeSource(t *testing.T) {
	fixture := newResumePathFixture(t, resumePathOptions{resumeFrom: resumeTestPublicID})

	for attempt := 1; attempt <= 3; attempt++ {
		rec := fixture.lifecycle(t, "resume", parentSettings(""))
		if rec.Code != http.StatusAccepted && rec.Code != http.StatusOK {
			t.Fatalf("attempt %d: status=%d body=%s", attempt, rec.Code, rec.Body.String())
		}
		if got := fixture.storedSettings(t).Session.ResumeFrom; got != resumeTestPublicID {
			t.Fatalf("attempt %d: stored resume_from=%q, want %q", attempt, got, resumeTestPublicID)
		}
	}
}

func TestParentResumeOfSavedWorkspaceSessionUsesOwnSnapshot(t *testing.T) {
	fixture := newResumePathFixture(t, resumePathOptions{resumeFrom: resumeTestPublicID})

	rec := fixture.lifecycle(t, "resume", parentSettings("tpl_source"))
	if rec.Code != http.StatusAccepted {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	if got := fixture.storedSettings(t).Session.ResumeFrom; got != resumeTestPublicID {
		t.Fatalf("stored resume_from=%q, want own snapshot %q", got, resumeTestPublicID)
	}
}

func TestParentResumeWithoutSaveDoesNotRequireOwnSnapshot(t *testing.T) {
	fixture := newResumePathFixture(t, resumePathOptions{})

	rec := fixture.lifecycle(t, "resume", parentSettings(""))
	if rec.Code != http.StatusAccepted {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	if got := fixture.storedSettings(t).Session.ResumeFrom; got != "" {
		t.Fatalf("stored resume_from=%q, want empty without a saved snapshot", got)
	}
}

func TestParentResumeOfUnsavedWorkspaceSessionStartsFromTemplate(t *testing.T) {
	fixture := newResumePathFixture(t, resumePathOptions{resumeFrom: "tpl_source"})

	rec := fixture.lifecycle(t, "resume", parentSettings("tpl_source"))
	if rec.Code != http.StatusAccepted {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	if got := fixture.storedSettings(t).Session.ResumeFrom; got != "tpl_source" {
		t.Fatalf("stored resume_from=%q, want first-start template tpl_source", got)
	}
}

func TestParentResumeDoesNotRewriteSettingsOfExistingWorkload(t *testing.T) {
	fixture := newResumePathFixture(t, resumePathOptions{resumeFrom: resumeTestPublicID})
	first := parentSettings("")
	first.InitialMessage = "first"
	if rec := fixture.lifecycle(t, "resume", first); rec.Code != http.StatusAccepted {
		t.Fatalf("first resume status=%d body=%s", rec.Code, rec.Body.String())
	}
	second := parentSettings("")
	second.InitialMessage = "second"
	if rec := fixture.lifecycle(t, "resume", second); rec.Code != http.StatusAccepted && rec.Code != http.StatusOK {
		t.Fatalf("second resume status=%d body=%s", rec.Code, rec.Body.String())
	}
	if got := fixture.storedSettings(t).InitialMessage; got != "first" {
		t.Fatalf("stored initial message=%q, want settings of the workload that was created", got)
	}
}

func TestProxySessionReportsSuspendedWorkloadWithoutRecreatingIt(t *testing.T) {
	fixture := newResumePathFixture(t, resumePathOptions{})

	rec := fixture.serve(t, http.MethodPost, "/"+resumeTestRunnerID+"/message", []byte(`{"content":"hi","type":"user"}`))
	if rec.Code != http.StatusConflict {
		t.Fatalf("status=%d body=%s, want 409", rec.Code, rec.Body.String())
	}
	var body struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil || body.Error.Code != "session_suspended" {
		t.Fatalf("body=%s, want session_suspended", rec.Body.String())
	}
	if count := fixture.workloadCount(t); count != 0 {
		t.Fatalf("runtime relay recreated %d workloads", count)
	}
	if got := fixture.storedSettings(t).Session.ResumeFrom; got != "" {
		t.Fatalf("runtime relay rewrote resume_from to %q", got)
	}
}

func TestParentSuspendRecordsOwnResumeSourceAfterCheckpoint(t *testing.T) {
	runtime, calls := parentCheckpointServer(t, http.StatusNoContent)
	fixture := newResumePathFixture(t, resumePathOptions{running: true, parentRuntime: runtime})
	settings := parentSettings("")
	settings.ParentRuntime = runtime

	rec := fixture.lifecycle(t, "suspend", settings)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	if *calls != 1 {
		t.Fatalf("checkpoint calls=%d, want 1", *calls)
	}
	if got := fixture.storedSettings(t).Session.ResumeFrom; got != resumeTestPublicID {
		t.Fatalf("stored resume_from=%q, want own snapshot %q", got, resumeTestPublicID)
	}
	if count := fixture.workloadCount(t); count != 0 {
		t.Fatalf("suspend kept %d workloads", count)
	}
}

func TestParentSuspendWritesNothingWhenCheckpointFails(t *testing.T) {
	runtime, _ := parentCheckpointServer(t, http.StatusInternalServerError)
	fixture := newResumePathFixture(t, resumePathOptions{running: true, resumeFrom: resumeTestPublicID, initialMessage: "original", parentRuntime: runtime})
	settings := parentSettings("")
	settings.InitialMessage = "replacement"
	settings.ParentRuntime = runtime

	rec := fixture.lifecycle(t, "suspend", settings)
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status=%d body=%s, want 503", rec.Code, rec.Body.String())
	}
	stored := fixture.storedSettings(t)
	if stored.InitialMessage != "original" || stored.Session.ResumeFrom != resumeTestPublicID {
		t.Fatalf("failed suspend rewrote settings: initial_message=%q resume_from=%q", stored.InitialMessage, stored.Session.ResumeFrom)
	}
	if count := fixture.workloadCount(t); count != 1 {
		t.Fatalf("failed suspend left %d workloads, want 1", count)
	}
}

func TestParentPauseWritesNothingWhenPauseFails(t *testing.T) {
	fixture := newResumePathFixture(t, resumePathOptions{running: true, resumeFrom: resumeTestPublicID, initialMessage: "original"})
	settings := parentSettings("")
	settings.InitialMessage = "replacement"

	rec := fixture.lifecycle(t, "pause", settings)
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status=%d body=%s, want 503", rec.Code, rec.Body.String())
	}
	stored := fixture.storedSettings(t)
	if stored.InitialMessage != "original" || stored.Session.ResumeFrom != resumeTestPublicID {
		t.Fatalf("failed pause rewrote settings: initial_message=%q resume_from=%q", stored.InitialMessage, stored.Session.ResumeFrom)
	}
}
