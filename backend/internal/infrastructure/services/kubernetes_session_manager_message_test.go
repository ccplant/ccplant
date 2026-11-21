package services

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/takutakahashi/agentapi-proxy/internal/domain/entities"
	portrepos "github.com/takutakahashi/agentapi-proxy/internal/usecases/ports/repositories"
	"github.com/takutakahashi/agentapi-proxy/pkg/config"
	"github.com/takutakahashi/agentapi-proxy/pkg/sessionsettings"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes/fake"
)

// TestBroadcastMessageUpdate_UpdatesLastMessageAt verifies that broadcastMessageUpdate
// updates the session's lastMessageAt field so late-arriving pollers can detect missed events.
func TestBroadcastMessageUpdate_UpdatesLastMessageAt(t *testing.T) {
	m := newTestManagerForCycle(t)

	session := NewKubernetesSession(
		"test-session",
		&entities.RunServerRequest{UserID: "user1"},
		"test-deploy", "test-svc", "test-pvc", "test-ns",
		9000, nil, nil,
	)
	before := time.Now()
	// Set lastMessageAt to a known past time.
	session.SetLastMessageAt(before.Add(-1 * time.Hour))

	m.mutex.Lock()
	m.sessions["test-session"] = session
	m.mutex.Unlock()

	m.broadcastMessageUpdate("test-session")

	if !session.LastMessageAt().After(before.Add(-1 * time.Hour)) {
		t.Errorf("lastMessageAt was not updated: got %v, want after %v",
			session.LastMessageAt(), before.Add(-1*time.Hour))
	}
	if session.LastMessageAt().Before(before) {
		t.Errorf("lastMessageAt %v should be >= broadcast time %v", session.LastMessageAt(), before)
	}
}

func TestIsACPAgentTypeIncludesCursor(t *testing.T) {
	if !isACPAgentType("cursor") {
		t.Fatal("expected cursor to be treated as an ACP agent type")
	}
}

func TestIsACPAgentTypeIncludesPi(t *testing.T) {
	if !isACPAgentType("pi") {
		t.Fatal("expected pi to be treated as an ACP agent type")
	}
}

func TestIsACPAgentTypeIncludesAntigravity(t *testing.T) {
	if !isACPAgentType("antigravity") {
		t.Fatal("expected antigravity to use ACP transport")
	}
}

func TestSessionServiceDoesNotExposeAgentRuntimePort(t *testing.T) {
	m := &KubernetesSessionManager{k8sConfig: &config.KubernetesSessionConfig{BasePort: 9000}}
	ports := m.buildServicePorts(nil)
	for _, port := range ports {
		if port.Port == 9000 || port.TargetPort.IntValue() == 9000 {
			t.Fatalf("agent runtime port is exposed by the Service: %#v", port)
		}
	}
}

// TestSubscribeMessageEvents_ReceivesBroadcast verifies that a subscriber
// registered before broadcastMessageUpdate is called receives the event.
func TestSubscribeMessageEvents_ReceivesBroadcast(t *testing.T) {
	m := newTestManagerForCycle(t)

	session := NewKubernetesSession(
		"test-session",
		&entities.RunServerRequest{UserID: "user1"},
		"test-deploy", "test-svc", "test-pvc", "test-ns",
		9000, nil, nil,
	)
	m.mutex.Lock()
	m.sessions["test-session"] = session
	m.mutex.Unlock()

	ch, cancel := m.SubscribeMessageEvents("test-session")
	defer cancel()

	m.broadcastMessageUpdate("test-session")

	select {
	case evt := <-ch:
		if evt.SessionID != "test-session" {
			t.Errorf("unexpected session ID: %s", evt.SessionID)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for message event")
	}
}

func TestStopAgentRejectsMissingOutboundControlConnection(t *testing.T) {
	m := newTestManagerForCycle(t)
	session := NewKubernetesSession(
		"test-session",
		&entities.RunServerRequest{UserID: "user1", AgentType: "codex-acp"},
		"test-deploy", "test-svc", "test-pvc", "test-ns",
		9000, nil, nil,
	)
	session.SetStatusSilent("active")
	m.mutex.Lock()
	m.sessions["test-session"] = session
	m.mutex.Unlock()

	called := false
	withRoundTripper(t, func(req *http.Request) (*http.Response, error) {
		called = true
		return jsonResponse(http.StatusOK), nil
	})

	err := m.StopAgent(context.Background(), "test-session")
	if err == nil || !strings.Contains(err.Error(), "outbound control connection") {
		t.Fatalf("StopAgent() error = %v, want unavailable outbound connection", err)
	}
	if called {
		t.Fatal("StopAgent attempted direct HTTP access to the session Pod")
	}
}

func TestRestoreSessionFromServiceRestoresAgentType(t *testing.T) {
	m := newTestManagerForCycle(t)
	svc := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name: "agentapi-session-test-session-svc",
			Labels: map[string]string{
				"agentapi.proxy/session-id": "test-session",
				"agentapi.proxy/user-id":    "user1",
				"agentapi.proxy/scope":      "user",
				"agentapi.proxy/agent-type": "codex-acp",
			},
			Annotations: map[string]string{
				"agentapi.proxy/agent-type": "codex-acp",
			},
		},
		Spec: corev1.ServiceSpec{
			Ports: []corev1.ServicePort{{Port: 9000}},
		},
	}

	session := m.restoreSessionFromService(svc)
	if session == nil || session.Request() == nil {
		t.Fatalf("expected restored session, got %#v", session)
	}
	if session.Request().AgentType != "codex-acp" {
		t.Fatalf("expected AgentType codex-acp, got %q", session.Request().AgentType)
	}
}

func TestSupportedAgentTypeOrDefault(t *testing.T) {
	tests := []struct {
		name      string
		agentType string
		want      string
	}{
		{name: "default", agentType: "", want: ""},
		{name: "claude acp", agentType: "claude-acp", want: "claude-acp"},
		{name: "codex acp", agentType: "codex-acp", want: "codex-acp"},
		{name: "pi ollama", agentType: "pi", want: "pi"},
		{name: "cursor", agentType: "cursor", want: "cursor"},
		{name: "antigravity", agentType: "antigravity", want: "antigravity"},
		{name: "unknown", agentType: "unknown-agent", want: ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := supportedAgentTypeOrDefault(tt.agentType); got != tt.want {
				t.Fatalf("supportedAgentTypeOrDefault(%q) = %q, want %q", tt.agentType, got, tt.want)
			}
		})
	}
}

func TestResolveAutoAgentType(t *testing.T) {
	const namespace = "test"
	codexFiles := sessionsettings.FilesToSecretData([]sessionsettings.ManagedFile{{
		Path:    sessionsettings.ManagedFileTypes[sessionsettings.FileTypeCodexAuth],
		Content: `{}`,
	}})
	tests := []struct {
		name      string
		agentType string
		objects   []runtime.Object
		want      string
	}{
		{name: "explicit agent is unchanged", agentType: "cursor", want: "cursor"},
		{name: "auto without credentials uses claude", agentType: "auto", want: "claude-acp"},
		{
			name:      "auto with codex auth uses codex",
			agentType: "auto",
			objects: []runtime.Object{&corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{Name: "agentapi-agent-files-user1", Namespace: namespace},
				Data:       codexFiles,
			}},
			want: "codex-acp",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := &KubernetesSessionManager{client: fake.NewSimpleClientset(tt.objects...), namespace: namespace}
			req := &entities.RunServerRequest{UserID: "user1", Scope: entities.ScopeUser, AgentType: tt.agentType}
			if got := m.resolveAutoAgentType(context.Background(), req); got != tt.want {
				t.Fatalf("resolveAutoAgentType() = %q, want %q", got, tt.want)
			}
		})
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func withRoundTripper(t *testing.T, f roundTripFunc) {
	t.Helper()
	originalClient := http.DefaultClient
	http.DefaultClient = &http.Client{Transport: f}
	t.Cleanup(func() {
		http.DefaultClient = originalClient
	})
}

func jsonResponse(statusCode int) *http.Response {
	return &http.Response{
		StatusCode: statusCode,
		Body:       io.NopCloser(bytes.NewBufferString(`{"ok":true}`)),
		Header:     make(http.Header),
	}
}

func TestCrossPodCompletionPreservesTTLStart(t *testing.T) {
	m := newTestManagerForCycle(t)
	session := NewKubernetesSession("ttl-session", &entities.RunServerRequest{}, "deploy", "svc", "pvc", "ns", 9000, nil, nil)
	session.SetStatus("running")
	session.SetUpdatedAt(time.Now().Add(-100 * time.Hour))
	m.sessions[session.ID()] = session
	completedAt := time.Now().Add(-30 * time.Minute)
	events := make(chan portrepos.StatusChangeEvent, 2)
	event := portrepos.StatusChangeEvent{SessionID: session.ID(), Status: "active", UpdatedAt: completedAt, PodID: "other-pod"}
	events <- event
	events <- event // Duplicate delivery must not extend the TTL.
	close(events)
	m.consumeStatusEvents(context.Background(), events)
	if session.Status() != "active" || !session.UpdatedAt().Equal(completedAt) {
		t.Fatalf("completion metadata lost: status=%s updatedAt=%s", session.Status(), session.UpdatedAt())
	}
}
