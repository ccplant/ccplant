package entities

import "time"

// SessionCountDimension identifies one principal's use of a session pool.
type SessionCountDimension struct {
	Pool        string `json:"pool"`
	PrincipalID string `json:"principal_id"`
}

// SessionCountSample is a point-in-time count of active pool allocations.
type SessionCountSample struct {
	SessionCountDimension
	SampledAt    time.Time `json:"sampled_at"`
	ActiveCount  int       `json:"active_count"`
	RunningCount int       `json:"running_count"`
}
