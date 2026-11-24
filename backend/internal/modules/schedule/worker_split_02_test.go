package schedule

import (
	"context"
	"testing"
	"time"

	"k8s.io/client-go/kubernetes/fake"
)

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
