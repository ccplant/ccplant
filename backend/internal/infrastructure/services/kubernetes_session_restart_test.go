package services

import (
	"context"
	"encoding/json"
	"github.com/takutakahashi/agentapi-proxy/internal/domain/entities"
	"github.com/takutakahashi/agentapi-proxy/pkg/config"
	"github.com/takutakahashi/agentapi-proxy/pkg/logger"
	"github.com/takutakahashi/agentapi-proxy/pkg/sessionsettings"
	"io"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
	"net/http"
	"strings"
	"testing"
	"time"
)

type restartRoundTripper func(*http.Request) (*http.Response, error)

func (f restartRoundTripper) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func TestRestartAppliesCompleteSettingsToPausedProcess(t *testing.T) {
	cfg := &config.Config{KubernetesSession: config.KubernetesSessionConfig{Namespace: "test", BasePort: 9000, ProvisionerToken: "manager-secret"}, SessionPersistence: config.SessionPersistenceConfig{Backend: "volume"}}
	client := fake.NewSimpleClientset(&corev1.Service{ObjectMeta: metav1.ObjectMeta{Name: "agentapi-session-one-svc", Namespace: "test", Annotations: map[string]string{restartHoldAnnotation: "true", restartPhaseAnnotation: "process_paused", restartRequestAnnotation: "manual"}}}, &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: "agentapi-session-one-settings", Namespace: "test"}})
	m, err := NewKubernetesSessionManagerWithClient(cfg, false, logger.NewLogger(), client)
	if err != nil {
		t.Fatal(err)
	}
	if m.suspendCancel != nil {
		m.suspendCancel()
	}
	old := &sessionsettings.SessionSettings{Session: sessionsettings.SessionMeta{ID: "one", UserID: "alice", AgentType: "codex-acp"}, CredentialOwner: "alice", CredentialSyncID: "old", Env: map[string]string{"OLD": "removed"}}
	ks := NewKubernetesSession("one", &entities.RunServerRequest{UserID: "alice", AgentType: "codex-acp"}, "agentapi-session-one", "agentapi-session-one-svc", "", "test", 9000, nil, nil)
	ks.SetProvisionSettings(old)
	ks.SetStatus("active")
	m.sessions["one"] = ks
	calls := 0
	m.restartHTTPClient = &http.Client{Transport: restartRoundTripper(func(r *http.Request) (*http.Response, error) {
		if r.Method == "POST" {
			calls++
			if r.URL.Path != "/restart" || r.Header.Get("Authorization") != "Bearer manager-secret" {
				t.Error("incorrect runtime request")
			}
			var received sessionsettings.SessionSettings
			if err := json.NewDecoder(r.Body).Decode(&received); err != nil {
				t.Error(err)
			}
			if received.Env["NEW"] != "applied" || received.Env["OLD"] != "" || received.Paused {
				t.Error("settings were not replaced")
			}
			return &http.Response{StatusCode: 202, Body: io.NopCloser(strings.NewReader(""))}, nil
		}
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`{"status":"ready","restart_id":"operation"}`))}, nil
	})}
	next := &sessionsettings.SessionSettings{Session: old.Session, Restart: true, CredentialOwner: "alice", CredentialSyncID: "new", Env: map[string]string{"NEW": "applied"}}
	if _, _, err := m.EnsureSessionWorkload(context.Background(), "one"); err == nil {
		t.Fatal("manual hold did not block automatic resume")
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := m.RestartSession(ctx, "one", "operation", next); err != nil {
		t.Fatal(err)
	}
	if calls != 1 {
		t.Fatal("wrong number of agent starts")
	}
	if err := m.RestartSession(ctx, "one", "operation", next); err != nil {
		t.Fatal(err)
	}
	if calls != 1 {
		t.Fatal("idempotent retry restarted agent again")
	}
	svc, _ := client.CoreV1().Services("test").Get(ctx, ks.ServiceName(), metav1.GetOptions{})
	if svc.Annotations[restartHoldAnnotation] != "" {
		t.Fatal("ready operation retained hold")
	}
	if _, err := m.ManagedFileSyncOwner(ctx, "one", "old"); err == nil {
		t.Fatal("old runtime can write credentials")
	}
	owner, err := m.ManagedFileSyncOwner(ctx, "one", "new")
	if err != nil || owner != "alice" {
		t.Fatalf("owner=%s err=%v", owner, err)
	}
	secret, err := client.CoreV1().Secrets("test").Get(ctx, "agentapi-session-one-settings", metav1.GetOptions{})
	if err != nil || !strings.Contains(string(secret.Data["settings.yaml"]), "applied") {
		t.Fatal("restart settings not persisted")
	}
}

func TestPooledRestartValidatesParentSnapshotWithoutMutatingRunner(t *testing.T) {
	cfg := &config.Config{KubernetesSession: config.KubernetesSessionConfig{Namespace: "test", BasePort: 9000}, SessionPersistence: config.SessionPersistenceConfig{Backend: "volume"}}
	client := fake.NewSimpleClientset(&corev1.Service{ObjectMeta: metav1.ObjectMeta{Name: "agentapi-session-runner-svc", Namespace: "test"}})
	m, err := NewKubernetesSessionManagerWithClient(cfg, false, logger.NewLogger(), client)
	if err != nil {
		t.Fatal(err)
	}
	if m.suspendCancel != nil {
		m.suspendCancel()
	}
	ks := NewKubernetesSession("runner", &entities.RunServerRequest{}, "agentapi-session-runner", "agentapi-session-runner-svc", "", "test", 9000, nil, nil)
	m.sessions["runner"] = ks
	current := &sessionsettings.SessionSettings{Session: sessionsettings.SessionMeta{ID: "public", UserID: "alice", AgentType: "codex-acp"}}
	next := *current
	if err := m.ValidateSessionRestartWithCurrent(context.Background(), "runner", current, &next); err != nil {
		t.Fatal(err)
	}
	if ks.ProvisionSettings() != nil {
		t.Fatal("preflight mutated runner")
	}
	next.Session.AgentType = "claude-acp"
	if err := m.ValidateSessionRestartWithCurrent(context.Background(), "runner", current, &next); err == nil {
		t.Fatal("incompatible agent accepted")
	}
	// Pause writes the running settings. Another manager replica must load them
	// even when its existing in-memory runner still has no provision settings.
	if err := m.PrepareSessionResume(context.Background(), "runner", current); err != nil {
		t.Fatal(err)
	}
	ks.SetProvisionSettings(nil)
	if _, err := m.restartableSession("runner"); err != nil {
		t.Fatal(err)
	}
	if ks.ProvisionSettings().Session.ID != "public" {
		t.Fatal("lost public conversation identity")
	}
}
