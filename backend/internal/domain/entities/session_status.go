package entities

// Canonical public session statuses. Runtime-only "stable" is normalized to
// active before it crosses a session-manager boundary.
const (
	SessionStatusCreating    = "creating"
	SessionStatusStarting    = "starting"
	SessionStatusActive      = "active"
	SessionStatusRunning     = "running"
	SessionStatusSuspending  = "suspending"
	SessionStatusSuspended   = "suspended"
	SessionStatusResuming    = "resuming"
	SessionStatusTerminating = "terminating"
	SessionStatusTerminated  = "terminated"
	SessionStatusStopped     = "stopped"
	SessionStatusUnhealthy   = "unhealthy"
	SessionStatusError       = "error"
	SessionStatusTimeout     = "timeout"
	SessionStatusUnknown     = "unknown"
)

// NormalizeSessionStatus maps runtime vocabulary to the public session
// vocabulary and rejects values that consumers cannot interpret safely.
func NormalizeSessionStatus(status string) (string, bool) {
	if status == "stable" {
		return SessionStatusActive, true
	}
	switch status {
	case SessionStatusCreating,
		SessionStatusStarting,
		SessionStatusActive,
		SessionStatusRunning,
		SessionStatusSuspending,
		SessionStatusSuspended,
		SessionStatusResuming,
		SessionStatusTerminating,
		SessionStatusTerminated,
		SessionStatusStopped,
		SessionStatusUnhealthy,
		SessionStatusError,
		SessionStatusTimeout,
		SessionStatusUnknown:
		return status, true
	default:
		return "", false
	}
}
