package schedule

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"

	"github.com/stretchr/testify/require"
	"github.com/takutakahashi/agentapi-proxy/internal/core/kubelabel"
	"github.com/takutakahashi/agentapi-proxy/internal/domain/entities"
)

func TestKubernetesManager_RecordExecutionBoundsHistory(t *testing.T) {
	ctx := context.Background()
	manager := NewKubernetesManager(fake.NewSimpleClientset(), "default")
	now := time.Now()
	require.NoError(t, manager.Create(ctx, &Schedule{
		ID: "bounded-history", Name: "Bounded History", UserID: "user-1",
		Status: ScheduleStatusActive, ScheduledAt: &now,
	}))

	for i := 0; i < MaxExecutionHistory+3; i++ {
		require.NoError(t, manager.RecordExecution(ctx, "bounded-history", ExecutionRecord{
			ExecutedAt: now.Add(time.Duration(i) * time.Minute),
			SessionID:  fmt.Sprintf("session-%d", i),
			Status:     "success",
		}))
	}

	got, err := manager.Get(ctx, "bounded-history")
	require.NoError(t, err)
	require.Len(t, got.ExecutionHistory, MaxExecutionHistory)
	require.Equal(t, "session-3", got.ExecutionHistory[0].SessionID)
	require.Equal(t, fmt.Sprintf("session-%d", MaxExecutionHistory+2), got.ExecutionHistory[MaxExecutionHistory-1].SessionID)
	require.Equal(t, MaxExecutionHistory+3, got.ExecutionCount)
}

func TestKubernetesManager_UpdateNextExecution(t *testing.T) {
	ctx := context.Background()
	client := fake.NewSimpleClientset()
	manager := NewKubernetesManager(client, "default")

	now := time.Now()
	future := now.Add(time.Hour)

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

	if err := manager.UpdateNextExecution(ctx, "test-schedule", future); err != nil {
		t.Fatalf("UpdateNextExecution() error = %v", err)
	}

	got, err := manager.Get(ctx, "test-schedule")
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}

	if got.NextExecutionAt == nil {
		t.Fatal("NextExecutionAt should not be nil")
	}
	if !got.NextExecutionAt.Equal(future) {
		t.Errorf("got NextExecutionAt = %v, want %v", *got.NextExecutionAt, future)
	}
}

func TestKubernetesManager_ListWithScopeAndTeam(t *testing.T) {
	ctx := context.Background()
	client := fake.NewSimpleClientset()
	manager := NewKubernetesManager(client, "default")

	now := time.Now()

	// Create schedules with different scopes
	schedules := []*Schedule{
		{
			ID:          "user-schedule-1",
			Name:        "User Schedule 1",
			UserID:      "user-1",
			Scope:       entities.ScopeUser,
			Status:      ScheduleStatusActive,
			ScheduledAt: &now,
		},
		{
			ID:          "team-schedule-1",
			Name:        "Team Schedule 1",
			UserID:      "user-1",
			Scope:       entities.ScopeTeam,
			TeamID:      "org/team-a",
			Status:      ScheduleStatusActive,
			ScheduledAt: &now,
		},
		{
			ID:          "team-schedule-2",
			Name:        "Team Schedule 2",
			UserID:      "user-2",
			Scope:       entities.ScopeTeam,
			TeamID:      "org/team-b",
			Status:      ScheduleStatusActive,
			ScheduledAt: &now,
		},
	}

	for _, s := range schedules {
		if err := manager.Create(ctx, s); err != nil {
			t.Fatalf("Create() error = %v", err)
		}
	}

	// List by scope (user)
	userScoped, err := manager.List(ctx, ScheduleFilter{Scope: entities.ScopeUser})
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	if len(userScoped) != 1 {
		t.Errorf("List(scope=user) got %d schedules, want 1", len(userScoped))
	}

	// List by scope (team)
	teamScoped, err := manager.List(ctx, ScheduleFilter{Scope: entities.ScopeTeam})
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	if len(teamScoped) != 2 {
		t.Errorf("List(scope=team) got %d schedules, want 2", len(teamScoped))
	}

	// List by team ID
	teamA, err := manager.List(ctx, ScheduleFilter{TeamID: "org/team-a"})
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	if len(teamA) != 1 {
		t.Errorf("List(team_id=org/team-a) got %d schedules, want 1", len(teamA))
	}

	// List by multiple team IDs
	multiTeam, err := manager.List(ctx, ScheduleFilter{
		Scope:   entities.ScopeTeam,
		TeamIDs: []string{"org/team-a", "org/team-b"},
	})
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	if len(multiTeam) != 2 {
		t.Errorf("List(team_ids=[team-a,team-b]) got %d schedules, want 2", len(multiTeam))
	}
}

func TestKubernetesManager_MigrateFromLegacy(t *testing.T) {
	ctx := context.Background()

	now := time.Now()
	legacySchedules := schedulesData{
		Schedules: []*Schedule{
			{
				ID:          "legacy-schedule-1",
				Name:        "Legacy Schedule 1",
				UserID:      "user-1",
				Scope:       entities.ScopeUser,
				Status:      ScheduleStatusActive,
				ScheduledAt: &now,
				CreatedAt:   now,
				UpdatedAt:   now,
			},
			{
				ID:          "legacy-schedule-2",
				Name:        "Legacy Schedule 2",
				UserID:      "user-2",
				Scope:       entities.ScopeTeam,
				TeamID:      "org/team-x",
				Status:      ScheduleStatusActive,
				ScheduledAt: &now,
				CreatedAt:   now,
				UpdatedAt:   now,
			},
		},
	}

	legacyData, _ := json.Marshal(legacySchedules)

	// Create fake client with legacy secret
	legacySecret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      LegacyScheduleSecretName,
			Namespace: "default",
			Labels: map[string]string{
				LabelSchedule: "true",
			},
		},
		Data: map[string][]byte{
			LegacySecretKeySchedules: legacyData,
		},
	}

	client := fake.NewSimpleClientset(legacySecret)
	manager := NewKubernetesManager(client, "default")

	// Run migration
	if err := manager.MigrateFromLegacy(ctx); err != nil {
		t.Fatalf("MigrateFromLegacy() error = %v", err)
	}

	// Verify schedules were migrated
	schedule1, err := manager.Get(ctx, "legacy-schedule-1")
	if err != nil {
		t.Fatalf("Get(legacy-schedule-1) error = %v", err)
	}
	if schedule1.Name != "Legacy Schedule 1" {
		t.Errorf("got Name = %v, want 'Legacy Schedule 1'", schedule1.Name)
	}

	schedule2, err := manager.Get(ctx, "legacy-schedule-2")
	if err != nil {
		t.Fatalf("Get(legacy-schedule-2) error = %v", err)
	}
	if schedule2.TeamID != "org/team-x" {
		t.Errorf("got TeamID = %v, want 'org/team-x'", schedule2.TeamID)
	}

	// Verify all schedules are listed
	all, err := manager.List(ctx, ScheduleFilter{})
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	if len(all) != 2 {
		t.Errorf("List() got %d schedules, want 2", len(all))
	}

	// Verify labels are set correctly on individual secrets
	secret1, err := client.CoreV1().Secrets("default").Get(ctx, scheduleSecretName("legacy-schedule-1"), metav1.GetOptions{})
	if err != nil {
		t.Fatalf("Get secret error = %v", err)
	}
	if secret1.Labels[LabelScheduleScope] != string(entities.ScopeUser) {
		t.Errorf("got scope label = %v, want 'user'", secret1.Labels[LabelScheduleScope])
	}

	secret2, err := client.CoreV1().Secrets("default").Get(ctx, scheduleSecretName("legacy-schedule-2"), metav1.GetOptions{})
	if err != nil {
		t.Fatalf("Get secret error = %v", err)
	}
	expectedTeamIDHash := kubelabel.HashValue("org/team-x")
	if secret2.Labels[LabelScheduleTeamID] != expectedTeamIDHash {
		t.Errorf("got team_id label = %v, want '%s'", secret2.Labels[LabelScheduleTeamID], expectedTeamIDHash)
	}
}

func TestKubernetesManager_MigrateFromLegacy_Idempotent(t *testing.T) {
	ctx := context.Background()

	now := time.Now()
	legacySchedules := schedulesData{
		Schedules: []*Schedule{
			{
				ID:          "schedule-to-migrate",
				Name:        "Original Name",
				UserID:      "user-1",
				Status:      ScheduleStatusActive,
				ScheduledAt: &now,
				CreatedAt:   now,
				UpdatedAt:   now,
			},
		},
	}

	legacyData, _ := json.Marshal(legacySchedules)

	// Create legacy secret
	legacySecret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      LegacyScheduleSecretName,
			Namespace: "default",
			Labels: map[string]string{
				LabelSchedule: "true",
			},
		},
		Data: map[string][]byte{
			LegacySecretKeySchedules: legacyData,
		},
	}

	// Also create an already-migrated schedule with updated name
	existingSchedule := &Schedule{
		ID:          "schedule-to-migrate",
		Name:        "Already Migrated Name",
		UserID:      "user-1",
		Status:      ScheduleStatusActive,
		ScheduledAt: &now,
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	existingData, _ := json.Marshal(existingSchedule)

	existingSecret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      scheduleSecretName("schedule-to-migrate"),
			Namespace: "default",
			Labels: map[string]string{
				LabelSchedule:       "true",
				LabelScheduleID:     "schedule-to-migrate",
				LabelScheduleScope:  string(entities.ScopeUser),
				LabelScheduleUserID: kubelabel.HashValue("user-1"),
			},
		},
		Data: map[string][]byte{
			SecretKeySchedule: existingData,
		},
	}

	client := fake.NewSimpleClientset(legacySecret, existingSecret)
	manager := NewKubernetesManager(client, "default")

	// Run migration (should skip already existing schedule)
	if err := manager.MigrateFromLegacy(ctx); err != nil {
		t.Fatalf("MigrateFromLegacy() error = %v", err)
	}

	// Verify the existing schedule was NOT overwritten
	schedule, err := manager.Get(ctx, "schedule-to-migrate")
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if schedule.Name != "Already Migrated Name" {
		t.Errorf("got Name = %v, want 'Already Migrated Name' (should not be overwritten)", schedule.Name)
	}
}

func TestKubernetesManager_MigrateFromLegacy_NoLegacySecret(t *testing.T) {
	ctx := context.Background()

	client := fake.NewSimpleClientset()
	manager := NewKubernetesManager(client, "default")

	// Run migration with no legacy secret (should not error)
	if err := manager.MigrateFromLegacy(ctx); err != nil {
		t.Fatalf("MigrateFromLegacy() error = %v, expected nil", err)
	}
}
