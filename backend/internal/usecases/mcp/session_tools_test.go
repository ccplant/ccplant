package mcp

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/takutakahashi/agentapi-proxy/internal/domain/entities"
	"github.com/takutakahashi/agentapi-proxy/internal/usecases/ports/repositories"
)

type controllerSessionManager struct {
	sessions   []entities.Session
	lastFilter entities.SessionFilter
}

func (m *controllerSessionManager) CreateSession(context.Context, string, *entities.RunServerRequest, []byte) (entities.Session, error) {
	return nil, nil
}
func (m *controllerSessionManager) GetSession(string) entities.Session { return nil }
func (m *controllerSessionManager) ListSessions(filter entities.SessionFilter) []entities.Session {
	m.lastFilter = filter
	return m.sessions
}
func (m *controllerSessionManager) DeleteSession(string) error                        { return nil }
func (m *controllerSessionManager) SendMessage(context.Context, string, string) error { return nil }
func (m *controllerSessionManager) StopAgent(context.Context, string) error           { return nil }
func (m *controllerSessionManager) GetMessages(context.Context, string) ([]repositories.Message, error) {
	return nil, nil
}
func (m *controllerSessionManager) Shutdown(time.Duration) error { return nil }

type recordingSessionCreator struct {
	called    bool
	sessionID string
	request   entities.StartRequest
	userID    string
}

func (c *recordingSessionCreator) CreateSession(_ context.Context, sessionID string, req entities.StartRequest, userID, _ string, _ []string) (entities.Session, error) {
	c.called = true
	c.sessionID = sessionID
	c.request = req
	c.userID = userID
	return controllerSession{id: sessionID, userID: userID, tags: req.Tags}, nil
}

type controllerSession struct {
	id     string
	userID string
	tags   map[string]string
}

func (s controllerSession) ID() string                    { return s.id }
func (s controllerSession) Addr() string                  { return "" }
func (s controllerSession) UserID() string                { return s.userID }
func (s controllerSession) Scope() entities.ResourceScope { return entities.ScopeUser }
func (s controllerSession) TeamID() string                { return "" }
func (s controllerSession) Tags() map[string]string       { return s.tags }
func (s controllerSession) Status() string                { return "running" }
func (s controllerSession) StartedAt() time.Time          { return time.Time{} }
func (s controllerSession) UpdatedAt() time.Time          { return time.Time{} }
func (s controllerSession) LastMessageAt() time.Time      { return time.Time{} }
func (s controllerSession) Description() string           { return "" }
func (s controllerSession) Cancel()                       {}

func TestCreateSessionAppliesControllerBoundary(t *testing.T) {
	manager := &controllerSessionManager{}
	creator := &recordingSessionCreator{}
	uc := NewMCPSessionToolsUseCase(manager, creator, nil)

	sessionID, err := uc.CreateSession(context.Background(), &CreateSessionInput{
		UserID:           "user-1",
		Message:          "investigate the failure",
		ProfileID:        "profile-1",
		ParentSessionID:  "parent-session",
		ParentAgentID:    "agent-1",
		MaxChildSessions: 4,
		Scope:            entities.ScopeTeam,
		TeamID:           "team-1",
		Tags:             map[string]string{"task": "investigation"},
	})

	require.NoError(t, err)
	require.NotEmpty(t, sessionID)
	require.True(t, creator.called)
	require.Equal(t, "user-1", creator.userID)
	require.Equal(t, entities.ScopeTeam, creator.request.Scope)
	require.Equal(t, "team-1", creator.request.TeamID)
	require.Equal(t, "profile-1", creator.request.SessionProfileID)
	require.Equal(t, "investigate the failure", creator.request.Params.Message)
	require.Equal(t, "agent-1", creator.request.Tags["parent_agent_id"])
	require.Equal(t, "parent-session", creator.request.Tags["parent_session_id"])
	require.Equal(t, "worker", creator.request.Tags["session_role"])
	require.Equal(t, "user-1", creator.request.Tags["user_id"])
	require.Equal(t, "agent-1", manager.lastFilter.Tags["parent_agent_id"])
}

func TestCreateSessionRejectsWhenControllerReachesChildLimit(t *testing.T) {
	manager := &controllerSessionManager{sessions: []entities.Session{nil, nil}}
	creator := &recordingSessionCreator{}
	uc := NewMCPSessionToolsUseCase(manager, creator, nil)

	_, err := uc.CreateSession(context.Background(), &CreateSessionInput{
		ParentAgentID:    "agent-1",
		MaxChildSessions: 2,
	})

	require.EqualError(t, err, "child session limit reached: maximum 2 sessions")
	require.False(t, creator.called)
	require.Equal(t, map[string]string{
		"parent_agent_id": "agent-1",
		"session_role":    "worker",
	}, manager.lastFilter.Tags)
}
