package auth

import (
	"testing"
)

func TestCanAccessResource_TeamScope(t *testing.T) {
	tests := []struct {
		name        string
		authzUserID string
		isAdmin     bool
		teams       []string
		ownerUserID string
		scope       string
		teamID      string
		want        bool
	}{
		{
			name:        "member can access team session",
			authzUserID: "user-1",
			isAdmin:     false,
			teams:       []string{"org/team-a"},
			ownerUserID: "user-2",
			scope:       "team",
			teamID:      "org/team-a",
			want:        true,
		},
		{
			name:        "non-member cannot access team session",
			authzUserID: "user-1",
			isAdmin:     false,
			teams:       []string{"org/team-b"},
			ownerUserID: "user-2",
			scope:       "team",
			teamID:      "org/team-a",
			want:        false,
		},
		{
			name:        "admin can access any team session",
			authzUserID: "admin-user",
			isAdmin:     true,
			teams:       nil,
			ownerUserID: "user-2",
			scope:       "team",
			teamID:      "org/team-a",
			want:        true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			authzCtx := newAuthzContext(tt.authzUserID, tt.isAdmin, tt.teams)
			got := authzCtx.CanAccessResource(tt.ownerUserID, tt.scope, tt.teamID)
			if got != tt.want {
				t.Errorf("CanAccessResource() = %v, want %v", got, tt.want)
			}
		})
	}
}
