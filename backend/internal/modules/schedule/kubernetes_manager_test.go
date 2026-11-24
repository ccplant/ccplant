package schedule

import (
	"context"
	"testing"
	"time"

	"k8s.io/client-go/kubernetes/fake"
)

func TestKubernetesManager_Create(t *testing.T) {
	ctx := context.Background()
	client := fake.NewSimpleClientset()
	manager := NewKubernetesManager(client, "default")

	now := time.Now()
	schedule := &Schedule{
		ID:          "test-schedule-1",
		Name:        "Test Schedule",
		UserID:      "user-1",
		Status:      ScheduleStatusActive,
		ScheduledAt: &now,
		SessionConfig: SessionConfig{
			Tags: map[string]string{"repository": "org/repo"},
		},
	}

	// Create schedule
	err := manager.Create(ctx, schedule)
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}

	// Verify it was created
	got, err := manager.Get(ctx, "test-schedule-1")
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}

	if got.ID != schedule.ID {
		t.Errorf("got ID = %v, want %v", got.ID, schedule.ID)
	}
	if got.Name != schedule.Name {
		t.Errorf("got Name = %v, want %v", got.Name, schedule.Name)
	}

	// Try to create duplicate
	err = manager.Create(ctx, schedule)
	if err == nil {
		t.Error("Create() should fail for duplicate ID")
	}
}

func TestKubernetesManager_Get(t *testing.T) {
	ctx := context.Background()
	client := fake.NewSimpleClientset()
	manager := NewKubernetesManager(client, "default")

	// Get non-existent schedule
	_, err := manager.Get(ctx, "non-existent")
	if err == nil {
		t.Error("Get() should fail for non-existent schedule")
	}
	if _, ok := err.(ErrScheduleNotFound); !ok {
		t.Errorf("expected ErrScheduleNotFound, got %T", err)
	}
}

func TestKubernetesManagerClaimAndFinalizeAreIdempotent(t *testing.T) {
	ctx := context.Background()
	manager := NewKubernetesManager(fake.NewSimpleClientset(), "default")
	now := time.Now().Add(-time.Minute)
	item := &Schedule{ID: "claimed", Name: "claimed", UserID: "alice", Status: ScheduleStatusActive, ScheduledAt: &now, NextExecutionAt: &now}
	if err := manager.Create(ctx, item); err != nil {
		t.Fatal(err)
	}
	claimed, err := manager.ClaimDueSchedules(ctx, time.Now(), time.Minute)
	if err != nil || len(claimed) != 1 {
		t.Fatalf("claimed=%d err=%v", len(claimed), err)
	}
	if again, err := manager.ClaimDueSchedules(ctx, time.Now(), time.Minute); err != nil || len(again) != 0 {
		t.Fatalf("second claim=%d err=%v", len(again), err)
	}
	pending := claimed[0].PendingExecution
	expired, err := manager.ClaimDueSchedules(ctx, pending.ExpiresAt.Add(time.Second), time.Minute)
	if err != nil || len(expired) != 1 {
		t.Fatalf("expired claim=%d err=%v", len(expired), err)
	}
	if expired[0].PendingExecution.ExecutionID != pending.ExecutionID || expired[0].PendingExecution.SessionID != pending.SessionID {
		t.Fatal("expired claim changed idempotency IDs")
	}
	record := ExecutionRecord{ExecutedAt: time.Now(), SessionID: pending.SessionID, Status: "success"}
	if err := manager.FinalizeClaim(ctx, item.ID, pending.ExecutionID, record, nil, true); err != nil {
		t.Fatal(err)
	}
	if err := manager.FinalizeClaim(ctx, item.ID, pending.ExecutionID, record, nil, true); err != nil {
		t.Fatal(err)
	}
	got, err := manager.Get(ctx, item.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.ExecutionCount != 1 || got.Status != ScheduleStatusCompleted || got.PendingExecution != nil {
		t.Fatalf("got=%#v", got)
	}
}

func TestKubernetesManager_List(t *testing.T) {
	ctx := context.Background()
	client := fake.NewSimpleClientset()
	manager := NewKubernetesManager(client, "default")

	now := time.Now()

	// Create multiple schedules
	schedules := []*Schedule{
		{
			ID:          "schedule-1",
			Name:        "Schedule 1",
			UserID:      "user-1",
			Status:      ScheduleStatusActive,
			ScheduledAt: &now,
		},
		{
			ID:          "schedule-2",
			Name:        "Schedule 2",
			UserID:      "user-2",
			Status:      ScheduleStatusActive,
			ScheduledAt: &now,
		},
		{
			ID:          "schedule-3",
			Name:        "Schedule 3",
			UserID:      "user-1",
			Status:      ScheduleStatusPaused,
			ScheduledAt: &now,
		},
	}

	for _, s := range schedules {
		if err := manager.Create(ctx, s); err != nil {
			t.Fatalf("Create() error = %v", err)
		}
	}

	// List all
	all, err := manager.List(ctx, ScheduleFilter{})
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	if len(all) != 3 {
		t.Errorf("List() got %d schedules, want 3", len(all))
	}

	// List by user
	user1, err := manager.List(ctx, ScheduleFilter{UserID: "user-1"})
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	if len(user1) != 2 {
		t.Errorf("List(user-1) got %d schedules, want 2", len(user1))
	}

	// List by status
	active, err := manager.List(ctx, ScheduleFilter{Status: ScheduleStatusActive})
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	if len(active) != 2 {
		t.Errorf("List(active) got %d schedules, want 2", len(active))
	}
}

func TestKubernetesManager_Update(t *testing.T) {
	ctx := context.Background()
	client := fake.NewSimpleClientset()
	manager := NewKubernetesManager(client, "default")

	now := time.Now()
	schedule := &Schedule{
		ID:          "test-schedule",
		Name:        "Test Schedule",
		UserID:      "user-1",
		Status:      ScheduleStatusActive,
		ScheduledAt: &now,
	}

	// Create schedule
	if err := manager.Create(ctx, schedule); err != nil {
		t.Fatalf("Create() error = %v", err)
	}

	// Update schedule
	schedule.Name = "Updated Schedule"
	schedule.Status = ScheduleStatusPaused
	if err := manager.Update(ctx, schedule); err != nil {
		t.Fatalf("Update() error = %v", err)
	}

	// Verify update
	got, err := manager.Get(ctx, "test-schedule")
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if got.Name != "Updated Schedule" {
		t.Errorf("got Name = %v, want 'Updated Schedule'", got.Name)
	}
	if got.Status != ScheduleStatusPaused {
		t.Errorf("got Status = %v, want %v", got.Status, ScheduleStatusPaused)
	}
}

func TestKubernetesManager_Delete(t *testing.T) {
	ctx := context.Background()
	client := fake.NewSimpleClientset()
	manager := NewKubernetesManager(client, "default")

	now := time.Now()
	schedule := &Schedule{
		ID:          "test-schedule",
		Name:        "Test Schedule",
		UserID:      "user-1",
		Status:      ScheduleStatusActive,
		ScheduledAt: &now,
	}

	// Create schedule
	if err := manager.Create(ctx, schedule); err != nil {
		t.Fatalf("Create() error = %v", err)
	}

	// Delete schedule
	if err := manager.Delete(ctx, "test-schedule"); err != nil {
		t.Fatalf("Delete() error = %v", err)
	}

	// Verify deletion
	_, err := manager.Get(ctx, "test-schedule")
	if err == nil {
		t.Error("Get() should fail after deletion")
	}

	// Delete non-existent
	err = manager.Delete(ctx, "non-existent")
	if err == nil {
		t.Error("Delete() should fail for non-existent schedule")
	}
}

func TestKubernetesManager_GetDueSchedules(t *testing.T) {
	ctx := context.Background()
	client := fake.NewSimpleClientset()
	manager := NewKubernetesManager(client, "default")

	now := time.Now()
	past := now.Add(-time.Hour)
	future := now.Add(time.Hour)

	schedules := []*Schedule{
		{
			ID:              "due-schedule",
			Name:            "Due Schedule",
			UserID:          "user-1",
			Status:          ScheduleStatusActive,
			ScheduledAt:     &past,
			NextExecutionAt: &past,
		},
		{
			ID:              "future-schedule",
			Name:            "Future Schedule",
			UserID:          "user-1",
			Status:          ScheduleStatusActive,
			ScheduledAt:     &future,
			NextExecutionAt: &future,
		},
		{
			ID:              "paused-schedule",
			Name:            "Paused Schedule",
			UserID:          "user-1",
			Status:          ScheduleStatusPaused,
			ScheduledAt:     &past,
			NextExecutionAt: &past,
		},
	}

	for _, s := range schedules {
		if err := manager.Create(ctx, s); err != nil {
			t.Fatalf("Create() error = %v", err)
		}
	}

	due, err := manager.GetDueSchedules(ctx, now)
	if err != nil {
		t.Fatalf("GetDueSchedules() error = %v", err)
	}

	if len(due) != 1 {
		t.Errorf("GetDueSchedules() got %d schedules, want 1", len(due))
	}

	if len(due) > 0 && due[0].ID != "due-schedule" {
		t.Errorf("got schedule ID = %v, want 'due-schedule'", due[0].ID)
	}
}

func TestKubernetesManager_RecordExecution(t *testing.T) {
	ctx := context.Background()
	client := fake.NewSimpleClientset()
	manager := NewKubernetesManager(client, "default")

	now := time.Now()
	schedule := &Schedule{
		ID:          "test-schedule",
		Name:        "Test Schedule",
		UserID:      "user-1",
		Status:      ScheduleStatusActive,
		ScheduledAt: &now,
	}

	if err := manager.Create(ctx, schedule); err != nil {
		t.Fatalf("Create() error = %v", err)
	}

	record := ExecutionRecord{
		ExecutedAt: now,
		SessionID:  "session-123",
		Status:     "success",
	}

	if err := manager.RecordExecution(ctx, "test-schedule", record); err != nil {
		t.Fatalf("RecordExecution() error = %v", err)
	}

	got, err := manager.Get(ctx, "test-schedule")
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}

	if got.LastExecution == nil {
		t.Fatal("LastExecution should not be nil")
	}
	if got.LastExecution.SessionID != "session-123" {
		t.Errorf("got SessionID = %v, want 'session-123'", got.LastExecution.SessionID)
	}
	if got.ExecutionCount != 1 {
		t.Errorf("got ExecutionCount = %d, want 1", got.ExecutionCount)
	}
	if len(got.ExecutionHistory) != 1 || got.ExecutionHistory[0].SessionID != "session-123" {
		t.Fatalf("got ExecutionHistory = %#v, want recorded execution", got.ExecutionHistory)
	}
}
