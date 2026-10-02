package controllers

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/labstack/echo/v4"
	"github.com/takutakahashi/agentapi-proxy/internal/domain/entities"
	ports "github.com/takutakahashi/agentapi-proxy/internal/usecases/ports/repositories"
	"github.com/takutakahashi/agentapi-proxy/pkg/auth"
)

type TeamMembershipController struct {
	teams       ports.TeamConfigRepository
	memberships ports.TeamMembershipRepository
	github      *GitHubConnectionsController
}

func NewTeamMembershipController(teams ports.TeamConfigRepository, memberships ports.TeamMembershipRepository, github *GitHubConnectionsController) *TeamMembershipController {
	return &TeamMembershipController{teams: teams, memberships: memberships, github: github}
}

type teamMembershipResponse struct {
	Members                     []entities.TeamMember `json:"members"`
	UnlinkedExternalMemberCount int                   `json:"unlinked_external_member_count"`
	Sync                        teamSyncStatus        `json:"sync"`
}

type teamSyncStatus struct {
	Status      string    `json:"status"`
	SyncedAt    time.Time `json:"synced_at,omitempty"`
	SyncedBy    string    `json:"synced_by,omitempty"`
	Reason      string    `json:"reason,omitempty"`
	NextSyncAt  time.Time `json:"next_sync_at,omitempty"`
	OperationID string    `json:"operation_id,omitempty"`
}

type teamSyncResponse struct {
	OperationID                 string    `json:"operation_id"`
	SyncedAt                    time.Time `json:"synced_at"`
	NextSyncAt                  time.Time `json:"next_sync_at"`
	MemberCount                 int       `json:"member_count"`
	UnlinkedExternalMemberCount int       `json:"unlinked_external_member_count"`
	AddedCount                  int       `json:"added_count"`
	RemovedCount                int       `json:"removed_count"`
}

func (c *TeamMembershipController) Get(ctx echo.Context) error {
	team, err := c.loadManageableTeam(ctx)
	if err != nil {
		return err
	}
	snapshot, found, err := c.memberships.Get(ctx.Request().Context(), team.PrincipalID())
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "failed to load team members").SetInternal(err)
	}
	if !found {
		return ctx.JSON(http.StatusOK, teamMembershipResponse{Members: []entities.TeamMember{}, Sync: teamSyncStatus{Status: "never"}})
	}
	return ctx.JSON(http.StatusOK, membershipResponse(snapshot))
}

func (c *TeamMembershipController) Sync(ctx echo.Context) error {
	team, err := c.loadManageableTeam(ctx)
	if err != nil {
		return err
	}
	user := auth.GetUserFromContext(ctx)
	if user == nil {
		return echo.NewHTTPError(http.StatusUnauthorized, "authentication required")
	}
	principalID, err := c.github.PrincipalIDForUser(ctx.Request().Context(), user)
	if err != nil {
		return echo.NewHTTPError(http.StatusUnprocessableEntity, "a linked GitHub account is required").SetInternal(err)
	}
	builtInToken := strings.TrimSpace(strings.TrimPrefix(ctx.Request().Header.Get("Authorization"), "Bearer "))
	result, err := c.syncTeam(ctx.Request().Context(), team, principalID, "manual", builtInToken)
	if errors.Is(err, ports.ErrTeamSyncRateLimited) {
		snapshot, _, _ := c.memberships.Get(ctx.Request().Context(), team.PrincipalID())
		retryAfter := 60
		if snapshot != nil {
			retryAfter = max(1, int(time.Until(snapshot.LastStartedAt.Add(time.Minute)).Seconds())+1)
		}
		ctx.Response().Header().Set("Retry-After", fmt.Sprintf("%d", retryAfter))
		return echo.NewHTTPError(http.StatusTooManyRequests, "team membership sync is limited to once per minute")
	}
	if errors.Is(err, ports.ErrTeamSyncInProgress) || errors.Is(err, ports.ErrTeamSyncConflict) {
		return echo.NewHTTPError(http.StatusConflict, "team membership sync is already in progress")
	}
	if err != nil {
		if strings.Contains(err.Error(), "no linked GitHub credential") || strings.Contains(err.Error(), "bindings") {
			return echo.NewHTTPError(http.StatusUnprocessableEntity, err.Error())
		}
		return echo.NewHTTPError(http.StatusBadGateway, "failed to synchronize GitHub team members").SetInternal(err)
	}
	return ctx.JSON(http.StatusOK, result)
}

func (c *TeamMembershipController) loadManageableTeam(ctx echo.Context) (*entities.TeamConfig, error) {
	team, err := c.teams.FindByTeamID(ctx.Request().Context(), ctx.Param("team"))
	if err != nil {
		return nil, echo.NewHTTPError(http.StatusNotFound, "team config not found")
	}
	if !canManageTeam(auth.GetAuthorizationContext(ctx), team) {
		return nil, echo.NewHTTPError(http.StatusForbidden, "team access denied")
	}
	return team, nil
}

func (c *TeamMembershipController) syncTeam(ctx context.Context, team *entities.TeamConfig, actorPrincipalID, reason, builtInToken string) (_ *teamSyncResponse, resultErr error) {
	bindings := team.ExternalTeams()
	if len(bindings) == 0 {
		return nil, errors.New("team has no GitHub team bindings")
	}
	for _, binding := range bindings {
		if strings.ContainsAny(binding.Organization, "*?") || strings.ContainsAny(binding.TeamSlug, "*?") {
			return nil, errors.New("wildcard GitHub team bindings cannot be synchronized")
		}
	}
	operationID := uuid.NewString()
	previous, err := c.memberships.AcquireSync(ctx, team.PrincipalID(), operationID, time.Now().UTC())
	if err != nil {
		return nil, err
	}
	log.Printf("[TEAM_MEMBERSHIP_SYNC] started operation=%s team=%s actor=%s reason=%s bindings=%d", operationID, team.PrincipalID(), actorPrincipalID, reason, len(bindings))
	defer func() {
		if resultErr != nil {
			_ = c.memberships.ReleaseSync(context.Background(), team.PrincipalID(), operationID)
			log.Printf("[TEAM_MEMBERSHIP_SYNC] failed operation=%s team=%s reason=%s error=%v", operationID, team.PrincipalID(), reason, resultErr)
		}
	}()

	externalByKey := make(map[string]*entities.ExternalTeamMember)
	for _, binding := range bindings {
		members, err := c.github.FetchExternalTeamMembers(ctx, actorPrincipalID, binding, builtInToken)
		if err != nil {
			return nil, err
		}
		for _, member := range members {
			key := externalIdentityKey(member.ConnectionID, member.GitHubUserID)
			if current := externalByKey[key]; current != nil {
				current.Sources = append(current.Sources, member.Sources...)
				continue
			}
			copy := member
			externalByKey[key] = &copy
		}
	}

	external := make([]entities.ExternalTeamMember, 0, len(externalByKey))
	memberByPrincipal := make(map[string]*entities.TeamMember)
	for _, value := range externalByKey {
		external = append(external, *value)
		principalID, found, err := c.github.PrincipalForExternalIdentity(ctx, value.ConnectionID, value.GitHubUserID)
		if err != nil {
			return nil, err
		}
		if !found {
			continue
		}
		ref := entities.ExternalIdentityRef{ConnectionID: value.ConnectionID, GitHubUserID: value.GitHubUserID}
		if current := memberByPrincipal[principalID]; current != nil {
			current.Sources = append(current.Sources, ref)
			continue
		}
		memberByPrincipal[principalID] = &entities.TeamMember{PrincipalID: principalID, Login: value.Login, Sources: []entities.ExternalIdentityRef{ref}}
	}
	members := make([]entities.TeamMember, 0, len(memberByPrincipal))
	for _, value := range memberByPrincipal {
		members = append(members, *value)
	}
	for _, value := range externalByKey {
		sort.Slice(value.Sources, func(i, j int) bool {
			left := value.Sources[i].ConnectionID + "\x00" + value.Sources[i].Organization + "\x00" + value.Sources[i].TeamSlug
			right := value.Sources[j].ConnectionID + "\x00" + value.Sources[j].Organization + "\x00" + value.Sources[j].TeamSlug
			return left < right
		})
	}
	for i := range members {
		sort.Slice(members[i].Sources, func(left, right int) bool {
			return externalIdentityKey(members[i].Sources[left].ConnectionID, members[i].Sources[left].GitHubUserID) < externalIdentityKey(members[i].Sources[right].ConnectionID, members[i].Sources[right].GitHubUserID)
		})
	}
	sort.Slice(external, func(i, j int) bool {
		return externalIdentityKey(external[i].ConnectionID, external[i].GitHubUserID) < externalIdentityKey(external[j].ConnectionID, external[j].GitHubUserID)
	})
	sort.Slice(members, func(i, j int) bool { return members[i].PrincipalID < members[j].PrincipalID })

	now := time.Now().UTC()
	snapshot := &entities.TeamMembershipSnapshot{TeamPrincipalID: team.PrincipalID(), ExternalMembers: external, Members: members, SyncedAt: now, SyncedBy: actorPrincipalID, SyncReason: reason}
	if err := c.memberships.Replace(ctx, snapshot, operationID); err != nil {
		return nil, err
	}
	previousIDs := make(map[string]struct{}, len(previous.Members))
	for _, member := range previous.Members {
		previousIDs[member.PrincipalID] = struct{}{}
	}
	currentIDs := make(map[string]struct{}, len(members))
	added := 0
	for _, member := range members {
		currentIDs[member.PrincipalID] = struct{}{}
		if _, exists := previousIDs[member.PrincipalID]; !exists {
			added++
		}
	}
	removed := 0
	for principalID := range previousIDs {
		if _, exists := currentIDs[principalID]; !exists {
			removed++
		}
	}
	log.Printf("[TEAM_MEMBERSHIP_SYNC] succeeded operation=%s team=%s actor=%s reason=%s members=%d unlinked=%d added=%d removed=%d", operationID, team.PrincipalID(), actorPrincipalID, reason, len(members), unlinkedExternalMemberCount(snapshot), added, removed)
	return &teamSyncResponse{OperationID: operationID, SyncedAt: now, NextSyncAt: previous.LastStartedAt.Add(time.Minute), MemberCount: len(members), UnlinkedExternalMemberCount: unlinkedExternalMemberCount(snapshot), AddedCount: added, RemovedCount: removed}, nil
}

// AutoSyncForPrincipal synchronizes only ccplant teams matched by the newly
// created or linked GitHub identity. Failures are returned for logging and do
// not roll back identity creation.
func (c *TeamMembershipController) AutoSyncForPrincipal(ctx context.Context, principalID, reason string) error {
	return c.AutoSyncForPrincipalWithToken(ctx, principalID, reason, "")
}

// AutoSyncForPrincipalWithToken also considers the built-in GitHub OAuth
// credential in addition to every linked connection credential.
func (c *TeamMembershipController) AutoSyncForPrincipalWithToken(ctx context.Context, principalID, reason, builtInToken string) error {
	live, _, err := c.github.ResolveLiveTeamMemberships(ctx, principalID)
	if err != nil {
		return err
	}
	if builtInToken != "" {
		if providerTeams, fetchErr := c.github.FetchBuiltInTeams(ctx, builtInToken); fetchErr == nil {
			live = append(live, providerTeams...)
		}
	}
	teams, err := c.teams.List(ctx)
	if err != nil {
		return err
	}
	matched := make(map[string]*entities.TeamConfig)
	for _, membership := range live {
		for _, team := range teams {
			for _, binding := range team.ExternalTeams() {
				if strings.EqualFold(binding.Organization, membership.Organization) && strings.EqualFold(binding.TeamSlug, membership.TeamSlug) {
					matched[team.PrincipalID()] = team
				}
			}
		}
	}
	var failures []error
	for _, team := range matched {
		if _, err := c.syncTeam(ctx, team, principalID, reason, builtInToken); err != nil && !errors.Is(err, ports.ErrTeamSyncRateLimited) && !errors.Is(err, ports.ErrTeamSyncInProgress) {
			failures = append(failures, fmt.Errorf("sync team %s: %w", team.TeamID(), err))
		}
	}
	return errors.Join(failures...)
}

func membershipResponse(snapshot *entities.TeamMembershipSnapshot) teamMembershipResponse {
	status := "succeeded"
	if snapshot.SyncedAt.IsZero() {
		status = "never"
	}
	if snapshot.OperationID != "" && snapshot.LeaseUntil.After(time.Now()) {
		status = "running"
	}
	return teamMembershipResponse{Members: snapshot.Members, UnlinkedExternalMemberCount: unlinkedExternalMemberCount(snapshot), Sync: teamSyncStatus{Status: status, SyncedAt: snapshot.SyncedAt, SyncedBy: snapshot.SyncedBy, Reason: snapshot.SyncReason, NextSyncAt: snapshot.LastStartedAt.Add(time.Minute), OperationID: snapshot.OperationID}}
}

func unlinkedExternalMemberCount(snapshot *entities.TeamMembershipSnapshot) int {
	linked := make(map[string]struct{})
	for _, member := range snapshot.Members {
		for _, source := range member.Sources {
			linked[externalIdentityKey(source.ConnectionID, source.GitHubUserID)] = struct{}{}
		}
	}
	count := 0
	for _, member := range snapshot.ExternalMembers {
		if _, exists := linked[externalIdentityKey(member.ConnectionID, member.GitHubUserID)]; !exists {
			count++
		}
	}
	return count
}

func externalIdentityKey(connectionID string, githubUserID int64) string {
	return fmt.Sprintf("%s\x00%d", connectionID, githubUserID)
}
