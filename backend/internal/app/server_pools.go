package app

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"strings"
	"time"

	sessionrunnercore "github.com/takutakahashi/agentapi-proxy/internal/core/sessionrunner"
	"github.com/takutakahashi/agentapi-proxy/internal/domain/entities"
	"github.com/takutakahashi/agentapi-proxy/internal/infrastructure/services"
	personalapikeyuc "github.com/takutakahashi/agentapi-proxy/internal/usecases/personal_api_key"
	portrepos "github.com/takutakahashi/agentapi-proxy/internal/usecases/ports/repositories"
	serviceaccountuc "github.com/takutakahashi/agentapi-proxy/internal/usecases/service_account"
	"github.com/takutakahashi/agentapi-proxy/pkg/sessionsettings"
)

func (s *Server) resolveSessionPool(ctx context.Context, subject sessionrunnercore.Subject, requestedPool string, tags map[string]string) (*sessionrunnercore.ResolvedPool, error) {
	resolver := sessionrunnercore.NewResolver(s.sessionRunnerStore, 90*time.Second)
	if s.esmControlStore != nil {
		resolver.WithManagerLiveness(s.esmControlStore)
	}
	return resolver.Resolve(ctx, subject, requestedPool, tags)
}

func (s *Server) resolveSessionRoute(ctx context.Context, subject sessionrunnercore.Subject, request sessionrunnercore.RouteRequest) (sessionrunnercore.AuthorizedRoute, error) {
	resolver := sessionrunnercore.NewResolver(s.sessionRunnerStore, 90*time.Second)
	if s.esmControlStore != nil {
		resolver.WithManagerLiveness(s.esmControlStore)
	}
	resolver.WithLocalFallback(s.localSessionFallbackEnabled)
	return resolver.ResolveRoute(ctx, subject, request)
}

func authorizedRouteContainsManager(route sessionrunnercore.AuthorizedRoute, managerID string) bool {
	for _, manager := range route.Managers() {
		if manager.ID == managerID {
			return true
		}
	}
	return false
}

func (s *Server) resolveSessionPoolWithTrace(ctx context.Context, subject sessionrunnercore.Subject, requestedPool string, tags map[string]string) (*sessionrunnercore.ResolvedPool, *sessionrunnercore.ResolutionTrace, error) {
	resolver := sessionrunnercore.NewResolver(s.sessionRunnerStore, 90*time.Second)
	if s.esmControlStore != nil {
		resolver.WithManagerLiveness(s.esmControlStore)
	}
	return resolver.ResolveWithTrace(ctx, subject, requestedPool, tags)
}

func requestedSessionPool(startReq entities.StartRequest) string {
	if startReq.Params == nil {
		return ""
	}
	return strings.TrimSpace(startReq.Params.Pool)
}

func (s *Server) createPoolSession(ctx context.Context, route sessionrunnercore.AuthorizedRoute, sessionID string, startReq entities.StartRequest, userID string, teams []string) (entities.Session, error) {
	if route == nil {
		return nil, fmt.Errorf("authorized session route is required")
	}
	pool := route.PoolName()
	if err := s.checkSessionPoolQuota(ctx, route); err != nil {
		return nil, err
	}
	runReq := s.runRequestForStart(sessionID, startReq, userID, teams)
	runReq.Pool = pool
	initialMessage, agentType, sessionTTL, docker := runReq.InitialMessage, runReq.AgentType, runReq.SessionTTL, runReq.Docker
	var settings *sessionsettings.SessionSettings
	if builder, ok := s.sessionManager.(portrepos.RemoteProvisionSettingsBuilder); ok {
		var err error
		settings, err = builder.BuildRemoteProvisionSettings(ctx, sessionID, runReq)
		if err != nil {
			return nil, fmt.Errorf("resolve pool provision settings: %w", err)
		}
		if settings == nil {
			return nil, fmt.Errorf("session manager returned no provision settings")
		}
	}
	if settings == nil {
		settings = &sessionsettings.SessionSettings{
			Session: sessionsettings.SessionMeta{UserID: userID, Scope: string(startReq.Scope), TeamID: startReq.TeamID, AgentType: agentType, Teams: teams},
			Env:     startReq.Environment, InitialMessage: initialMessage, UnsyncedFilePaths: runReq.UnsyncedFilePaths,
		}
	}
	settings.WebhookPayload = string(startReq.WebhookPayload)
	s.applyPoolAutoSuspendPolicy(ctx, settings, startReq.Scope, userID, startReq.TeamID)
	settingsRaw, err := json.Marshal(settings)
	if err != nil {
		return nil, fmt.Errorf("marshal pool provision settings: %w", err)
	}
	token, tokenHash, err := newDirectRuntimeToken()
	if err != nil {
		return nil, fmt.Errorf("create pool runtime credential: %w", err)
	}
	authorizedManagerIDs := make([]string, 0, len(route.Managers()))
	for _, manager := range route.Managers() {
		if manager != nil {
			authorizedManagerIDs = append(authorizedManagerIDs, manager.ID)
		}
	}
	allocation := &sessionrunnercore.Allocation{
		SessionID: sessionID, Pool: pool, BindingID: route.BindingID(), Generation: 1,
		AuthorizedManagerIDs: authorizedManagerIDs,
		Requirements: map[string]string{
			"agent_type": agentType,
			"dind":       fmt.Sprintf("%t", docker != nil && docker.Enabled),
		}, RuntimeToken: token,
		RuntimeTokenHash: tokenHash, ProvisionSettings: settingsRaw,
	}
	if err := s.sessionRunnerStore.Enqueue(ctx, allocation); err != nil {
		return nil, fmt.Errorf("enqueue session pool allocation: %w", err)
	}
	startedAt := time.Now().UTC()
	routeTags := make(map[string]string, len(startReq.Tags)+2)
	for key, value := range startReq.Tags {
		routeTags[key] = value
	}
	if sessionTTL != "" {
		routeTags["session_ttl"] = sessionTTL
	}
	if err := s.sessionRouteRepo.Save(ctx, &portrepos.SessionRoute{
		SessionID: sessionID, Transport: portrepos.SessionRouteTransportDirectRuntime,
		RuntimeTokenHash: tokenHash, Generation: 1, UserID: userID, Scope: string(startReq.Scope),
		TeamID: startReq.TeamID, Pool: pool, Tags: routeTags, StartedAt: startedAt, InitialMessage: initialMessage,
		Status: "creating", StatusUpdatedAt: startedAt,
	}); err != nil {
		return nil, fmt.Errorf("save pending pool session route: %w", err)
	}
	if s.sessionAllocationNotifier != nil {
		if err := s.sessionAllocationNotifier.Notify(ctx, pool); err != nil {
			log.Printf("[SESSION_POOL] Warning: failed to notify runners for allocation %s: %v", sessionID, err)
		}
	}
	return entities.NewProxySessionWithStatus(sessionID, userID, startReq.Scope, startReq.TeamID, startReq.Tags, startedAt, "creating"), nil
}

func (s *Server) applyPoolAutoSuspendPolicy(ctx context.Context, settings *sessionsettings.SessionSettings, scope entities.ResourceScope, userID, teamID string) {
	if settings == nil || s.settingsRepo == nil {
		return
	}
	settingsName := userID
	if scope == entities.ScopeTeam && teamID != "" {
		settingsName = teamID
	}
	stored, err := s.settingsRepo.FindByName(ctx, settingsName)
	if err != nil || stored == nil || stored.AutoSuspend() == nil {
		return
	}
	policy := stored.AutoSuspend()
	settings.Session.AutoSuspendEnabled = &policy.Enabled
	settings.Session.AutoSuspendMinutes = policy.IdleTimeoutMinutes
}

func (s *Server) checkSessionPoolQuota(ctx context.Context, route sessionrunnercore.AuthorizedRoute) error {
	if route == nil || route.MaxConcurrent() <= 0 {
		return nil
	}
	allocations, err := s.sessionRunnerStore.ListAllocations(ctx, route.PoolName())
	if err != nil {
		return fmt.Errorf("list session pool allocations: %w", err)
	}
	active := 0
	for _, allocation := range allocations {
		if allocation.BindingID == route.BindingID() && allocationCountsTowardQuota(allocation.Status) {
			active++
		}
	}
	if active >= route.MaxConcurrent() {
		return &sessionrunnercore.QuotaExceededError{
			Pool: route.PoolName(), BindingID: route.BindingID(),
			MaxConcurrent: route.MaxConcurrent(), Active: active,
		}
	}
	return nil
}

func allocationCountsTowardQuota(status sessionrunnercore.AllocationStatus) bool {
	switch status {
	case sessionrunnercore.AllocationPending,
		sessionrunnercore.AllocationLeased,
		sessionrunnercore.AllocationClaimed,
		sessionrunnercore.AllocationRunning:
		return true
	default:
		return false
	}
}

func (s *Server) EnsureTeamServiceAccount(ctx context.Context, teamID string) error {
	if teamID == "" || s.teamConfigRepo == nil {
		return nil
	}
	simpleAuth, ok := s.container.AuthService.(*services.SimpleAuthService)
	if !ok {
		return errors.New("team service-account auth service is unavailable")
	}
	return serviceaccountuc.NewGetOrCreateServiceAccountUseCase(s.teamConfigRepo, simpleAuth).EnsureServiceAccount(ctx, teamID)
}

func (s *Server) EnsurePersonalAPIKey(ctx context.Context, userID string) error {
	if userID == "" || s.personalAPIKeyRepo == nil {
		return nil
	}
	key, err := personalapikeyuc.NewGetOrCreatePersonalAPIKeyUseCase(s.personalAPIKeyRepo).Execute(ctx, entities.UserID(userID))
	if err != nil {
		return err
	}
	if simpleAuth, ok := s.container.AuthService.(*services.SimpleAuthService); ok {
		return simpleAuth.LoadPersonalAPIKey(ctx, key)
	}
	return errors.New("personal API-key auth service is unavailable")
}

func newDirectRuntimeToken() (string, string, error) {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", "", err
	}
	token := base64.RawURLEncoding.EncodeToString(raw)
	digest := sha256.Sum256([]byte(token))
	return token, hex.EncodeToString(digest[:]), nil
}

func githubTokenForStartRequest(startReq entities.StartRequest) string {
	if startReq.Scope == entities.ScopeTeam || startReq.Params == nil {
		return ""
	}
	return startReq.Params.GithubToken
}

// findESMByID searches the user's settings and team settings for an ESM entry with the given ID.
// findAutomaticAssignmentESM searches user and team settings for a legacy, pool-less ESM enabled for automatic assignment.
// User settings take precedence over team settings.
func (s *Server) findAutomaticAssignmentESM(ctx context.Context, userID string, teams []string, tags map[string]string) (*entities.ExternalSessionManagerEntry, error) {
	if s.settingsRepo == nil {
		return nil, nil
	}

	// Search user settings first
	userSettings, err := s.settingsRepo.FindByName(ctx, userID)
	if err == nil && userSettings != nil {
		for _, esm := range userSettings.ExternalSessionManagers() {
			if esm.Pool == "" && esm.IsAutomaticAssignmentEnabled() && externalSessionManagerMatches(esm, tags) {
				entry := esm
				return &entry, nil
			}
		}
	}

	// Search team settings
	for _, teamID := range teams {
		teamSettings, err := s.settingsRepo.FindByName(ctx, teamID)
		if err != nil {
			continue
		}
		for _, esm := range teamSettings.ExternalSessionManagers() {
			if esm.Pool == "" && esm.IsAutomaticAssignmentEnabled() && externalSessionManagerMatches(esm, tags) {
				entry := esm
				return &entry, nil
			}
		}
	}

	return nil, nil
}

func hasAllocatorSelector(tags map[string]string) bool {
	for key := range tags {
		if strings.HasPrefix(key, "allocator.") && key != "allocator.pool" {
			return true
		}
	}
	return false
}

func externalSessionManagerMatches(manager entities.ExternalSessionManagerEntry, tags map[string]string) bool {
	for key, expected := range tags {
		if !strings.HasPrefix(key, "allocator.") || key == "allocator.pool" {
			continue
		}
		label := strings.TrimPrefix(key, "allocator.")
		if label == "id" {
			if manager.ID != expected {
				return false
			}
			continue
		}
		if manager.Labels[label] != expected {
			return false
		}
	}
	return true
}

func (s *Server) findESMByID(ctx context.Context, userID string, teams []string, managerID string) (*entities.ExternalSessionManagerEntry, error) {
	if s.settingsRepo == nil {
		return nil, nil
	}

	// Search user settings
	userSettings, err := s.settingsRepo.FindByName(ctx, userID)
	if err == nil && userSettings != nil {
		for _, esm := range userSettings.ExternalSessionManagers() {
			if esm.ID == managerID {
				return &esm, nil
			}
		}
	}

	// Search team settings
	for _, teamID := range teams {
		teamSettings, err := s.settingsRepo.FindByName(ctx, teamID)
		if err != nil {
			continue
		}
		for _, esm := range teamSettings.ExternalSessionManagers() {
			if esm.ID == managerID {
				return &esm, nil
			}
		}
	}

	return nil, nil
}

// DeleteSessionByID deletes a session by ID
