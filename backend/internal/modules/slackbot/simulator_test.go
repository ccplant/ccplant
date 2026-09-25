package slackbot

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/takutakahashi/agentapi-proxy/internal/domain/entities"
)

type simulationDryRunManager struct {
	*mockSessionManager
	response map[string]interface{}
	err      error
}

func (m *simulationDryRunManager) DryRunTriggerSession(context.Context, string, *entities.RunServerRequest, []byte) (map[string]interface{}, error) {
	return m.response, m.err
}

func TestSimulateSlackBotEventBuildsOfflinePlan(t *testing.T) {
	bot := entities.NewSlackBot("bot-1", "debug", "owner")
	bot.SetAllowedEventTypes([]string{"app_mention"})
	bot.SetAllowedChannelNames([]string{"dev"})
	cfg := entities.NewWebhookSessionConfig()
	cfg.SetInitialMessageTemplate("initial: {{.event.text}} {{.thread_messages}}")
	cfg.SetReuseMessageTemplate("reuse: {{.event.text}}")
	cfg.SetTags(map[string]string{"username": "{{.event.user}}", "source": "{{.team_id}}"})
	cfg.SetEnvironment(map[string]string{"PROMPT_SOURCE": "{{.event.channel}}"})
	cfg.SetParams(&entities.SessionParams{AgentType: "codex", Model: "gpt-debug", RepoFullName: "configured/repo"})
	bot.SetSessionConfig(cfg)

	repo := newMockSlackBotRepository()
	require.NoError(t, repo.Create(t.Context(), bot))
	result := SimulateSlackBotEvent(t.Context(), repo, nil, nil, bot, SlackBotSimulationRequest{
		TeamID: "T1", ChannelName: "#dev-tools", ThreadMessages: []SlackMessage{{User: "U0", Text: "earlier context", Ts: "100.0"}},
		Event: SlackEvent{Type: "app_mention", User: "U1", Channel: "C1", Text: "other/repo investigate", Ts: "100.1", ThreadTs: "100.0"},
	})

	require.Equal(t, simulationDecisionCreateOrReuse, result.Decision)
	require.NotNil(t, result.Plan)
	assert.Equal(t, "initial: other/repo investigate [U0]: earlier context", result.Plan.InitialMessage)
	assert.Equal(t, "reuse: other/repo investigate", result.Plan.ReuseMessage)
	assert.Equal(t, "U1", result.Plan.Tags["triggered_user_id"])
	assert.Equal(t, "T1", result.Plan.Tags["source"])
	assert.Equal(t, "configured/repo", result.Plan.Repository)
	assert.Equal(t, "configured/repo", result.Plan.Tags["repository"])
	assert.Equal(t, "C1", result.Plan.Environment["PROMPT_SOURCE"])
	assert.Equal(t, "codex", result.Plan.Params.AgentType)
	assert.Equal(t, []string{"create_or_reuse_session", "post_message_to_slack"}, result.SideEffects)
}

func TestSimulateSlackBotEventIncludesStartAPIDryRunResponse(t *testing.T) {
	bot := entities.NewSlackBot("bot-1", "debug", "owner")
	repo := newMockSlackBotRepository()
	require.NoError(t, repo.Create(t.Context(), bot))
	manager := &simulationDryRunManager{mockSessionManager: &mockSessionManager{}, response: map[string]interface{}{
		"dry_run": true, "session_id": "candidate", "decision": "create",
		"placement": map[string]interface{}{"transport": "direct_runtime", "pool": "dev"},
	}}

	result := SimulateSlackBotEvent(t.Context(), repo, manager, nil, bot, SlackBotSimulationRequest{
		ChannelName: "debug",
		Event:       SlackEvent{Type: "message", Text: "investigate", User: "U1", Channel: "C1", Ts: "1"},
	})

	require.Equal(t, simulationDecisionCreateOrReuse, result.Decision)
	require.NotNil(t, result.SessionDryRun)
	assert.Equal(t, true, result.SessionDryRun["dry_run"])
	assert.Equal(t, "create", result.SessionDryRun["decision"])
}

func TestSimulateSlackBotEventCanCreateRealSession(t *testing.T) {
	bot := entities.NewSlackBot("bot-1", "debug", "owner")
	repo := newMockSlackBotRepository()
	require.NoError(t, repo.Create(t.Context(), bot))
	manager := &mockSessionManager{}
	dryRun := false

	result := SimulateSlackBotEvent(t.Context(), repo, manager, nil, bot, SlackBotSimulationRequest{
		DryRun:      &dryRun,
		ChannelName: "debug",
		Event:       SlackEvent{Type: "message", Text: "create this", User: "U1", Channel: "C1", Ts: "1"},
	})

	require.Equal(t, simulationDecisionCreateOrReuse, result.Decision)
	assert.False(t, result.DryRun)
	assert.Nil(t, result.SessionDryRun)
	require.NotNil(t, result.Session)
	assert.Equal(t, result.Plan.SessionID, result.Session.ID)
	assert.Equal(t, "active", result.Session.Status)
	assert.False(t, result.Session.Reused)
	assert.Equal(t, 1, manager.createdCount())
}

func TestSimulateSlackBotEventExplainsIgnoredAndInvalidEvents(t *testing.T) {
	bot := entities.NewSlackBot("bot-1", "debug", "owner")
	bot.SetAllowedChannelNames([]string{"production"})

	repo := newMockSlackBotRepository()
	require.NoError(t, repo.Create(t.Context(), bot))
	missingName := SimulateSlackBotEvent(t.Context(), repo, nil, nil, bot, SlackBotSimulationRequest{Event: SlackEvent{Type: "message"}})
	assert.Equal(t, simulationDecisionError, missingName.Decision)
	assert.NotEmpty(t, missingName.Errors)
	assert.Equal(t, []string{"post_message_to_slack"}, missingName.SideEffects)

	notAllowed := SimulateSlackBotEvent(t.Context(), repo, nil, nil, bot, SlackBotSimulationRequest{ChannelName: "development", Event: SlackEvent{Type: "message"}})
	assert.Equal(t, simulationDecisionIgnore, notAllowed.Decision)
	assert.Equal(t, "filtered_or_not_actionable", notAllowed.Reason)
	assert.Empty(t, notAllowed.SideEffects)
}

func TestSimulateSlackBotEventStopHasNoActualSideEffects(t *testing.T) {
	bot := entities.NewSlackBot("bot-1", "debug", "owner")
	repo := newMockSlackBotRepository()
	require.NoError(t, repo.Create(t.Context(), bot))
	result := SimulateSlackBotEvent(t.Context(), repo, nil, nil, bot, SlackBotSimulationRequest{Event: SlackEvent{Type: "message", Text: "<@UBOT> /stop"}})
	assert.Equal(t, simulationDecisionStop, result.Decision)
	assert.True(t, result.DryRun)
	assert.Equal(t, []string{"post_message_to_slack"}, result.SideEffects)
}

func TestSimulateSlackBotEventReturnsTemplateError(t *testing.T) {
	bot := entities.NewSlackBot("bot-1", "debug", "owner")
	cfg := entities.NewWebhookSessionConfig()
	cfg.SetInitialMessageTemplate("{{")
	bot.SetSessionConfig(cfg)
	repo := newMockSlackBotRepository()
	require.NoError(t, repo.Create(t.Context(), bot))
	result := SimulateSlackBotEvent(t.Context(), repo, nil, nil, bot, SlackBotSimulationRequest{Event: SlackEvent{Type: "message", Text: "hello", User: "U1", Channel: "C1", Ts: "1"}})
	// Production logs an invalid message template and falls back to the event text;
	// simulation deliberately follows that same behavior.
	assert.Equal(t, simulationDecisionCreateOrReuse, result.Decision)
	require.NotNil(t, result.Plan)
	assert.Equal(t, "hello", result.Plan.InitialMessage)
}
