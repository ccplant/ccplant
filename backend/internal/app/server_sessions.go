package app

import (
	"context"
	"fmt"
	"strings"

	sessionrunnercore "github.com/takutakahashi/agentapi-proxy/internal/core/sessionrunner"
	"github.com/takutakahashi/agentapi-proxy/internal/domain/entities"
	"github.com/takutakahashi/agentapi-proxy/internal/infrastructure/services"
	portrepos "github.com/takutakahashi/agentapi-proxy/internal/usecases/ports/repositories"
	serviceaccountuc "github.com/takutakahashi/agentapi-proxy/internal/usecases/service_account"
	sessionuc "github.com/takutakahashi/agentapi-proxy/internal/usecases/session"
	"github.com/takutakahashi/agentapi-proxy/pkg/telemetry"
)

func (s *Server) CreateSession(ctx context.Context, sessionID string, startReq entities.StartRequest, userID, userRole string, teams []string) (entities.Session, error) {
	return telemetry.Operation(ctx, "app.Server.CreateSession", func(ctx context.Context) (entities.Session, error) {
		return s.createSession(ctx, sessionID, startReq, userID, userRole, teams)
	}, telemetry.String("session.scope", string(startReq.Scope)))
}

// PreviewSession resolves placement and provision settings without creating or
// persisting any session resources. The returned session ID is only a candidate.
func (s *Server) PreviewSession(ctx context.Context, sessionID string, startReq entities.StartRequest, userID, userRole string, teams []string) (*entities.SessionStartPreview, error) {
	if startReq.Params != nil && startReq.Params.ManagerID != "" {
		esm, err := s.findESMByID(ctx, userID, teams, startReq.Params.ManagerID)
		if err != nil {
			return nil, fmt.Errorf("failed to find external session manager %s: %w", startReq.Params.ManagerID, err)
		}
		if esm == nil {
			return nil, fmt.Errorf("external session manager not found: %s", startReq.Params.ManagerID)
		}
		if s.sessionRunnerStore == nil || esm.Pool == "" {
			return nil, fmt.Errorf("session manager %s has no runner pool", startReq.Params.ManagerID)
		}
		pool, err := s.sessionRunnerStore.GetLogicalPool(ctx, esm.Pool)
		if err != nil || !pool.Enabled {
			return nil, fmt.Errorf("session manager pool is unavailable: %s", esm.Pool)
		}
		preview, err := s.previewWithPlacement(ctx, sessionID, startReq, userID, teams, entities.SessionStartPlacement{
			Transport: portrepos.SessionRouteTransportDirectRuntime, Pool: pool.Name, ManagerID: esm.ID,
		})
		if preview != nil {
			preview.Resolution = map[string]interface{}{"placement": map[string]interface{}{"reason": "explicit_manager_selected", "manager_id": esm.ID, "pool": pool.Name}}
		}
		return preview, err
	}

	if s.sessionRunnerStore != nil {
		subject := sessionrunnercore.Subject{Type: sessionrunnercore.SubjectUser, ID: userID}
		if startReq.Scope == entities.ScopeTeam {
			subject = sessionrunnercore.Subject{Type: sessionrunnercore.SubjectTeam, ID: startReq.TeamID}
		}
		resolved, trace, err := s.resolveSessionPoolWithTrace(ctx, subject, requestedSessionPool(startReq), startReq.Tags)
		if err != nil {
			return nil, fmt.Errorf("select session pool: %w", err)
		}
		if resolved != nil {
			route, err := s.resolveSessionRoute(ctx, subject, sessionrunnercore.RouteRequest{RequestedPool: resolved.Pool.Name, Tags: startReq.Tags})
			if err != nil {
				return nil, fmt.Errorf("resolve authorized session route: %w", err)
			}
			if route == nil {
				return nil, fmt.Errorf("no authorized and healthy session route is available")
			}
			if err := s.checkSessionPoolQuota(ctx, route); err != nil {
				return nil, err
			}
			placement := entities.SessionStartPlacement{Transport: portrepos.SessionRouteTransportDirectRuntime, Pool: route.PoolName(), BindingID: route.BindingID()}
			preview, err := s.previewWithPlacement(ctx, sessionID, startReq, userID, teams, placement)
			if preview != nil {
				preview.Resolution = map[string]interface{}{"pool": trace, "placement": map[string]interface{}{"reason": "session_runner_pool_selected"}}
			}
			return preview, err
		}
	}
	if requestedPool := requestedSessionPool(startReq); requestedPool != "" {
		return nil, fmt.Errorf("no authorized and healthy session pool matches %q", requestedPool)
	}

	sandboxRequested := startReq.Params != nil && startReq.Params.Sandbox != nil && startReq.Params.Sandbox.Enabled
	dindRequested := startReq.Params != nil && startReq.Params.Docker != nil && startReq.Params.Docker.Enabled
	hasAllocatorSelector := hasAllocatorSelector(startReq.Tags)
	if hasAllocatorSelector && (sandboxRequested || dindRequested) {
		return nil, fmt.Errorf("allocator.* routing does not support sandbox or Docker-in-Docker")
	}
	if !sandboxRequested && !dindRequested {
		selectedESM, err := s.findAutomaticAssignmentESM(ctx, userID, teams, startReq.Tags)
		if err != nil {
			return nil, fmt.Errorf("select external session manager: %w", err)
		}
		if selectedESM != nil {
			if startReq.Params == nil {
				startReq.Params = &entities.SessionParams{}
			}
			startReq.Params.ManagerID = selectedESM.ID
			return s.PreviewSession(ctx, sessionID, startReq, userID, userRole, teams)
		}
		if hasAllocatorSelector {
			return nil, fmt.Errorf("no external session manager matches allocator.* tags")
		}
	}
	if !s.localSessionFallbackEnabled {
		return nil, fmt.Errorf("no authorized and healthy session pool is available")
	}

	mergedEnv, err := services.MergeEnvironmentVariables(services.EnvMergeConfig{
		RoleEnvFiles: &s.config.RoleEnvFiles, UserRole: userRole,
		TeamEnvFile: services.ExtractTeamEnvFile(startReq.Tags), RequestEnv: startReq.Environment,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to merge environment variables: %w", err)
	}
	startReq.Environment = mergedEnv
	preview, err := s.previewWithPlacement(ctx, sessionID, startReq, userID, teams, entities.SessionStartPlacement{
		Transport: "local", LocalFallback: true,
	})
	if preview != nil {
		preview.Resolution = map[string]interface{}{"placement": map[string]interface{}{"reason": "no_remote_placement_local_fallback"}}
	}
	return preview, err
}

func (s *Server) previewWithPlacement(ctx context.Context, sessionID string, startReq entities.StartRequest, userID string, teams []string, placement entities.SessionStartPlacement) (*entities.SessionStartPreview, error) {
	builder, ok := s.sessionManager.(portrepos.RemoteProvisionSettingsBuilder)
	if !ok {
		return nil, fmt.Errorf("session settings preview is not supported")
	}
	resolvedStorages, err := s.resolveStorageAttachments(ctx, startReq, userID, placement.Pool)
	if err != nil {
		return nil, err
	}
	startReq.ResolvedStorages = resolvedStorages
	runReq := s.runRequestForStart(sessionID, startReq, userID, teams)
	runReq.Pool = placement.Pool
	settings, err := builder.BuildRemoteProvisionSettings(ctx, sessionID, runReq)
	if err != nil {
		return nil, fmt.Errorf("resolve session settings: %w", err)
	}
	if settings == nil {
		return nil, fmt.Errorf("session manager returned no provision settings")
	}
	settings.WebhookPayload = string(startReq.WebhookPayload)
	if placement.Pool != "" {
		s.applyPoolAutoSuspendPolicy(ctx, settings, startReq.Scope, userID, startReq.TeamID)
	}
	return &entities.SessionStartPreview{Placement: placement, Settings: settings}, nil
}

func (s *Server) createSession(ctx context.Context, sessionID string, startReq entities.StartRequest, userID, userRole string, teams []string) (entities.Session, error) {
	if s.sessionRunnerStore == nil {
		return nil, fmt.Errorf("authorized session routing is unavailable")
	}
	subject := sessionrunnercore.Subject{Type: sessionrunnercore.SubjectUser, ID: userID}
	if startReq.Scope == entities.ScopeTeam {
		subject = sessionrunnercore.Subject{Type: sessionrunnercore.SubjectTeam, ID: startReq.TeamID}
	}
	requestedPool := requestedSessionPool(startReq)
	explicitManagerID := ""
	if startReq.Params != nil {
		explicitManagerID = strings.TrimSpace(startReq.Params.ManagerID)
	}
	route, err := s.resolveSessionRoute(ctx, subject, sessionrunnercore.RouteRequest{
		RequestedPool: requestedPool, Tags: startReq.Tags, RequiredManagerID: explicitManagerID,
	})
	if err != nil {
		return nil, fmt.Errorf("select authorized session route: %w", err)
	}
	if route == nil {
		return nil, fmt.Errorf("no authorized and healthy session route is available")
	}
	if explicitManagerID != "" && !authorizedRouteContainsManager(route, explicitManagerID) {
		return nil, fmt.Errorf("requested session manager is not an authorized supplier of pool %q", route.PoolName())
	}
	resolvedStorages, err := s.resolveStorageAttachments(ctx, startReq, userID, route.PoolName())
	if err != nil {
		return nil, err
	}
	startReq.ResolvedStorages = resolvedStorages

	// Identity and TeamConfig mutation belong to the API. The execution-plane
	// manager receives an already-authorized request and never initializes the
	// public authentication service.
	if startReq.Scope == entities.ScopeTeam && startReq.TeamID != "" && s.teamConfigRepo != nil {
		if simpleAuth, ok := s.container.AuthService.(*services.SimpleAuthService); ok {
			ensurer := serviceaccountuc.NewGetOrCreateServiceAccountUseCase(s.teamConfigRepo, simpleAuth)
			if err := ensurer.EnsureServiceAccount(ctx, startReq.TeamID); err != nil {
				return nil, fmt.Errorf("ensure team service account: %w", err)
			}
		}
	}
	if startReq.Scope != entities.ScopeTeam {
		if err := s.EnsurePersonalAPIKey(ctx, userID); err != nil {
			return nil, fmt.Errorf("ensure personal API key: %w", err)
		}
	}
	switch route.Kind() {
	case sessionrunnercore.RouteKindPool:
		return s.createPoolSession(ctx, route, sessionID, startReq, userID, teams)
	case sessionrunnercore.RouteKindLocal:
		return s.createLocalSession(ctx, route, sessionID, startReq, userID, userRole, teams)
	default:
		return nil, fmt.Errorf("unsupported authorized session route kind %q", route.Kind())
	}
}

func (s *Server) createLocalSession(ctx context.Context, route sessionrunnercore.AuthorizedRoute, sessionID string, startReq entities.StartRequest, userID, userRole string, teams []string) (entities.Session, error) {
	if route == nil || route.Kind() != sessionrunnercore.RouteKindLocal {
		return nil, fmt.Errorf("authorized local session route is required")
	}
	mergedEnv, err := services.MergeEnvironmentVariables(services.EnvMergeConfig{
		RoleEnvFiles: &s.config.RoleEnvFiles, UserRole: userRole,
		TeamEnvFile: services.ExtractTeamEnvFile(startReq.Tags), RequestEnv: startReq.Environment,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to merge environment variables: %w", err)
	}
	startReq.Environment = mergedEnv
	repoInfo := s.extractRepositoryInfo(sessionID, startReq.Tags)
	var initialMessage, agentType, sessionTTL string
	var slackParams *entities.SlackParams
	var initialMessageWaitSecond *int
	var cycleMessage string
	var cycleMaxCount int
	var sandbox *entities.SandboxParams
	var docker *entities.DockerParams
	var authProxy *bool
	var unsyncedFilePaths, modelOptions []string
	var credentialSource, codexAuthMode, claudeAuthMode, model, resumeFrom string
	if startReq.Params != nil {
		initialMessage = startReq.Params.Message
		agentType = startReq.Params.AgentType
		slackParams = startReq.Params.Slack
		initialMessageWaitSecond = startReq.Params.InitialMessageWaitSecond
		cycleMessage = startReq.Params.CycleMessage
		cycleMaxCount = startReq.Params.CycleMaxCount
		sandbox = startReq.Params.Sandbox
		docker = startReq.Params.Docker
		authProxy = startReq.Params.AuthProxy
		sessionTTL = sessionuc.ResolveSessionTTL(startReq.Params)
		unsyncedFilePaths = append([]string(nil), startReq.Params.UnsyncedFilePaths...)
		credentialSource = startReq.Params.CredentialSource
		codexAuthMode = startReq.Params.CodexAuthMode
		claudeAuthMode = startReq.Params.ClaudeAuthMode
		resumeFrom = startReq.Params.ResumeFrom
		model = startReq.Params.Model
		modelOptions = append([]string(nil), startReq.Params.ModelOptions...)
	}
	result, err := sessionuc.NewLaunchUseCase(s.sessionManager).Launch(ctx, sessionID, sessionuc.LaunchRequest{
		WebhookPayload: startReq.WebhookPayload, ResumeFrom: resumeFrom, TriggeredUserID: startReq.TriggeredUserID,
		UserID: userID, Environment: startReq.Environment, ProfileEnvironment: startReq.ProfileEnvironment,
		Tags: startReq.Tags, RepoInfo: repoInfo, InitialMessage: initialMessage, Teams: teams,
		GithubToken: githubTokenForStartRequest(startReq), Scope: startReq.Scope, TeamID: startReq.TeamID,
		AgentType: agentType, Model: model, ModelOptions: modelOptions, SlackParams: slackParams,
		InitialMessageWaitSecond: initialMessageWaitSecond, CycleMessage: cycleMessage, CycleMaxCount: cycleMaxCount,
		Sandbox: sandbox, Docker: docker, AuthProxy: authProxy, SessionTTL: sessionTTL,
		UnsyncedFilePaths: unsyncedFilePaths, CredentialSource: credentialSource,
		CodexAuthMode: codexAuthMode, ClaudeAuthMode: claudeAuthMode, ProfileFiles: startReq.ProfileFiles, ProfileRepositories: startReq.ProfileRepositories,
		ProfileMCPServers: startReq.ProfileMCPServers, ResolvedSessionProfileID: startReq.ResolvedSessionProfileID,
		ProfileSkills: startReq.ProfileSkills, ProfileCommandWrapper: startReq.ProfileCommandWrapper,
		ProfileSystemPrompt: startReq.ProfileSystemPrompt,
		Storages:            startReq.ResolvedStorages,
	})
	if err != nil {
		return nil, err
	}
	return result.Session, nil
}
