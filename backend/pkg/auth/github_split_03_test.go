package auth

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/takutakahashi/agentapi-proxy/pkg/config"
)

var hasWildcardPatternTests = []struct {
	name        string
	teamMapping map[string]config.TeamRoleRule
	expected    bool
}{
	{
		name: "no wildcard patterns",
		teamMapping: map[string]config.TeamRoleRule{
			"org/team":   {Role: "developer"},
			"org2/team2": {Role: "admin"},
		},
		expected: false,
	},
	{
		name: "has wildcard pattern",
		teamMapping: map[string]config.TeamRoleRule{
			"org/team":   {Role: "developer"},
			"*/cc-users": {Role: "admin"},
		},
		expected: true,
	},
	{
		name: "only wildcard patterns",
		teamMapping: map[string]config.TeamRoleRule{
			"*/cc-users": {Role: "developer"},
			"*/admins":   {Role: "admin"},
		},
		expected: true,
	},
	{
		name: "team wildcard pattern",
		teamMapping: map[string]config.TeamRoleRule{
			"myorg/*": {Role: "developer"},
		},
		expected: true,
	},
	{
		name: "prefix pattern",
		teamMapping: map[string]config.TeamRoleRule{
			"myorg/backend-*": {Role: "developer"},
		},
		expected: true,
	},
	{
		name: "suffix pattern",
		teamMapping: map[string]config.TeamRoleRule{
			"myorg/*-engineer": {Role: "developer"},
		},
		expected: true,
	},
	{
		name:        "empty mapping",
		teamMapping: map[string]config.TeamRoleRule{},
		expected:    false,
	},
}

func TestGitHubAuthProvider_HasWildcardPatterns(t *testing.T) {

	for _, tt := range hasWildcardPatternTests {
		t.Run(tt.name, func(t *testing.T) {
			provider := NewGitHubAuthProvider(&config.GitHubAuthConfig{
				UserMapping: config.GitHubUserMapping{
					TeamRoleMapping: tt.teamMapping,
				},
			})

			result := provider.hasWildcardPatterns()
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestMergeTeamMembershipsDeduplicates(t *testing.T) {
	first := []GitHubTeamMembership{{Organization: "takutaka-lab", TeamSlug: "admin", TeamName: "Admin"}}
	second := []GitHubTeamMembership{
		{Organization: "TAKUTAKA-LAB", TeamSlug: "ADMIN", TeamName: "duplicate"},
		{Organization: "takutaka-lab", TeamSlug: "developers", TeamName: "Developers"},
	}

	merged := mergeTeamMemberships(first, second)
	if len(merged) != 2 {
		t.Fatalf("len(merged) = %d, want 2", len(merged))
	}
	if merged[0].TeamName != "Admin" || merged[1].TeamSlug != "developers" {
		t.Fatalf("merged = %#v", merged)
	}
}

func TestMatchWildcard(t *testing.T) {
	tests := []struct {
		name     string
		pattern  string
		str      string
		expected bool
	}{
		// Exact matches
		{name: "exact match", pattern: "team", str: "team", expected: true},
		{name: "exact no match", pattern: "team", str: "other", expected: false},

		// Full wildcard
		{name: "full wildcard", pattern: "*", str: "anything", expected: true},
		{name: "full wildcard empty", pattern: "*", str: "", expected: true},

		// Prefix patterns
		{name: "prefix match", pattern: "backend-*", str: "backend-team", expected: true},
		{name: "prefix match multiple", pattern: "backend-*", str: "backend-engineers", expected: true},
		{name: "prefix no match", pattern: "backend-*", str: "frontend-team", expected: false},
		{name: "prefix no match missing dash", pattern: "backend-*", str: "backend", expected: false},

		// Suffix patterns
		{name: "suffix match", pattern: "*-engineer", str: "backend-engineer", expected: true},
		{name: "suffix match multiple", pattern: "*-engineer", str: "frontend-engineer", expected: true},
		{name: "suffix no match", pattern: "*-engineer", str: "backend-team", expected: false},
		{name: "suffix no match missing dash", pattern: "*-engineer", str: "engineer", expected: false},

		// Edge cases
		{name: "empty pattern no match", pattern: "", str: "team", expected: false},
		{name: "empty string exact match", pattern: "", str: "", expected: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := matchWildcard(tt.pattern, tt.str)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestMatchTeamPattern(t *testing.T) {
	tests := []struct {
		name     string
		pattern  string
		org      string
		teamSlug string
		expected bool
	}{
		// Exact matches
		{name: "exact match", pattern: "myorg/myteam", org: "myorg", teamSlug: "myteam", expected: true},
		{name: "exact no match org", pattern: "myorg/myteam", org: "otherorg", teamSlug: "myteam", expected: false},
		{name: "exact no match team", pattern: "myorg/myteam", org: "myorg", teamSlug: "otherteam", expected: false},

		// Org wildcard
		{name: "org wildcard match", pattern: "*/myteam", org: "anyorg", teamSlug: "myteam", expected: true},
		{name: "org wildcard no match", pattern: "*/myteam", org: "anyorg", teamSlug: "otherteam", expected: false},

		// Team wildcard
		{name: "team wildcard match", pattern: "myorg/*", org: "myorg", teamSlug: "anyteam", expected: true},
		{name: "team wildcard no match", pattern: "myorg/*", org: "otherorg", teamSlug: "anyteam", expected: false},

		// Both wildcards
		{name: "both wildcards match", pattern: "*/*", org: "anyorg", teamSlug: "anyteam", expected: true},

		// Prefix patterns
		{name: "team prefix match", pattern: "myorg/backend-*", org: "myorg", teamSlug: "backend-team", expected: true},
		{name: "team prefix no match", pattern: "myorg/backend-*", org: "myorg", teamSlug: "frontend-team", expected: false},

		// Suffix patterns
		{name: "team suffix match", pattern: "myorg/*-engineer", org: "myorg", teamSlug: "backend-engineer", expected: true},
		{name: "team suffix match multiple", pattern: "myorg/*-engineer", org: "myorg", teamSlug: "frontend-engineer", expected: true},
		{name: "team suffix no match", pattern: "myorg/*-engineer", org: "myorg", teamSlug: "backend-team", expected: false},

		// Combined patterns
		{name: "org wildcard team suffix", pattern: "*/*-engineer", org: "anyorg", teamSlug: "backend-engineer", expected: true},
		{name: "org wildcard team prefix", pattern: "*/backend-*", org: "anyorg", teamSlug: "backend-team", expected: true},

		// Invalid patterns
		{name: "invalid pattern no slash", pattern: "invalid", org: "org", teamSlug: "team", expected: false},
		{name: "invalid pattern multiple slashes", pattern: "a/b/c", org: "org", teamSlug: "team", expected: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := matchTeamPattern(tt.pattern, tt.org, tt.teamSlug)
			assert.Equal(t, tt.expected, result)
		})
	}
}
