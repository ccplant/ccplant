package codexauth

import (
	"context"
	"errors"
	"time"
)

var (
	ErrAttemptNotFound = errors.New("codex device auth attempt not found")
	ErrAttemptActive   = errors.New("codex device auth attempt already active")
)

const (
	StatusStarting       = "starting"
	StatusWaitingForUser = "waiting_for_user"
	StatusAuthorized     = "authorized"
	StatusDenied         = "denied"
	StatusFailed         = "failed"
	StatusCancelled      = "cancelled"
)

// WorkloadRequest contains only the information required by the short-lived
// Codex authentication worker. Token is written to a Kubernetes Secret by the
// execution-plane manager and never placed in pod labels or arguments.
type WorkloadRequest struct {
	AttemptID   string    `json:"attempt_id"`
	CallbackURL string    `json:"callback_url"`
	Token       string    `json:"token"`
	ExpiresAt   time.Time `json:"expires_at"`
}

type Challenge struct {
	UserCode        string `json:"user_code"`
	VerificationURI string `json:"verification_uri"`
	CLIVersion      string `json:"cli_version,omitempty"`
}

type Result struct {
	Status    string `json:"status"`
	AuthJSON  []byte `json:"auth_json,omitempty"`
	ErrorCode string `json:"error_code,omitempty"`
}

// Attempt is the durable control-plane record for an authentication workload.
// TokenHash contains only a SHA-256 digest; the bearer token itself exists only
// in the workload request Secret.
type Attempt struct {
	ID             string    `json:"id"`
	UserID         string    `json:"user_id"`
	CredentialName string    `json:"credential_name"`
	TokenHash      []byte    `json:"token_hash"`
	Status         string    `json:"status"`
	Challenge      Challenge `json:"challenge,omitempty"`
	ExpiresAt      time.Time `json:"expires_at"`
}

type AttemptStore interface {
	Create(context.Context, *Attempt) error
	Get(context.Context, string) (*Attempt, error)
	Update(context.Context, *Attempt) error
	ActiveByCredential(context.Context, string) (*Attempt, error)
	LatestByUser(context.Context, string) (*Attempt, error)
	Release(context.Context, *Attempt) error
}

// WorkloadLauncher is implemented by local Kubernetes managers and by the
// private session-manager API client used by an isolated parent API process.
type WorkloadLauncher interface {
	StartCodexDeviceAuth(context.Context, WorkloadRequest) error
	CancelCodexDeviceAuth(context.Context, string) error
}
