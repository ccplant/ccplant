package services

import (
	"context"

	"github.com/takutakahashi/agentapi-proxy/internal/domain/entities"
	"github.com/takutakahashi/agentapi-proxy/pkg/sessionsettings"
	"github.com/takutakahashi/agentapi-proxy/pkg/settingspatch"
)

// This consolidates buildEnvVars and envFrom logic into a single unified structure.

type sessionSettingsBuilder struct {
	manager        *KubernetesSessionManager
	ctx            context.Context
	session        *KubernetesSession
	request        *entities.RunServerRequest
	webhookPayload []byte
	settings       *sessionsettings.SessionSettings
	environment    map[string]string
	materialized   settingspatch.MaterializedSettings
	secretFiles    []sessionsettings.ManagedFile
}

func (m *KubernetesSessionManager) buildSessionSettings(ctx context.Context, session *KubernetesSession, req *entities.RunServerRequest, webhookPayload []byte) *sessionsettings.SessionSettings {
	b := &sessionSettingsBuilder{manager: m, ctx: ctx, session: session, request: req, webhookPayload: webhookPayload, settings: &sessionsettings.SessionSettings{}}
	b.metadata()
	b.baseEnvironment()
	b.requestEnvironment()
	b.secretEnvironment()
	b.scopeEnvironment()
	b.layeredEnvironment()
	b.agentConfiguration()
	b.repositoryConfiguration()
	b.startupCommand()
	b.slackIntegration()
	b.telemetry()
	b.credentials()
	b.userFiles()
	b.projectedFiles()
	b.sandbox()
	b.dockerAndModels()
	return b.settings
}
