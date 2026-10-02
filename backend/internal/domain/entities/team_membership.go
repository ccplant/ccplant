package entities

import "time"

// ExternalTeamMember is a GitHub user observed while synchronizing a bound
// GitHub team. GitHubUserID is only unique within ConnectionID.
type ExternalTeamMember struct {
	ConnectionID string            `json:"connection_id"`
	GitHubUserID int64             `json:"github_user_id"`
	Login        string            `json:"login"`
	Sources      []ExternalTeamRef `json:"sources"`
}

// ExternalTeamRef records which external team granted membership.
type ExternalTeamRef struct {
	ConnectionID string `json:"connection_id"`
	Organization string `json:"organization"`
	TeamSlug     string `json:"team_slug"`
}

// TeamMember is the authorization-ready view of an external member.
type TeamMember struct {
	PrincipalID string                `json:"principal_id"`
	Login       string                `json:"login"`
	Sources     []ExternalIdentityRef `json:"sources"`
}

// ExternalIdentityRef identifies a linked GitHub identity.
type ExternalIdentityRef struct {
	ConnectionID string `json:"connection_id"`
	GitHubUserID int64  `json:"github_user_id"`
}

// TeamMembershipSnapshot is the durable membership state for one ccplant team.
type TeamMembershipSnapshot struct {
	SchemaVersion   int                  `json:"schema_version"`
	TeamPrincipalID string               `json:"team_principal_id"`
	Generation      int64                `json:"generation"`
	ExternalMembers []ExternalTeamMember `json:"external_members"`
	Members         []TeamMember         `json:"members"`
	SyncedAt        time.Time            `json:"synced_at"`
	SyncedBy        string               `json:"synced_by"`
	SyncReason      string               `json:"sync_reason"`
	LastStartedAt   time.Time            `json:"last_started_at"`
	LeaseUntil      time.Time            `json:"lease_until"`
	OperationID     string               `json:"operation_id,omitempty"`
}
