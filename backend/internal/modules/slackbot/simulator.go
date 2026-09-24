package slackbot

import (
	"context"
	"fmt"
	"time"

	"github.com/takutakahashi/agentapi-proxy/internal/domain/entities"
	"github.com/takutakahashi/agentapi-proxy/internal/usecases/ports/repositories"
)

const (
	simulationDecisionCreateOrReuse = "create_or_reuse"
	simulationDecisionStop          = "stop"
	simulationDecisionIgnore        = "ignore"
	simulationDecisionError         = "error"
)

// SlackBotSimulationRequest supplies the Socket Mode payload and the Slack API
// responses needed after receipt. No real Slack call is made during simulation.
type SlackBotSimulationRequest struct {
	Type           string         `json:"type,omitempty"`
	TeamID         string         `json:"team_id,omitempty"`
	ChannelName    string         `json:"channel_name,omitempty"`
	ThreadMessages []SlackMessage `json:"thread_messages,omitempty"`
	Event          SlackEvent     `json:"event"`
}

type SlackBotSimulationPlan struct {
	SessionID        string                 `json:"session_id,omitempty"`
	InitialMessage   string                 `json:"initial_message"`
	ReuseMessage     string                 `json:"reuse_message"`
	Tags             map[string]string      `json:"tags"`
	Environment      map[string]string      `json:"environment"`
	Params           *SlackBotSessionParams `json:"params,omitempty"`
	Repository       string                 `json:"repository,omitempty"`
	ReuseMatchTags   map[string]string      `json:"reuse_match_tags"`
	MaxSessions      int                    `json:"max_sessions"`
	WouldPostToSlack bool                   `json:"would_post_to_slack"`
}

type SlackBotSimulationResponse struct {
	DryRun      bool                    `json:"dry_run"`
	Decision    string                  `json:"decision"`
	Reason      string                  `json:"reason,omitempty"`
	Plan        *SlackBotSimulationPlan `json:"plan,omitempty"`
	SideEffects []string                `json:"side_effects"`
	Errors      []string                `json:"errors,omitempty"`
}

// SimulateSlackBotEvent enters through SlackBotEventHandler.ProcessEvent, exactly
// like Socket Mode after payload decoding. Only its Slack and session adapters are
// replaced with recorders, and deferred work is run synchronously for the response.
func SimulateSlackBotEvent(ctx context.Context, repo repositories.SlackBotRepository, sessionManager repositories.SessionManager, profileRepo repositories.SessionProfileRepository, bot *entities.SlackBot, req SlackBotSimulationRequest) SlackBotSimulationResponse {
	recorder := &simulationSessionManager{reader: sessionManager}
	slack := &simulationSlackClient{channelName: req.ChannelName, threadMessages: req.ThreadMessages}
	handler := NewSlackBotEventHandler(repo, recorder, "simulation-secret", "bot-token", slack, "https://simulation.invalid", false, profileRepo)
	handler.synchronous = true
	payloadType := req.Type
	if payloadType == "" {
		payloadType = "event_callback"
	}
	err := handler.ProcessEvent(ctx, bot.ID(), SlackPayload{Type: payloadType, TeamID: req.TeamID, Event: &req.Event})
	response := SlackBotSimulationResponse{DryRun: true, Decision: handler.eventOutcome(), SideEffects: recorder.sideEffects()}
	if response.Decision == "" {
		response.Decision = simulationDecisionIgnore
	}
	if err != nil {
		response.Decision = simulationDecisionError
		response.Errors = []string{err.Error()}
	}
	if recorder.createRequest != nil {
		response.Plan = simulationPlan(recorder.sessionID, recorder.createRequest, len(slack.posts) > 0)
	}
	if len(slack.posts) > 0 {
		response.SideEffects = append(response.SideEffects, "post_message_to_slack")
	}
	if response.Decision == simulationDecisionIgnore {
		response.Reason = "filtered_or_not_actionable"
	}
	return response
}

func simulationPlan(sessionID string, req *entities.RunServerRequest, posted bool) *SlackBotSimulationPlan {
	params := &SlackBotSessionParams{Pool: req.Pool, ManagerID: req.ManagerID, AgentType: req.AgentType, Model: req.Model, AuthProxy: req.AuthProxy, CredentialSource: req.CredentialSource}
	repository := ""
	if req.RepoInfo != nil {
		repository = req.RepoInfo.FullName
		params.RepoFullName = repository
	}
	return &SlackBotSimulationPlan{SessionID: sessionID, InitialMessage: req.InitialMessage, ReuseMessage: req.ReuseMessage, Tags: req.Tags, Environment: req.Environment, Params: params, Repository: repository, ReuseMatchTags: req.ReuseMatchTags, MaxSessions: req.MaxSessions, WouldPostToSlack: posted}
}

type simulationSlackClient struct {
	channelName    string
	threadMessages []SlackMessage
	posts          []string
}

func (s *simulationSlackClient) ResolveChannelName(context.Context, string, string) (string, error) {
	if s.channelName == "" {
		return "", fmt.Errorf("channel_name is required to simulate channel resolution")
	}
	return s.channelName, nil
}
func (*simulationSlackClient) GetBotToken(context.Context, string, string) (string, error) {
	return "simulation-token", nil
}
func (s *simulationSlackClient) FetchThreadReplies(context.Context, string, string, string) ([]SlackMessage, error) {
	return s.threadMessages, nil
}
func (s *simulationSlackClient) PostMessage(_ context.Context, _, _, message, _ string) error {
	s.posts = append(s.posts, message)
	return nil
}

type simulationSessionManager struct {
	reader        repositories.SessionManager
	sessionID     string
	createRequest *entities.RunServerRequest
	stopped       []string
	sent          []string
}

func (s *simulationSessionManager) CreateSession(_ context.Context, id string, req *entities.RunServerRequest, _ []byte) (entities.Session, error) {
	s.sessionID, s.createRequest = id, req
	return &simulationSession{id: id, req: req}, nil
}
func (s *simulationSessionManager) GetSession(id string) entities.Session {
	if s.reader != nil {
		return s.reader.GetSession(id)
	}
	return nil
}
func (s *simulationSessionManager) ListSessions(filter entities.SessionFilter) []entities.Session {
	if s.reader != nil {
		return s.reader.ListSessions(filter)
	}
	return nil
}
func (*simulationSessionManager) DeleteSession(string) error { return nil }
func (s *simulationSessionManager) SendMessage(_ context.Context, id, _ string) error {
	s.sent = append(s.sent, id)
	return nil
}
func (s *simulationSessionManager) StopAgent(_ context.Context, id string) error {
	s.stopped = append(s.stopped, id)
	return nil
}
func (s *simulationSessionManager) GetMessages(ctx context.Context, id string) ([]repositories.Message, error) {
	if s.reader != nil {
		return s.reader.GetMessages(ctx, id)
	}
	return nil, nil
}
func (*simulationSessionManager) Shutdown(time.Duration) error { return nil }
func (s *simulationSessionManager) sideEffects() []string {
	effects := []string{}
	if s.createRequest != nil {
		effects = append(effects, "create_or_reuse_session")
	}
	if len(s.stopped) > 0 {
		effects = append(effects, "stop_session")
	}
	if len(s.sent) > 0 {
		effects = append(effects, "send_message_to_session")
	}
	return effects
}

type simulationSession struct {
	id  string
	req *entities.RunServerRequest
}

func (s *simulationSession) ID() string                    { return s.id }
func (*simulationSession) Addr() string                    { return "" }
func (s *simulationSession) UserID() string                { return s.req.UserID }
func (s *simulationSession) Scope() entities.ResourceScope { return s.req.Scope }
func (s *simulationSession) TeamID() string                { return s.req.TeamID }
func (s *simulationSession) Tags() map[string]string       { return s.req.Tags }
func (*simulationSession) Status() string                  { return "active" }
func (*simulationSession) StartedAt() time.Time            { return time.Time{} }
func (*simulationSession) UpdatedAt() time.Time            { return time.Time{} }
func (*simulationSession) LastMessageAt() time.Time        { return time.Time{} }
func (s *simulationSession) Description() string           { return s.req.InitialMessage }
func (*simulationSession) Cancel()                         {}
