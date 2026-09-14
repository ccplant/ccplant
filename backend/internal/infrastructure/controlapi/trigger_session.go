package controlapi

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/takutakahashi/agentapi-proxy/internal/domain/entities"
	"github.com/takutakahashi/agentapi-proxy/pkg/executiontoken"
)

// WithSessionAPIURL selects the public API used by worker-triggered launches.
func (m *SessionManager) WithSessionAPIURL(apiURL string) *SessionManager {
	m.sessionAPIURL = strings.TrimRight(apiURL, "/")
	return m
}

func (m *SessionManager) startTriggerSession(ctx context.Context, id string, req *entities.RunServerRequest, webhookPayload []byte) (entities.Session, error) {
	if m.token == "" {
		return nil, fmt.Errorf("worker execution signing key is required")
	}
	claims := executiontoken.ExecutionClaims{
		SlackBotID: req.Tags["slackbot_id"], ExecutionID: id, SessionID: id,
		ScheduleID: req.Tags["schedule_id"], WebhookID: req.Tags["webhook_id"],
		UserID: req.UserID, TriggeredUserID: req.TriggeredUserID,
		Scope: req.Scope, TeamID: req.TeamID, Teams: req.Teams,
		ExpiresAt: time.Now().Add(5 * time.Minute).Unix(),
	}
	token, err := executiontoken.SignExecutionToken([]byte(m.token), claims)
	if err != nil {
		return nil, err
	}
	tags := make(map[string]string, len(req.Tags)+1)
	for k, v := range req.Tags {
		tags[k] = v
	}
	params := &entities.SessionParams{
		Pool: req.Pool, ResumeFrom: req.ResumeFrom, Message: req.InitialMessage,
		GithubToken: req.GithubToken, AgentType: req.AgentType, Model: req.Model,
		Slack: req.SlackParams, Oneshot: req.Oneshot, InitialMessageWaitSecond: req.InitialMessageWaitSecond,
		CycleMessage: req.CycleMessage, CycleMaxCount: req.CycleMaxCount,
		Sandbox: req.Sandbox, Docker: req.Docker, AuthProxy: req.AuthProxy,
		SessionTTL: req.SessionTTL, UnsyncedFilePaths: req.UnsyncedFilePaths,
		CredentialSource: req.CredentialSource, CodexAuthMode: req.CodexAuthMode, ClaudeAuthMode: req.ClaudeAuthMode,
	}
	if req.RepoInfo != nil {
		params.RepoFullName = req.RepoInfo.FullName
		tags["repository"] = req.RepoInfo.FullName
	}
	start := entities.StartRequest{
		Environment: req.Environment, Tags: tags, Params: params, Scope: req.Scope, TeamID: req.TeamID,
		MemoryKey: req.MemoryKey, SessionProfileID: req.ResolvedSessionProfileID,
		WebhookPayload: webhookPayload,
	}
	apiURL := m.sessionAPIURL
	if apiURL == "" {
		apiURL = m.baseURL
	}
	sessionID, err := m.startSession(ctx, apiURL, start, token, id)
	if err != nil {
		return nil, err
	}
	return entities.NewProxySessionWithStatus(sessionID, req.UserID, req.Scope, req.TeamID, tags, time.Now(), "creating"), nil
}
