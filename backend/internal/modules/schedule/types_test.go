package schedule

import (
	"testing"
	"time"

	"github.com/takutakahashi/agentapi-proxy/internal/domain/entities"
)

func TestSessionConfigEffectiveReuseMessage(t *testing.T) {
	tests := []struct {
		name   string
		config SessionConfig
		want   string
	}{
		{name: "dedicated reuse message", config: SessionConfig{ReuseMessage: "reuse", Params: &entities.SessionParams{Message: "initial"}}, want: "reuse"},
		{name: "initial message fallback", config: SessionConfig{Params: &entities.SessionParams{Message: "initial"}}, want: "initial"},
		{name: "empty config", config: SessionConfig{}, want: ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.config.EffectiveReuseMessage(); got != tt.want {
				t.Fatalf("EffectiveReuseMessage() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestSessionConfigSessionTags(t *testing.T) {
	executedAt := time.Date(2026, time.October, 20, 6, 7, 8, 900, time.FixedZone("JST", 9*60*60))

	t.Run("reusable schedule keeps stable tags", func(t *testing.T) {
		config := SessionConfig{Tags: map[string]string{"custom": "value"}, ReuseSession: true}
		got := config.SessionTags("schedule-1", "daily", executedAt)

		if got["schedule_id"] != "schedule-1" || got["schedule_name"] != "daily" || got["custom"] != "value" {
			t.Fatalf("SessionTags() = %#v", got)
		}
		if _, ok := got[ScheduleExecutionTimeTag]; ok {
			t.Fatalf("reusable schedule must not include %q: %#v", ScheduleExecutionTimeTag, got)
		}
	})

	t.Run("non-reusable schedule includes UTC execution time", func(t *testing.T) {
		config := SessionConfig{Tags: map[string]string{"custom": "value"}}
		got := config.SessionTags("schedule-1", "daily", executedAt)

		want := executedAt.UTC().Format(time.RFC3339Nano)
		if got[ScheduleExecutionTimeTag] != want {
			t.Fatalf("%s = %q, want %q", ScheduleExecutionTimeTag, got[ScheduleExecutionTimeTag], want)
		}
		if _, ok := config.Tags[ScheduleExecutionTimeTag]; ok {
			t.Fatal("SessionTags mutated the configured tags")
		}
	})
}

func TestSchedule_IsOneTime(t *testing.T) {
	now := time.Now()
	tests := []struct {
		name     string
		schedule Schedule
		want     bool
	}{
		{
			name: "one-time schedule with scheduled_at only",
			schedule: Schedule{
				ScheduledAt: &now,
				CronExpr:    "",
			},
			want: true,
		},
		{
			name: "recurring schedule with cron_expr only",
			schedule: Schedule{
				ScheduledAt: nil,
				CronExpr:    "0 9 * * 1-5",
			},
			want: false,
		},
		{
			name: "recurring schedule with both",
			schedule: Schedule{
				ScheduledAt: &now,
				CronExpr:    "0 9 * * 1-5",
			},
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.schedule.IsOneTime(); got != tt.want {
				t.Errorf("Schedule.IsOneTime() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestSchedule_IsRecurring(t *testing.T) {
	now := time.Now()
	tests := []struct {
		name     string
		schedule Schedule
		want     bool
	}{
		{
			name: "one-time schedule",
			schedule: Schedule{
				ScheduledAt: &now,
				CronExpr:    "",
			},
			want: false,
		},
		{
			name: "recurring schedule",
			schedule: Schedule{
				CronExpr: "0 9 * * 1-5",
			},
			want: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.schedule.IsRecurring(); got != tt.want {
				t.Errorf("Schedule.IsRecurring() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestSchedule_IsDue(t *testing.T) {
	now := time.Now()
	past := now.Add(-time.Hour)
	future := now.Add(time.Hour)

	tests := []struct {
		name     string
		schedule Schedule
		checkAt  time.Time
		want     bool
	}{
		{
			name: "schedule is due",
			schedule: Schedule{
				Status:          ScheduleStatusActive,
				NextExecutionAt: &past,
			},
			checkAt: now,
			want:    true,
		},
		{
			name: "schedule is not due yet",
			schedule: Schedule{
				Status:          ScheduleStatusActive,
				NextExecutionAt: &future,
			},
			checkAt: now,
			want:    false,
		},
		{
			name: "paused schedule is not due",
			schedule: Schedule{
				Status:          ScheduleStatusPaused,
				NextExecutionAt: &past,
			},
			checkAt: now,
			want:    false,
		},
		{
			name: "schedule with no next execution time",
			schedule: Schedule{
				Status:          ScheduleStatusActive,
				NextExecutionAt: nil,
			},
			checkAt: now,
			want:    false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.schedule.IsDue(tt.checkAt); got != tt.want {
				t.Errorf("Schedule.IsDue() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestSchedule_Validate(t *testing.T) {
	now := time.Now()

	tests := []struct {
		name     string
		schedule Schedule
		wantErr  bool
	}{
		{
			name: "valid one-time schedule",
			schedule: Schedule{
				ID:          "test-id",
				Name:        "Test Schedule",
				UserID:      "user-1",
				ScheduledAt: &now,
			},
			wantErr: false,
		},
		{
			name: "valid recurring schedule",
			schedule: Schedule{
				ID:       "test-id",
				Name:     "Test Schedule",
				UserID:   "user-1",
				CronExpr: "0 9 * * 1-5",
			},
			wantErr: false,
		},
		{
			name: "missing id",
			schedule: Schedule{
				Name:        "Test Schedule",
				UserID:      "user-1",
				ScheduledAt: &now,
			},
			wantErr: true,
		},
		{
			name: "missing name",
			schedule: Schedule{
				ID:          "test-id",
				UserID:      "user-1",
				ScheduledAt: &now,
			},
			wantErr: true,
		},
		{
			name: "missing user_id",
			schedule: Schedule{
				ID:          "test-id",
				Name:        "Test Schedule",
				ScheduledAt: &now,
			},
			wantErr: true,
		},
		{
			name: "missing both scheduled_at and cron_expr",
			schedule: Schedule{
				ID:     "test-id",
				Name:   "Test Schedule",
				UserID: "user-1",
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.schedule.Validate()
			if (err != nil) != tt.wantErr {
				t.Errorf("Schedule.Validate() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}
