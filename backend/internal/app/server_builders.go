package app

import (
	"context"
	"crypto/tls"
	"fmt"
	"log"
	"os"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/takutakahashi/agentapi-proxy/internal/domain/entities"
	"github.com/takutakahashi/agentapi-proxy/internal/infrastructure/repositories"
	"github.com/takutakahashi/agentapi-proxy/internal/modules/schedule"
	portrepos "github.com/takutakahashi/agentapi-proxy/internal/usecases/ports/repositories"
	sessionuc "github.com/takutakahashi/agentapi-proxy/internal/usecases/session"
	storageattachmentuc "github.com/takutakahashi/agentapi-proxy/internal/usecases/storageattachment"
	"github.com/takutakahashi/agentapi-proxy/pkg/config"
	"github.com/takutakahashi/agentapi-proxy/pkg/sessionsettings"
)

func buildStatusEventRepository(cfg *config.Config) portrepos.StatusEventRepository {
	if cfg.Redis.Addr == "" {
		log.Printf("[SERVER] Redis not configured – using noop status event repository")
		return repositories.NewNoopStatusRepository()
	}

	opts := &redis.Options{
		Addr:     cfg.Redis.Addr,
		Password: cfg.Redis.Password,
		DB:       cfg.Redis.DB,
	}

	if d, err := time.ParseDuration(cfg.Redis.DialTimeout); err == nil && d > 0 {
		opts.DialTimeout = d
	}
	if d, err := time.ParseDuration(cfg.Redis.ReadTimeout); err == nil && d > 0 {
		opts.ReadTimeout = d
	}
	if d, err := time.ParseDuration(cfg.Redis.WriteTimeout); err == nil && d > 0 {
		opts.WriteTimeout = d
	}
	if cfg.Redis.TLSEnabled {
		opts.TLSConfig = &tls.Config{MinVersion: tls.VersionTLS12}
	}

	client := redis.NewClient(opts)

	// Verify connectivity at startup (non-fatal: a misconfigured Redis falls
	// back to noop so the proxy can still serve requests).
	pingCtx, pingCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer pingCancel()
	if err := client.Ping(pingCtx).Err(); err != nil {
		log.Printf("[SERVER] Warning: Redis ping failed (%s) – falling back to noop status event repository: %v",
			cfg.Redis.Addr, err)
		_ = client.Close()
		return repositories.NewNoopStatusRepository()
	}

	podID, _ := os.Hostname()
	if podID == "" {
		podID = "unknown"
	}
	log.Printf("[SERVER] Redis status event repository connected: addr=%s podID=%s", cfg.Redis.Addr, podID)
	return repositories.NewRedisStatusRepository(client, podID)
}

func buildWorkerLeaseClient(cfg *config.Config) schedule.LeaseClient {
	if cfg.Redis.Addr == "" {
		log.Printf("[WORKER_CONTROL] Leader election disabled: API Redis is required")
		return nil
	}
	opts := &redis.Options{Addr: cfg.Redis.Addr, Password: cfg.Redis.Password, DB: cfg.Redis.DB}
	if d, err := time.ParseDuration(cfg.Redis.DialTimeout); err == nil && d > 0 {
		opts.DialTimeout = d
	}
	if d, err := time.ParseDuration(cfg.Redis.ReadTimeout); err == nil && d > 0 {
		opts.ReadTimeout = d
	}
	if d, err := time.ParseDuration(cfg.Redis.WriteTimeout); err == nil && d > 0 {
		opts.WriteTimeout = d
	}
	if cfg.Redis.TLSEnabled {
		opts.TLSConfig = &tls.Config{MinVersion: tls.VersionTLS12}
	}
	return schedule.NewRedisLeaseClient(redis.NewClient(opts))
}

func (s *Server) runRequestForStart(sessionID string, startReq entities.StartRequest, userID string, teams []string) *entities.RunServerRequest {
	var initialMessage, agentType, credentialSource, codexAuthMode, claudeAuthMode, model, sessionTTL string
	var authProxy *bool
	var sandbox *entities.SandboxParams
	var docker *entities.DockerParams
	var unsyncedFilePaths, modelOptions []string
	if startReq.Params != nil {
		initialMessage = startReq.Params.Message
		agentType = startReq.Params.AgentType
		authProxy = startReq.Params.AuthProxy
		sandbox = startReq.Params.Sandbox
		docker = startReq.Params.Docker
		credentialSource = startReq.Params.CredentialSource
		codexAuthMode = startReq.Params.CodexAuthMode
		claudeAuthMode = startReq.Params.ClaudeAuthMode
		model = startReq.Params.Model
		if len(startReq.Params.ModelOptions) > 0 {
			modelOptions = append([]string(nil), startReq.Params.ModelOptions...)
		}
		sessionTTL = sessionuc.ResolveSessionTTL(startReq.Params)
		unsyncedFilePaths = append([]string(nil), startReq.Params.UnsyncedFilePaths...)
	}
	runReq := &entities.RunServerRequest{
		UserID: userID, Teams: teams, Scope: startReq.Scope, TeamID: startReq.TeamID,
		TriggeredUserID: startReq.TriggeredUserID,
		Pool:            requestedSessionPool(startReq), AgentType: agentType, Model: model, SessionTTL: sessionTTL, Environment: startReq.Environment,
		ModelOptions:       modelOptions,
		ProfileEnvironment: startReq.ProfileEnvironment, Tags: startReq.Tags,
		InitialMessage: initialMessage, RepoInfo: s.extractRepositoryInfo(sessionID, startReq.Tags),
		GithubToken: githubTokenForStartRequest(startReq), AuthProxy: authProxy,
		Sandbox: sandbox, Docker: docker,
		UnsyncedFilePaths: unsyncedFilePaths, CredentialSource: credentialSource,
		CodexAuthMode: codexAuthMode, ClaudeAuthMode: claudeAuthMode,
		ProfileFiles:             startReq.ProfileFiles,
		ProfileRepositories:      startReq.ProfileRepositories,
		ProfileMCPServers:        startReq.ProfileMCPServers,
		ProfileSkills:            startReq.ProfileSkills,
		ProfileCommandWrapper:    startReq.ProfileCommandWrapper,
		ProfileSystemPrompt:      startReq.ProfileSystemPrompt,
		ResolvedSessionProfileID: startReq.ResolvedSessionProfileID,
		Storages:                 append([]entities.ResolvedStorageAttachment(nil), startReq.ResolvedStorages...),
	}
	if startReq.Params != nil {
		runReq.SlackParams = startReq.Params.Slack
		runReq.ResumeFrom = startReq.Params.ResumeFrom
		runReq.InitialMessageWaitSecond = startReq.Params.InitialMessageWaitSecond
		runReq.CycleMessage = startReq.Params.CycleMessage
		runReq.CycleMaxCount = startReq.Params.CycleMaxCount
	}
	return runReq
}

func (s *Server) resolveStorageAttachments(ctx context.Context, startReq entities.StartRequest, userID, pool string) ([]entities.ResolvedStorageAttachment, error) {
	if startReq.Params == nil || len(startReq.Params.Storages) == 0 {
		return nil, nil
	}
	return s.storageAttachmentResolver.Resolve(ctx, storageattachmentuc.Request{
		Attachments: startReq.Params.Storages,
		Tags:        startReq.Tags,
		Scope:       startReq.Scope,
		TeamID:      startReq.TeamID,
		UserID:      userID,
		Pool:        pool,
	})
}

func (s *Server) ResolveRestartSettings(ctx context.Context, id string, input entities.StartRequest, userID string, teams []string) (*sessionsettings.SessionSettings, error) {
	builder, ok := s.sessionManager.(portrepos.RemoteProvisionSettingsBuilder)
	if !ok {
		return nil, fmt.Errorf("settings reload is not supported")
	}
	return builder.BuildRemoteProvisionSettings(ctx, id, s.runRequestForStart(id, input, userID, teams))
}
