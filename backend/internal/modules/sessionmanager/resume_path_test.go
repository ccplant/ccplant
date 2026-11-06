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
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
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

type resumePathFixture struct {
	client *fake.Clientset
	echo   *echo.Echo
}

func newResumePathFixture(t *testing.T, initialResumeFrom string) *resumePathFixture {
	t.Helper()
	pvcEnabled := true
	cfg := &config.Config{KubernetesSession: config.KubernetesSessionConfig{
		Namespace: resumeTestNamespace, Image: "test-image:latest", BasePort: 9000,
		PVCEnabled: &pvcEnabled, CPURequest: "100m", CPULimit: "1",
		MemoryRequest: "128Mi", MemoryLimit: "512Mi",
	}, SessionPersistence: config.SessionPersistenceConfig{Backend: "volume", SuspendAfter: "1h"}}
	settingsData, err := sessionsettings.MarshalYAML(&sessionsettings.SessionSettings{Session: sessionsettings.SessionMeta{
		ID: resumeTestPublicID, UserID: "user-1", AgentType: "claude-acp", PersistenceEnabled: true, ResumeFrom: initialResumeFrom,
	}})
	if err != nil {
		t.Fatal(err)
	}
	client := fake.NewSimpleClientset(
		&corev1.Service{ObjectMeta: metav1.ObjectMeta{
			Name: "agentapi-session-" + resumeTestRunnerID + "-svc", Namespace: resumeTestNamespace,
			Labels: map[string]string{"agentapi.proxy/session-id": resumeTestRunnerID, "agentapi.proxy/user-id": "user-1"},
			Annotations: map[string]string{
				"agentapi.proxy/agent-type":   "claude-acp",
				"agentapi.proxy/suspended-at": time.Now().UTC().Format(time.RFC3339Nano),
			},
		}, Spec: corev1.ServiceSpec{Ports: []corev1.ServicePort{{Port: 9000}}}},
		&corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: "agentapi-session-" + resumeTestRunnerID + "-settings", Namespace: resumeTestNamespace}, Data: map[string][]byte{"settings.yaml": settingsData}},
	)
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

func (f *resumePathFixture) resume(t *testing.T, settings sessionsettings.SessionSettings) *httptest.ResponseRecorder {
	t.Helper()
	body, err := json.Marshal(&settings)
	if err != nil {
		t.Fatal(err)
	}
	return f.serve(t, http.MethodPost, "/api/v1/sessions/"+resumeTestRunnerID+"/resume", body)
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

func parentResumeSettings(resumeFrom string) sessionsettings.SessionSettings {
	return sessionsettings.SessionSettings{Session: sessionsettings.SessionMeta{
		ID: resumeTestPublicID, UserID: "user-1", AgentType: "claude-acp", ResumeFrom: resumeFrom,
	}}
}

func TestRepeatedParentResumeKeepsOwnResumeSource(t *testing.T) {
	fixture := newResumePathFixture(t, "")

	for attempt := 1; attempt <= 3; attempt++ {
		rec := fixture.resume(t, parentResumeSettings(""))
		if rec.Code != http.StatusAccepted && rec.Code != http.StatusOK {
			t.Fatalf("attempt %d: status=%d body=%s", attempt, rec.Code, rec.Body.String())
		}
		if got := fixture.storedSettings(t).Session.ResumeFrom; got != resumeTestPublicID {
			t.Fatalf("attempt %d: stored resume_from=%q, want %q", attempt, got, resumeTestPublicID)
		}
	}
}

func TestParentResumeReplacesTemplateSourceWithOwnSnapshot(t *testing.T) {
	fixture := newResumePathFixture(t, "tpl_source")

	rec := fixture.resume(t, parentResumeSettings("tpl_source"))
	if rec.Code != http.StatusAccepted {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	if got := fixture.storedSettings(t).Session.ResumeFrom; got != resumeTestPublicID {
		t.Fatalf("stored resume_from=%q, want own snapshot %q", got, resumeTestPublicID)
	}
}

func TestParentResumeDoesNotRewriteSettingsOfExistingWorkload(t *testing.T) {
	fixture := newResumePathFixture(t, "")
	first := parentResumeSettings("")
	first.InitialMessage = "first"
	if rec := fixture.resume(t, first); rec.Code != http.StatusAccepted {
		t.Fatalf("first resume status=%d body=%s", rec.Code, rec.Body.String())
	}
	second := parentResumeSettings("")
	second.InitialMessage = "second"
	if rec := fixture.resume(t, second); rec.Code != http.StatusAccepted && rec.Code != http.StatusOK {
		t.Fatalf("second resume status=%d body=%s", rec.Code, rec.Body.String())
	}
	if got := fixture.storedSettings(t).InitialMessage; got != "first" {
		t.Fatalf("stored initial message=%q, want settings of the workload that was created", got)
	}
}

func TestProxySessionReportsSuspendedWorkloadWithoutRecreatingIt(t *testing.T) {
	fixture := newResumePathFixture(t, "")

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
	deployments, err := fixture.client.AppsV1().Deployments(resumeTestNamespace).List(context.Background(), metav1.ListOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(deployments.Items) != 0 {
		t.Fatalf("runtime relay recreated %d workloads", len(deployments.Items))
	}
	if got := fixture.storedSettings(t).Session.ResumeFrom; got != "" {
		t.Fatalf("runtime relay rewrote resume_from to %q", got)
	}
}
