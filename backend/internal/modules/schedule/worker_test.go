package schedule

import (
	"context"
	"testing"
	"time"

	"github.com/takutakahashi/agentapi-proxy/internal/domain/entities"
	"github.com/takutakahashi/agentapi-proxy/internal/usecases/ports/repositories"
	"k8s.io/client-go/kubernetes/fake"
)

// mockProxySessionManager implements portrepos.SessionManager for testing
type mockProxySessionManager struct {
	sessions  map[string]*mockProxySession
	createErr error
}

type mockProxySession struct {
	id        string
	userID    string
	status    string
	addr      string
	tags      map[string]string
	startedAt time.Time
	updatedAt time.Time
}

func (s *mockProxySession) ID() string                    { return s.id }
func (s *mockProxySession) Addr() string                  { return s.addr }
func (s *mockProxySession) UserID() string                { return s.userID }
func (s *mockProxySession) Tags() map[string]string       { return s.tags }
func (s *mockProxySession) Status() string                { return s.status }
func (s *mockProxySession) StartedAt() time.Time          { return s.startedAt }
func (s *mockProxySession) UpdatedAt() time.Time          { return s.updatedAt }
func (s *mockProxySession) LastMessageAt() time.Time      { return s.startedAt }
func (s *mockProxySession) Description() string           { return "" }
func (s *mockProxySession) Scope() entities.ResourceScope { return entities.ScopeUser }
func (s *mockProxySession) TeamID() string                { return "" }
func (s *mockProxySession) Cancel()                       {}

func newMockProxySessionManager() *mockProxySessionManager {
	return &mockProxySessionManager{sessions: make(map[string]*mockProxySession)}
}

func (m *mockProxySessionManager) CreateSession(ctx context.Context, id string, req *entities.RunServerRequest, webhookPayload []byte) (entities.Session, error) {
	if m.createErr != nil {
		return nil, m.createErr
	}
	now := time.Now()
	session := &mockProxySession{
		id:        id,
		userID:    req.UserID,
		status:    "active",
		addr:      "localhost:9000",
		tags:      req.Tags,
		startedAt: now,
		updatedAt: now,
	}
	m.sessions[id] = session
	return session, nil
}

func TestWorkerBuildLaunchRequestDelegatesReuseToStart(t *testing.T) {
	executedAt := time.Date(2026, time.October, 20, 6, 7, 8, 0, time.UTC)
	worker := NewWorker(nil, newMockProxySessionManager(), WorkerConfig{}, nil)

	for _, tt := range []struct {
		name              string
		reuseSession      bool
		wantExecutionTime bool
	}{
		{name: "reuse", reuseSession: true},
		{name: "new session", wantExecutionTime: true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			schedule := &Schedule{
				ID: "schedule-1", Name: "daily", UserID: "alice",
				SessionConfig: SessionConfig{
					Tags:         map[string]string{"custom": "value"},
					Params:       &entities.SessionParams{Message: "run"},
					ReuseSession: tt.reuseSession,
				},
			}
			request := worker.buildLaunchRequest(schedule, "session-1", executedAt)

			if !request.DeferReuseToStart {
				t.Fatal("schedule reuse must be delegated to /start")
			}
			if request.ReuseSession {
				t.Fatal("schedule worker must not run its own reuse lookup")
			}
			if request.ReuseMessage != "run" {
				t.Fatalf("ReuseMessage = %q, want run", request.ReuseMessage)
			}
			if len(request.Tags) != len(request.ReuseMatchTags) {
				t.Fatalf("Tags = %#v, ReuseMatchTags = %#v", request.Tags, request.ReuseMatchTags)
			}
			for key, value := range request.Tags {
				if request.ReuseMatchTags[key] != value {
					t.Fatalf("ReuseMatchTags[%q] = %q, want %q", key, request.ReuseMatchTags[key], value)
				}
			}
			_, hasExecutionTime := request.Tags[ScheduleExecutionTimeTag]
			if hasExecutionTime != tt.wantExecutionTime {
				t.Fatalf("%s presence = %t, want %t", ScheduleExecutionTimeTag, hasExecutionTime, tt.wantExecutionTime)
			}
		})
	}
}

func (m *mockProxySessionManager) GetSession(id string) entities.Session {
	s, ok := m.sessions[id]
	if !ok {
		return nil
	}
	return s
}

func (m *mockProxySessionManager) ListSessions(filter entities.SessionFilter) []entities.Session {
	var result []entities.Session
	for _, s := range m.sessions {
		result = append(result, s)
	}
	return result
}

func (m *mockProxySessionManager) DeleteSession(id string) error {
	delete(m.sessions, id)
	return nil
}

func (m *mockProxySessionManager) SendMessage(ctx context.Context, id string, message string) error {
	return nil
}

func (m *mockProxySessionManager) StopAgent(ctx context.Context, id string) error {
	return nil
}

func (m *mockProxySessionManager) GetMessages(ctx context.Context, id string) ([]repositories.Message, error) {
	return nil, nil
}

func (m *mockProxySessionManager) Shutdown(timeout time.Duration) error {
	return nil
}

func TestWorker_StartStop(t *testing.T) {
	client := fake.NewSimpleClientset()
	manager := NewKubernetesManager(client, "default")
	sessionManager := newMockProxySessionManager()

	config := WorkerConfig{
		CheckInterval: 100 * time.Millisecond,
		Enabled:       true,
	}

	worker := NewWorker(manager, sessionManager, config, nil)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Start worker
	err := worker.Start(ctx)
	if err != nil {
		t.Fatalf("Start() error = %v", err)
	}

	// Wait a bit
	time.Sleep(50 * time.Millisecond)

	// Stop worker
	worker.Stop()
}

func TestWorker_ProcessSchedules(t *testing.T) {
	client := fake.NewSimpleClientset()
	manager := NewKubernetesManager(client, "default")
	sessionManager := newMockProxySessionManager()

	ctx := context.Background()

	// Create a due schedule
	now := time.Now()
	past := now.Add(-time.Hour)
	schedule := &Schedule{
		ID:              "test-schedule",
		Name:            "Test Schedule",
		UserID:          "test-user",
		Status:          ScheduleStatusActive,
		ScheduledAt:     &past,
		NextExecutionAt: &past,
		SessionConfig: SessionConfig{
			Tags: map[string]string{"test": "true"},
		},
	}
	if err := manager.Create(ctx, schedule); err != nil {
		t.Fatalf("Create() error = %v", err)
	}

	config := WorkerConfig{
		CheckInterval: 100 * time.Millisecond,
		Enabled:       true,
	}

	worker := NewWorker(manager, sessionManager, config, nil)

	// Process schedules
	worker.processSchedules(ctx)

	// Verify session was created
	if len(sessionManager.sessions) != 1 {
		t.Errorf("expected 1 session, got %d", len(sessionManager.sessions))
	}

	// Verify execution was recorded
	updated, _ := manager.Get(ctx, "test-schedule")
	if updated.LastExecution == nil {
		t.Error("expected execution to be recorded")
	}
	if updated.LastExecution.Status != "success" {
		t.Errorf("expected status 'success', got %q", updated.LastExecution.Status)
	}
	if updated.ExecutionCount != 1 {
		t.Errorf("expected execution count 1, got %d", updated.ExecutionCount)
	}
}

func TestWorker_DelegatesActiveSessionReuseToStart(t *testing.T) {
	client := fake.NewSimpleClientset()
	manager := NewKubernetesManager(client, "default")
	sessionManager := newMockProxySessionManager()

	ctx := context.Background()

	// Create an active session
	prevSession := &mockProxySession{
		id:        "prev-session",
		userID:    "test-user",
		status:    "active",
		addr:      "localhost:9000",
		startedAt: time.Now(),
	}
	sessionManager.sessions["prev-session"] = prevSession

	// Create a due schedule with previous execution
	now := time.Now()
	past := now.Add(-time.Hour)
	schedule := &Schedule{
		ID:              "test-schedule",
		Name:            "Test Schedule",
		UserID:          "test-user",
		Status:          ScheduleStatusActive,
		CronExpr:        "* * * * *",
		NextExecutionAt: &past,
		LastExecution: &ExecutionRecord{
			ExecutedAt: past,
			SessionID:  "prev-session",
			Status:     "success",
		},
		SessionConfig: SessionConfig{
			Tags: map[string]string{"test": "true"},
		},
	}
	if err := manager.Create(ctx, schedule); err != nil {
		t.Fatalf("Create() error = %v", err)
	}

	config := WorkerConfig{
		CheckInterval: 100 * time.Millisecond,
		Enabled:       true,
	}

	worker := NewWorker(manager, sessionManager, config, nil)

	// Process schedules
	worker.processSchedules(ctx)

	// The schedule worker must not skip based on its own session lookup. The
	// SessionManager implementation owns the /start call and its reuse decision.
	if len(sessionManager.sessions) != 2 {
		t.Errorf("expected the /start delegate to be called, got %d sessions", len(sessionManager.sessions))
	}

	// Verify the delegated execution was recorded.
	updated, _ := manager.Get(ctx, "test-schedule")
	if updated.LastExecution == nil {
		t.Error("expected execution to be recorded")
	}
	if updated.LastExecution.Status != "success" {
		t.Errorf("expected status 'success', got %q", updated.LastExecution.Status)
	}
}

func TestWorker_OneTimeScheduleCompleted(t *testing.T) {
	client := fake.NewSimpleClientset()
	manager := NewKubernetesManager(client, "default")
	sessionManager := newMockProxySessionManager()

	ctx := context.Background()

	// Create a one-time schedule
	now := time.Now()
	past := now.Add(-time.Hour)
	schedule := &Schedule{
		ID:              "one-time-schedule",
		Name:            "One Time Schedule",
		UserID:          "test-user",
		Status:          ScheduleStatusActive,
		ScheduledAt:     &past,
		NextExecutionAt: &past,
		SessionConfig: SessionConfig{
			Tags: map[string]string{"test": "true"},
		},
	}
	if err := manager.Create(ctx, schedule); err != nil {
		t.Fatalf("Create() error = %v", err)
	}

	config := WorkerConfig{
		CheckInterval: 100 * time.Millisecond,
		Enabled:       true,
	}

	worker := NewWorker(manager, sessionManager, config, nil)

	// Process schedules
	worker.processSchedules(ctx)

	// Verify schedule is marked as completed
	updated, _ := manager.Get(ctx, "one-time-schedule")
	if updated.Status != ScheduleStatusCompleted {
		t.Errorf("expected status %q, got %q", ScheduleStatusCompleted, updated.Status)
	}
}

func TestWorker_RecurringScheduleNextExecution(t *testing.T) {
	client := fake.NewSimpleClientset()
	manager := NewKubernetesManager(client, "default")
	sessionManager := newMockProxySessionManager()

	ctx := context.Background()

	// Create a recurring schedule
	now := time.Now()
	past := now.Add(-time.Hour)
	schedule := &Schedule{
		ID:              "recurring-schedule",
		Name:            "Recurring Schedule",
		UserID:          "test-user",
		Status:          ScheduleStatusActive,
		CronExpr:        "0 9 * * *", // Daily at 9am
		NextExecutionAt: &past,
		SessionConfig: SessionConfig{
			Tags: map[string]string{"test": "true"},
		},
	}
	if err := manager.Create(ctx, schedule); err != nil {
		t.Fatalf("Create() error = %v", err)
	}

	config := WorkerConfig{
		CheckInterval: 100 * time.Millisecond,
		Enabled:       true,
	}

	worker := NewWorker(manager, sessionManager, config, nil)

	// Process schedules
	worker.processSchedules(ctx)

	// Verify next execution is updated (should be in the future)
	updated, _ := manager.Get(ctx, "recurring-schedule")
	if updated.NextExecutionAt == nil {
		t.Error("expected next execution time to be set")
	}
	if !updated.NextExecutionAt.After(now) {
		t.Errorf("expected next execution after now, got %v", updated.NextExecutionAt)
	}

	// Status should still be active
	if updated.Status != ScheduleStatusActive {
		t.Errorf("expected status %q, got %q", ScheduleStatusActive, updated.Status)
	}
}

func TestWorker_DoesNotDeletePreviousInactiveSession(t *testing.T) {
	client := fake.NewSimpleClientset()
	manager := NewKubernetesManager(client, "default")
	sessionManager := newMockProxySessionManager()

	ctx := context.Background()

	// Create a previous session that is NOT active (stopped)
	prevSession := &mockProxySession{
		id:        "prev-session",
		userID:    "test-user",
		status:    "stopped", // Not active
		addr:      "localhost:9000",
		startedAt: time.Now().Add(-time.Hour),
	}
	sessionManager.sessions["prev-session"] = prevSession

	// Create a due schedule with previous execution
	now := time.Now()
	past := now.Add(-time.Hour)
	schedule := &Schedule{
		ID:              "test-schedule",
		Name:            "Test Schedule",
		UserID:          "test-user",
		Status:          ScheduleStatusActive,
		CronExpr:        "* * * * *",
		NextExecutionAt: &past,
		LastExecution: &ExecutionRecord{
			ExecutedAt: past,
			SessionID:  "prev-session",
			Status:     "success",
		},
		SessionConfig: SessionConfig{
			Tags: map[string]string{"test": "true"},
		},
	}
	if err := manager.Create(ctx, schedule); err != nil {
		t.Fatalf("Create() error = %v", err)
	}

	config := WorkerConfig{
		CheckInterval: 100 * time.Millisecond,
		Enabled:       true,
	}

	worker := NewWorker(manager, sessionManager, config, nil)

	// Process schedules
	worker.processSchedules(ctx)

	// Lifecycle cleanup is not schedule-specific; /start decides whether a live
	// route can be reused and leaves old sessions untouched.
	if _, exists := sessionManager.sessions["prev-session"]; !exists {
		t.Error("expected previous session to remain")
	}

	if len(sessionManager.sessions) != 2 {
		t.Errorf("expected previous and newly delegated sessions, got %d", len(sessionManager.sessions))
	}

	// Verify execution was recorded as success
	updated, _ := manager.Get(ctx, "test-schedule")
	if updated.LastExecution == nil {
		t.Error("expected execution to be recorded")
	}
	if updated.LastExecution.Status != "success" {
		t.Errorf("expected status 'success', got %q", updated.LastExecution.Status)
	}
}
