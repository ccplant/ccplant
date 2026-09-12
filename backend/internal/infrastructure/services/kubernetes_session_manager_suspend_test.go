package services

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes/fake"

	coresessioncontrol "github.com/takutakahashi/agentapi-proxy/internal/core/sessioncontrol"
	"github.com/takutakahashi/agentapi-proxy/internal/domain/entities"
	"github.com/takutakahashi/agentapi-proxy/pkg/config"
	"github.com/takutakahashi/agentapi-proxy/pkg/logger"
)

type suspendCheckpointStore struct {
	events []coresessioncontrol.Event
}

func (s *suspendCheckpointStore) TouchConnection(context.Context, string) error     { return nil }
func (s *suspendCheckpointStore) IsConnected(context.Context, string) (bool, error) { return true, nil }
func (s *suspendCheckpointStore) EnqueueCommand(_ context.Context, _ string, command coresessioncontrol.Command) (string, error) {
	s.events = append(s.events, coresessioncontrol.Event{StreamID: "1-0", Type: "command_completed", CommandID: command.ID, Payload: json.RawMessage(`{}`)})
	return "1-0", nil
}
func (s *suspendCheckpointStore) ReadCommands(context.Context, string, string, time.Duration, int64) ([]coresessioncontrol.Command, error) {
	return nil, nil
}
func (s *suspendCheckpointStore) AckCommand(context.Context, string, string) error { return nil }
func (s *suspendCheckpointStore) AppendEvents(context.Context, string, []coresessioncontrol.Event) (string, error) {
	return "", nil
}
func (s *suspendCheckpointStore) ReadEvents(context.Context, string, string, time.Duration, int64) ([]coresessioncontrol.Event, error) {
	return s.events, nil
}

func newSuspendTestManager(t *testing.T, objects ...runtime.Object) *KubernetesSessionManager {
	t.Helper()
	pvcEnabled := true
	client := fake.NewSimpleClientset(objects...)
	cfg := &config.Config{
		KubernetesSession:  config.KubernetesSessionConfig{Namespace: "test-ns", BasePort: 9000, PVCEnabled: &pvcEnabled},
		SessionPersistence: config.SessionPersistenceConfig{Backend: "volume", SuspendAfter: "1h"},
	}
	manager, err := NewKubernetesSessionManagerWithClient(cfg, false, logger.NewLogger(), client)
	if err != nil {
		t.Fatal(err)
	}
	if manager.suspendCancel != nil {
		manager.suspendCancel()
	}
	return manager
}

func TestScheduleAndReconcileSessionSuspend(t *testing.T) {
	service := &corev1.Service{ObjectMeta: metav1.ObjectMeta{
		Name: "agentapi-session-session-1-svc", Namespace: "test-ns",
		Labels: map[string]string{"agentapi.proxy/session-id": "session-1"},
	}}
	deployment := &appsv1.Deployment{ObjectMeta: metav1.ObjectMeta{Name: "agentapi-session-session-1", Namespace: "test-ns"}}
	manager := newSuspendTestManager(t, service, deployment)
	session := NewKubernetesSession("session-1", &entities.RunServerRequest{UserID: "user-1", AgentType: "codex-acp"},
		deployment.Name, service.Name, "session-1-pvc", "test-ns", 9000, nil, nil)
	manager.sessions[session.id] = session
	manager.SetSessionControlStore(&suspendCheckpointStore{})

	if err := manager.ScheduleSessionSuspend(context.Background(), session.id); err != nil {
		t.Fatal(err)
	}
	stored, err := manager.client.CoreV1().Services("test-ns").Get(context.Background(), service.Name, metav1.GetOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := time.Parse(time.RFC3339Nano, stored.Annotations[sessionSuspendAtAnnotation]); err != nil {
		t.Fatalf("suspend deadline was not persisted: %v", err)
	}

	stored.Annotations[sessionSuspendAtAnnotation] = time.Now().Add(-time.Minute).UTC().Format(time.RFC3339Nano)
	if _, err := manager.client.CoreV1().Services("test-ns").Update(context.Background(), stored, metav1.UpdateOptions{}); err != nil {
		t.Fatal(err)
	}
	manager.reconcileSessionSuspends(context.Background())
	if _, err := manager.client.AppsV1().Deployments("test-ns").Get(context.Background(), deployment.Name, metav1.GetOptions{}); !apierrors.IsNotFound(err) {
		t.Fatalf("deployment should be removed on suspend, got %v", err)
	}
	stored, err = manager.client.CoreV1().Services("test-ns").Get(context.Background(), service.Name, metav1.GetOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if stored.Annotations[sessionSuspendAtAnnotation] != "" || stored.Annotations[sessionSuspendedAtAnnotation] == "" {
		t.Fatalf("unexpected suspend annotations: %#v", stored.Annotations)
	}
	if session.Status() != "suspended" {
		t.Fatalf("status = %q, want suspended", session.Status())
	}
}

func TestScheduleSessionSuspendUsesScopedSettings(t *testing.T) {
	service := &corev1.Service{ObjectMeta: metav1.ObjectMeta{
		Name: "agentapi-session-session-1-svc", Namespace: "test-ns",
		Labels: map[string]string{"agentapi.proxy/session-id": "session-1"},
	}}
	manager := newSuspendTestManager(t, service)
	settings := entities.NewSettings("user-1")
	settings.SetAutoSuspend(&entities.AutoSuspendSettings{Enabled: true, IdleTimeoutMinutes: 1})
	manager.SetSettingsRepository(&fakeSettingsRepository{settings: map[string]*entities.Settings{"user-1": settings}})
	session := NewKubernetesSession("session-1", &entities.RunServerRequest{UserID: "user-1", Scope: entities.ScopeUser},
		"agentapi-session-session-1", service.Name, "session-1-pvc", "test-ns", 9000, nil, nil)
	manager.sessions[session.id] = session

	before := time.Now().Add(50 * time.Second)
	if err := manager.ScheduleSessionSuspend(context.Background(), session.id); err != nil {
		t.Fatal(err)
	}
	stored, err := manager.client.CoreV1().Services("test-ns").Get(context.Background(), service.Name, metav1.GetOptions{})
	if err != nil {
		t.Fatal(err)
	}
	deadline, err := time.Parse(time.RFC3339Nano, stored.Annotations[sessionSuspendAtAnnotation])
	if err != nil || deadline.Before(before) || stored.Annotations[sessionAutoSuspendIdleSecondsAnnotation] != "60" {
		t.Fatalf("unexpected scoped deadline/annotations: deadline=%v annotations=%#v err=%v", deadline, stored.Annotations, err)
	}

	settings.SetAutoSuspend(&entities.AutoSuspendSettings{Enabled: false, IdleTimeoutMinutes: 1})
	if err := manager.ScheduleSessionSuspend(context.Background(), session.id); err != nil {
		t.Fatal(err)
	}
	stored, _ = manager.client.CoreV1().Services("test-ns").Get(context.Background(), service.Name, metav1.GetOptions{})
	if stored.Annotations[sessionSuspendAtAnnotation] != "" || stored.Annotations[sessionAutoSuspendEnabledAnnotation] != "false" {
		t.Fatalf("disabled policy retained a deadline: %#v", stored.Annotations)
	}
}

func TestResolveAutoSuspendPolicyForRemoteSessionWithoutLocalPersistence(t *testing.T) {
	manager := newSuspendTestManager(t)
	manager.config.SessionPersistence.Backend = ""
	settings := entities.NewSettings("user-1")
	settings.SetAutoSuspend(&entities.AutoSuspendSettings{Enabled: true, IdleTimeoutMinutes: 1})
	manager.SetSettingsRepository(&fakeSettingsRepository{settings: map[string]*entities.Settings{"user-1": settings}})
	session := NewKubernetesSession("session-1", &entities.RunServerRequest{UserID: "user-1", Scope: entities.ScopeUser},
		"agentapi-session-session-1", "agentapi-session-session-1-svc", "session-1-pvc", "test-ns", 9000, nil, nil)

	after, enabled, err := manager.resolveAutoSuspendPolicy(context.Background(), session)
	if err != nil {
		t.Fatal(err)
	}
	if !enabled || after != time.Minute {
		t.Fatalf("remote policy = (%v, %v), want (1m, true)", after, enabled)
	}
}
