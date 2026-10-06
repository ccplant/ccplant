package entities

import "time"

// SessionStatusUsageEvent is an append-only session state transition. A usage
// snapshot can be reconstructed by selecting each session's latest event at a
// point in time and aggregating it by pool and principal.
type SessionStatusUsageEvent struct {
	EventID     string    `json:"event_id"`
	OccurredAt  time.Time `json:"occurred_at"`
	SessionID   string    `json:"session_id"`
	Pool        string    `json:"pool"`
	Scope       string    `json:"scope"`
	PrincipalID string    `json:"principal_id"`
	Status      string    `json:"status"`
}

type SessionRuntimeQuery struct {
	PrincipalID string
	Pool        string
	From        time.Time
	To          time.Time
}

type SessionRuntimeSummary struct {
	RuntimeSeconds   int64 `json:"runtime_seconds"`
	RunningSeconds   int64 `json:"running_seconds"`
	SuspendedSeconds int64 `json:"suspended_seconds"`
	Sessions         int   `json:"sessions"`
	PeakConcurrent   int   `json:"peak_concurrent"`
}

type SessionRuntimeBucket struct {
	Start            time.Time `json:"start"`
	Sessions         int       `json:"sessions"`
	RuntimeSeconds   int64     `json:"runtime_seconds"`
	RunningSeconds   int64     `json:"running_seconds"`
	SuspendedSeconds int64     `json:"suspended_seconds"`
	PeakConcurrent   int       `json:"peak_concurrent"`
}

type SessionRuntimeBreakdown struct {
	SessionID        string `json:"session_id"`
	RuntimeSeconds   int64  `json:"runtime_seconds"`
	RunningSeconds   int64  `json:"running_seconds"`
	SuspendedSeconds int64  `json:"suspended_seconds"`
	CurrentStatus    string `json:"current_status"`
}

type SessionRuntimeDashboard struct {
	From              time.Time                 `json:"from"`
	To                time.Time                 `json:"to"`
	AsOf              time.Time                 `json:"as_of"`
	IsPartial         bool                      `json:"is_partial"`
	Timezone          string                    `json:"timezone"`
	CoverageStartedAt *time.Time                `json:"coverage_started_at,omitempty"`
	Summary           SessionRuntimeSummary     `json:"summary"`
	Trend             []SessionRuntimeBucket    `json:"trend"`
	BySession         []SessionRuntimeBreakdown `json:"by_session"`
	AvailablePools    []string                  `json:"available_pools"`
}
