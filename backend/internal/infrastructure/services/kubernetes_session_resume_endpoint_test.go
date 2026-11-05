package services

import (
	"context"
	"testing"

	"github.com/takutakahashi/agentapi-proxy/internal/domain/entities"
	"github.com/takutakahashi/agentapi-proxy/pkg/config"
	"github.com/takutakahashi/agentapi-proxy/pkg/logger"
	"github.com/takutakahashi/agentapi-proxy/pkg/sessionsettings"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes/fake"
)

func TestPrepareSessionResumeUsesRunnerParentURLForParentRuntimeEndpoint(t *testing.T) {
	const upstream = "https://cc-api.example.com/api/v1"
	tests := []struct {
		name             string
		runnerParentURL  string
		withSecret       bool
		incoming         string
		expectedEndpoint string
	}{
		{name: "loopback endpoint from internal trigger", runnerParentURL: upstream, withSecret: true, incoming: "https://127.0.0.1:8080", expectedEndpoint: upstream},
		{name: "cluster service endpoint from internal trigger", runnerParentURL: upstream, withSecret: true, incoming: "https://agentapi-proxy.ccplant.svc.cluster.local:8080", expectedEndpoint: upstream},
		{name: "empty endpoint", runnerParentURL: upstream, withSecret: true, incoming: "", expectedEndpoint: upstream},
		{name: "settings secret not created yet", runnerParentURL: upstream, incoming: "https://127.0.0.1:8080", expectedEndpoint: upstream},
		{name: "runner parent URL with trailing slash", runnerParentURL: upstream + "/", withSecret: true, incoming: "https://127.0.0.1:8080", expectedEndpoint: upstream},
		{name: "manager without runner pool keeps incoming endpoint", withSecret: true, incoming: "https://cc-api.example.com", expectedEndpoint: "https://cc-api.example.com"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			objects := []runtime.Object{&corev1.Service{ObjectMeta: metav1.ObjectMeta{Name: "agentapi-session-one-svc", Namespace: "test"}}}
			if tt.withSecret {
				objects = append(objects, &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: "agentapi-session-one-settings", Namespace: "test"}})
			}
			m := newResumeEndpointTestManager(t, objects...)
			if tt.runnerParentURL != "" {
				m.ConfigureSessionRunnerPool(tt.runnerParentURL, "manager", "manager-token", "pool")
			}
			ks := NewKubernetesSession("one", &entities.RunServerRequest{UserID: "alice", AgentType: "claude-acp"}, "agentapi-session-one", "agentapi-session-one-svc", "", "test", 9000, nil, nil)
			m.sessions["one"] = ks

			incoming := resumeEndpointSettings(tt.incoming)
			if err := m.PrepareSessionResume(context.Background(), "one", incoming); err != nil {
				t.Fatal(err)
			}

			if got := ks.ProvisionSettings().ParentRuntime.Endpoint; got != tt.expectedEndpoint {
				t.Fatalf("in-memory endpoint = %q, want %q", got, tt.expectedEndpoint)
			}
			persisted, err := m.CurrentSessionSettings(context.Background(), "one")
			if err != nil {
				t.Fatal(err)
			}
			if got := persisted.ParentRuntime.Endpoint; got != tt.expectedEndpoint {
				t.Fatalf("persisted endpoint = %q, want %q", got, tt.expectedEndpoint)
			}
			if persisted.ParentRuntime.Token != "fresh-token" || persisted.ParentRuntime.Generation != 2 || persisted.ParentRuntime.SessionID != "public" {
				t.Fatal("resume runtime credentials were not applied")
			}
			if incoming.ParentRuntime.Endpoint != tt.incoming {
				t.Fatalf("incoming settings were mutated: endpoint = %q", incoming.ParentRuntime.Endpoint)
			}
		})
	}
}

func TestPrepareSessionResumeWithoutParentRuntime(t *testing.T) {
	m := newResumeEndpointTestManager(t,
		&corev1.Service{ObjectMeta: metav1.ObjectMeta{Name: "agentapi-session-one-svc", Namespace: "test"}},
		&corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: "agentapi-session-one-settings", Namespace: "test"}},
	)
	m.ConfigureSessionRunnerPool("https://cc-api.example.com/api/v1", "manager", "manager-token", "pool")
	ks := NewKubernetesSession("one", &entities.RunServerRequest{UserID: "alice", AgentType: "claude-acp"}, "agentapi-session-one", "agentapi-session-one-svc", "", "test", 9000, nil, nil)
	m.sessions["one"] = ks

	settings := resumeEndpointSettings("")
	settings.ParentRuntime = nil
	if err := m.PrepareSessionResume(context.Background(), "one", settings); err != nil {
		t.Fatal(err)
	}

	if got := ks.ProvisionSettings().ParentRuntime; got != nil {
		t.Fatalf("parent runtime = %+v, want nil", got)
	}
}

func newResumeEndpointTestManager(t *testing.T, objects ...runtime.Object) *KubernetesSessionManager {
	t.Helper()
	client := fake.NewSimpleClientset(objects...)
	cfg := &config.Config{KubernetesSession: config.KubernetesSessionConfig{Namespace: "test", BasePort: 9000}, SessionPersistence: config.SessionPersistenceConfig{Backend: "volume"}}
	m, err := NewKubernetesSessionManagerWithClient(cfg, false, logger.NewLogger(), client)
	if err != nil {
		t.Fatal(err)
	}
	if m.suspendCancel != nil {
		m.suspendCancel()
	}
	return m
}

func resumeEndpointSettings(endpoint string) *sessionsettings.SessionSettings {
	return &sessionsettings.SessionSettings{
		Session: sessionsettings.SessionMeta{ID: "public", UserID: "alice", AgentType: "claude-acp"},
		ParentRuntime: &sessionsettings.ParentRuntimeConfig{
			Enabled: true, Endpoint: endpoint, SessionID: "public", ManagerID: "manager", Token: "fresh-token", Generation: 2,
		},
	}
}
