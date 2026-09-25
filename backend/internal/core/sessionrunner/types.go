package sessionrunner

import "time"

const (
	CapabilityRunnerClaimV1     = "runner_claim_v1"
	CapabilityDirectRuntimeV1   = "direct_session_runtime_v1"
	CapabilityCodexDeviceAuthV1 = "codex_device_auth_v1"
)

type ManagerScope string

const (
	ManagerScopeUser   ManagerScope = "user"
	ManagerScopeTeam   ManagerScope = "team"
	ManagerScopeSystem ManagerScope = "system"
)

type SubjectType string
type BindingRole string

const (
	SubjectUser             SubjectType = "user"
	SubjectTeam             SubjectType = "team"
	SubjectAll              SubjectType = "all"
	BindingRoleUse          BindingRole = "use"
	BindingRoleManage       BindingRole = "manage"
	BindingRoleManageAndUse BindingRole = "manage_and_use"
)

func (r BindingRole) GrantsUse() bool {
	return r == BindingRoleUse || r == BindingRoleManageAndUse
}

func (r BindingRole) GrantsManage() bool {
	return r == BindingRoleManage || r == BindingRoleManageAndUse
}

type Manager struct {
	ID                    string            `json:"id"`
	Name                  string            `json:"name"`
	Scope                 ManagerScope      `json:"scope"`
	OwnerID               string            `json:"owner_id,omitempty"`
	InstallPool           string            `json:"install_pool,omitempty"`
	Default               bool              `json:"default,omitempty"`
	ConnectionTokenHash   string            `json:"connection_token_hash,omitempty"`
	RegistrationTokenHash string            `json:"registration_token_hash,omitempty"`
	RegistrationExpiresAt time.Time         `json:"registration_expires_at,omitempty"`
	Labels                map[string]string `json:"labels,omitempty"`
	Capabilities          []string          `json:"capabilities,omitempty"`
	Enabled               bool              `json:"enabled"`
	Draining              bool              `json:"draining,omitempty"`
	LastHeartbeatAt       time.Time         `json:"last_heartbeat_at,omitempty"`
	CreatedAt             time.Time         `json:"created_at"`
	UpdatedAt             time.Time         `json:"updated_at"`
}

type LogicalPool struct {
	Name      string            `json:"name"`
	Labels    map[string]string `json:"labels,omitempty"`
	Enabled   bool              `json:"enabled"`
	CreatedAt time.Time         `json:"created_at"`
	UpdatedAt time.Time         `json:"updated_at"`
}

// PoolSupplier describes a manager's capacity for a logical pool.
type PoolSupplier struct {
	Pool         string            `json:"pool"`
	ManagerID    string            `json:"manager_id"`
	Labels       map[string]string `json:"labels,omitempty"`
	MinIdle      int               `json:"min_idle,omitempty"`
	MaxRunners   int               `json:"max_runners,omitempty"`
	Enabled      bool              `json:"enabled"`
	Draining     bool              `json:"draining,omitempty"`
	CreatedAt    time.Time         `json:"created_at"`
	UpdatedAt    time.Time         `json:"updated_at"`
	IdleRunners  int               `json:"idle_runners,omitempty"`
	TotalRunners int               `json:"total_runners,omitempty"`
}

type Binding struct {
	ID          string      `json:"id"`
	Pool        string      `json:"pool"`
	SubjectType SubjectType `json:"subject_type"`
	SubjectID   string      `json:"subject_id"`
	Role        BindingRole `json:"role"`
	// Roles is the multi-value representation of the permissions granted by the
	// binding. Role is retained for persisted-data and API compatibility.
	Roles   []BindingRole `json:"roles,omitempty"`
	Enabled bool          `json:"enabled"`
	// ExplicitOnly makes the pool available for explicit selection without
	// allowing the resolver to choose it for requests that omit params.pool.
	ExplicitOnly  bool      `json:"explicit_only,omitempty"`
	Priority      int       `json:"priority,omitempty"`
	MaxConcurrent int       `json:"max_concurrent,omitempty"`
	CreatedAt     time.Time `json:"created_at"`
	UpdatedAt     time.Time `json:"updated_at"`
}

// NormalizeRoles keeps the legacy scalar role and the multi-value roles in
// sync. New callers may specify use and manage independently, while old
// records and clients continue to use manage_and_use.
func (b *Binding) NormalizeRoles() {
	if len(b.Roles) == 0 {
		switch b.Role {
		case BindingRoleManageAndUse:
			b.Roles = []BindingRole{BindingRoleManage, BindingRoleUse}
		case BindingRoleManage, BindingRoleUse:
			b.Roles = []BindingRole{b.Role}
		}
		return
	}
	hasUse, hasManage := false, false
	for _, role := range b.Roles {
		hasUse = hasUse || role == BindingRoleUse
		hasManage = hasManage || role == BindingRoleManage
	}
	// A scalar role changed by a legacy caller wins when it disagrees with a
	// previously hydrated Roles slice. New multi-role requests clear Role before
	// normalization, so their array remains authoritative.
	if b.Role == BindingRoleUse || b.Role == BindingRoleManage || b.Role == BindingRoleManageAndUse {
		if b.Role.GrantsUse() != hasUse || b.Role.GrantsManage() != hasManage {
			b.Roles = nil
			b.NormalizeRoles()
			return
		}
	}
	switch {
	case hasUse && hasManage:
		b.Role = BindingRoleManageAndUse
	case hasManage:
		b.Role = BindingRoleManage
	case hasUse:
		b.Role = BindingRoleUse
	default:
		b.Role = ""
	}
}

func (b *Binding) GrantsUse() bool {
	b.NormalizeRoles()
	return b.Role.GrantsUse()
}

func (b *Binding) GrantsManage() bool {
	b.NormalizeRoles()
	return b.Role.GrantsManage()
}

type Subject struct {
	Type SubjectType `json:"type"`
	ID   string      `json:"id"`
}

type ResolvedPool struct {
	Pool    *LogicalPool `json:"pool"`
	Binding *Binding     `json:"binding"`
}

type RunnerStatus string

const (
	RunnerIdle     RunnerStatus = "idle"
	RunnerClaiming RunnerStatus = "claiming"
	RunnerRunning  RunnerStatus = "running"
	RunnerOffline  RunnerStatus = "offline"
	RunnerDraining RunnerStatus = "draining"
)

type Runner struct {
	ID        string `json:"id"`
	ManagerID string `json:"manager_id"`
	Pool      string `json:"pool"`
	// Capabilities describes workload features prepared before this runner was
	// registered. Allocations are only claimed by runners with matching values.
	Capabilities map[string]string `json:"capabilities,omitempty"`
	TokenHash    string            `json:"token_hash,omitempty"`
	Status       RunnerStatus      `json:"status"`
	PodName      string            `json:"pod_name,omitempty"`
	Namespace    string            `json:"namespace,omitempty"`
	CreatedAt    time.Time         `json:"created_at"`
	UpdatedAt    time.Time         `json:"updated_at"`
	LastSeen     time.Time         `json:"last_seen,omitempty"`
}

type AllocationStatus string

const (
	AllocationPending   AllocationStatus = "pending"
	AllocationLeased    AllocationStatus = "leased"
	AllocationClaimed   AllocationStatus = "claimed"
	AllocationRunning   AllocationStatus = "running"
	AllocationCompleted AllocationStatus = "completed"
	AllocationFailed    AllocationStatus = "failed"
)

type Allocation struct {
	SessionID         string            `json:"session_id"`
	Pool              string            `json:"pool"`
	BindingID         string            `json:"binding_id,omitempty"`
	ManagerID         string            `json:"manager_id,omitempty"`
	RunnerID          string            `json:"runner_id,omitempty"`
	Status            AllocationStatus  `json:"status"`
	LeaseID           string            `json:"lease_id,omitempty"`
	LeaseExpiresAt    time.Time         `json:"lease_expires_at,omitempty"`
	Generation        int64             `json:"generation"`
	Attempts          int               `json:"attempts"`
	Requirements      map[string]string `json:"requirements,omitempty"`
	RuntimeToken      string            `json:"runtime_token,omitempty"`
	RuntimeTokenHash  string            `json:"runtime_token_hash,omitempty"`
	ProvisionSettings []byte            `json:"provision_settings,omitempty"`
	CreatedAt         time.Time         `json:"created_at"`
	UpdatedAt         time.Time         `json:"updated_at"`
}

type QuotaExceededError struct {
	Pool          string
	BindingID     string
	MaxConcurrent int
	Active        int
}

func (e *QuotaExceededError) Error() string {
	return "session pool quota exceeded"
}

type Claim struct {
	Allocation *Allocation `json:"allocation"`
	Runner     *Runner     `json:"runner"`
}

// Configuration preserves user input separately from resolved profile defaults.
// Secret-bearing Input and Settings are never returned by the public status API.
type Configuration struct {
	TriggeredUserID string    `json:"triggered_user_id,omitempty"`
	RequestHash     string    `json:"request_hash,omitempty"`
	SessionID       string    `json:"session_id"`
	UserID          string    `json:"user_id"`
	Scope           string    `json:"scope"`
	TeamID          string    `json:"team_id,omitempty"`
	Teams           []string  `json:"teams,omitempty"`
	Input           []byte    `json:"input"`
	ProfileID       string    `json:"profile_id,omitempty"`
	Settings        []byte    `json:"settings,omitempty"`
	Revision        int64     `json:"revision"`
	Phase           string    `json:"phase,omitempty"`
	RequestID       string    `json:"request_id,omitempty"`
	Error           string    `json:"error,omitempty"`
	UpdatedAt       time.Time `json:"updated_at"`
	Version         int64     `json:"-"`
}
