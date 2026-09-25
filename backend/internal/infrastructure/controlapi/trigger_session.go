package controlapi

import (
	"context"
	"encoding/json"
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
	start, token, err := m.triggerStartRequest(id, req, webhookPayload)
	if err != nil {
		return nil, err
	}
	apiURL := m.triggerSessionAPIURL()
	sessionID, reused, err := m.startSession(ctx, apiURL, start, token, id)
	if err != nil {
		return nil, err
	}
	tags := start.Tags
	session := entities.NewProxySessionWithStatus(sessionID, req.UserID, req.Scope, req.TeamID, tags, time.Now(), "creating")
	session.SetSessionReused(reused)
	return session, nil
}

// DryRunTriggerSession sends the trigger-derived request through the public
// /start API without creating or persisting a session.
func (m *SessionManager) DryRunTriggerSession(ctx context.Context, id string, req *entities.RunServerRequest, webhookPayload []byte) (map[string]interface{}, error) {
	start, token, err := m.triggerStartRequest(id, req, webhookPayload)
	if err != nil {
		return nil, err
	}
	data, err := m.startSessionResponse(ctx, m.triggerSessionAPIURL(), start, token, id, true)
	if err != nil {
		return nil, err
	}
	var response map[string]interface{}
	if err := json.Unmarshal(data, &response); err != nil {
		return nil, fmt.Errorf("decode session dry-run response: %w", err)
	}
	return response, nil
}

func (m *SessionManager) triggerStartRequest(id string, req *entities.RunServerRequest, webhookPayload []byte) (entities.StartRequest, string, error) {
	if m.token == "" {
		return entities.StartRequest{}, "", fmt.Errorf("worker execution signing key is required")
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
		return entities.StartRequest{}, "", err
	}
	tags := make(map[string]string, len(req.Tags)+1)
	for k, v := range req.Tags {
		tags[k] = v
	}
	params := &entities.SessionParams{
		Pool: req.Pool, ManagerID: req.ManagerID, ResumeFrom: req.ResumeFrom, Message: req.InitialMessage,
		GithubToken: req.GithubToken, AgentType: req.AgentType, Model: req.Model,
		Slack: req.SlackParams, InitialMessageWaitSecond: req.InitialMessageWaitSecond,
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
		SessionProfileID: req.ResolvedSessionProfileID,
		WebhookPayload:   webhookPayload, ReuseMatchTags: req.ReuseMatchTags, ReuseMessage: req.ReuseMessage,
		StopBeforeReuse: req.StopBeforeReuse,
		LimitMatchTags:  req.LimitMatchTags, MaxSessions: req.MaxSessions,
	}
	return start, token, nil
}

func (m *SessionManager) triggerSessionAPIURL() string {
	apiURL := m.sessionAPIURL
	if apiURL == "" {
		apiURL = m.baseURL
	}
	return apiURL
}
