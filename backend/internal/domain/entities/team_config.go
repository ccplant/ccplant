package entities

import (
	"crypto/rand"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/oklog/ulid/v2"
)

const teamPrincipalPrefix = "team-"

// ExternalTeamBinding maps a team from one GitHub connection into this
// ccplant team. ManagedBy is either "discovery" or "api".
type ExternalTeamBinding struct {
	ConnectionID string `json:"connection_id"`
	Organization string `json:"organization"`
	TeamSlug     string `json:"team_slug"`
	ManagedBy    string `json:"managed_by"`
}

// TeamConfig represents a team configuration domain entity
type TeamConfig struct {
	teamID         string
	principalID    string
	name           string
	ownerIDs       []string
	externalTeams  []ExternalTeamBinding
	serviceAccount *ServiceAccount
	envVars        map[string]string
}

// NewTeamPrincipalID creates a URL-safe, typed ULID principal ID.
func NewTeamPrincipalID() (string, error) {
	id, err := ulid.New(ulid.Timestamp(time.Now().UTC()), rand.Reader)
	if err != nil {
		return "", fmt.Errorf("generate team principal ULID: %w", err)
	}
	return teamPrincipalPrefix + id.String(), nil
}

// NewTeamConfig creates a new team configuration
func NewTeamConfig(teamID string, serviceAccount *ServiceAccount, envVars map[string]string) *TeamConfig {
	if envVars == nil {
		envVars = make(map[string]string)
	}
	return &TeamConfig{
		teamID:         teamID,
		serviceAccount: serviceAccount,
		envVars:        envVars,
	}
}

// TeamID returns the team ID
func (tc *TeamConfig) TeamID() string {
	return tc.teamID
}

// PrincipalID returns the stable team principal ID. It is empty for a legacy
// TeamConfig until the repository adopts it.
func (tc *TeamConfig) PrincipalID() string { return tc.principalID }

// SetPrincipalID assigns the principal during legacy adoption.
func (tc *TeamConfig) SetPrincipalID(id string) { tc.principalID = id }

// Name returns the human-readable team name. Legacy teams use their team ID.
func (tc *TeamConfig) Name() string {
	if tc.name == "" {
		return tc.teamID
	}
	return tc.name
}

func (tc *TeamConfig) SetName(name string) { tc.name = name }

func (tc *TeamConfig) OwnerIDs() []string { return append([]string(nil), tc.ownerIDs...) }

func (tc *TeamConfig) SetOwnerIDs(ownerIDs []string) {
	tc.ownerIDs = append([]string(nil), ownerIDs...)
}

func (tc *TeamConfig) IsOwner(principalID string) bool {
	for _, ownerID := range tc.ownerIDs {
		if ownerID == principalID {
			return true
		}
	}
	return false
}

// ExternalTeams returns a defensive copy of external GitHub team bindings.
func (tc *TeamConfig) ExternalTeams() []ExternalTeamBinding {
	return append([]ExternalTeamBinding(nil), tc.externalTeams...)
}

// SetExternalTeams replaces external GitHub team bindings.
func (tc *TeamConfig) SetExternalTeams(bindings []ExternalTeamBinding) {
	tc.externalTeams = append([]ExternalTeamBinding(nil), bindings...)
}

// ServiceAccount returns the service account
func (tc *TeamConfig) ServiceAccount() *ServiceAccount {
	return tc.serviceAccount
}

// EnvVars returns the environment variables
func (tc *TeamConfig) EnvVars() map[string]string {
	return tc.envVars
}

// SetServiceAccount sets the service account
func (tc *TeamConfig) SetServiceAccount(sa *ServiceAccount) {
	tc.serviceAccount = sa
}

// SetEnvVars sets the environment variables
func (tc *TeamConfig) SetEnvVars(envVars map[string]string) {
	tc.envVars = envVars
}

// AddEnvVar adds an environment variable
func (tc *TeamConfig) AddEnvVar(key, value string) {
	if tc.envVars == nil {
		tc.envVars = make(map[string]string)
	}
	tc.envVars[key] = value
}

// RemoveEnvVar removes an environment variable
func (tc *TeamConfig) RemoveEnvVar(key string) {
	delete(tc.envVars, key)
}

// Validate validates the team configuration
func (tc *TeamConfig) Validate() error {
	if tc.teamID == "" {
		return errors.New("team ID cannot be empty")
	}
	if tc.principalID != "" {
		raw := tc.principalID
		if !strings.HasPrefix(raw, teamPrincipalPrefix) || len(raw) <= len(teamPrincipalPrefix) {
			return errors.New("team principal ID must use team-<ULID> format")
		}
		if _, err := ulid.ParseStrict(raw[len(teamPrincipalPrefix):]); err != nil {
			return errors.New("team principal ID must use team-<ULID> format")
		}
	}
	for _, binding := range tc.externalTeams {
		if binding.Organization == "" || binding.TeamSlug == "" {
			return errors.New("external team binding requires organization and team slug")
		}
	}

	// Service account is optional, but if present, it must be valid
	if tc.serviceAccount != nil {
		if err := tc.serviceAccount.Validate(); err != nil {
			return err
		}
	}

	return nil
}
