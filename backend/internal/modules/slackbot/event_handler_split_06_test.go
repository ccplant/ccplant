package slackbot

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/takutakahashi/agentapi-proxy/internal/domain/entities"
)

func TestProcessEvent_ConfiguredRepo_UsedWhenNoMessageRepo(t *testing.T) {
	const (
		botID     = "config-repo-bot"
		channelID = "C-configRepo"
	)

	repo := newMockSlackBotRepository()
	bot := entities.NewSlackBot(botID, "Config Repo Bot", "user-1")

	// Configure a static repository in session params.
	sc := entities.NewWebhookSessionConfig()
	sc.SetParams(&entities.SessionParams{
		RepoFullName: "myorg/configured-repo",
		Pool:         "pool-a",
		ManagerID:    "manager-a",
	})
	bot.SetSessionConfig(sc)
	repo.bots[botID] = bot

	sessionMgr := &mockSessionManager{}
	handler := NewSlackBotEventHandler(repo, sessionMgr, "", "", nil, "", false, nil)

	// Message does NOT contain an org/repo identifier.
	payload := buildEventPayload(channelID, "バグを修正してください")
	err := handler.ProcessEvent(context.Background(), botID, payload)
	require.NoError(t, err)

	ok := waitForCondition(2*time.Second, 10*time.Millisecond, func() bool {
		return sessionMgr.createdCount() == 1
	})
	require.True(t, ok, "session should be created")

	sess := sessionMgr.getCreatedSession(0)
	assert.Equal(t, "myorg/configured-repo", sess.tags["repository"],
		"repository tag should be set from bot session_config.params.repo_full_name")
	require.NotNil(t, sess.repoInfo, "RepoInfo should be set from configured repo")
	assert.Equal(t, "myorg/configured-repo", sess.repoInfo.FullName)
	assert.Equal(t, "/home/agentapi/workdir/repo", sess.repoInfo.CloneDir)
	assert.Equal(t, "pool-a", sess.pool)
	assert.Equal(t, "manager-a", sess.managerID)
}

// TestProcessEvent_ConfiguredRepo_TakesPriorityOverMessageRepo verifies that when
// session_config.params.repo_full_name is set and the message also contains an org/repo
// identifier, the static bot config takes priority.
func TestProcessEvent_ConfiguredRepo_TakesPriorityOverMessageRepo(t *testing.T) {
	const (
		botID     = "config-repo-priority-bot"
		channelID = "C-priority"
	)

	repo := newMockSlackBotRepository()
	bot := entities.NewSlackBot(botID, "Config Repo Priority Bot", "user-1")

	// Configure a static repository in session params.
	sc := entities.NewWebhookSessionConfig()
	sc.SetParams(&entities.SessionParams{
		RepoFullName: "myorg/configured-repo",
	})
	bot.SetSessionConfig(sc)
	repo.bots[botID] = bot

	sessionMgr := &mockSessionManager{}
	handler := NewSlackBotEventHandler(repo, sessionMgr, "", "", nil, "", false, nil)

	// Message contains a DIFFERENT org/repo identifier.
	payload := buildEventPayload(channelID, "other-org/other-repo\nPlease fix this.")
	err := handler.ProcessEvent(context.Background(), botID, payload)
	require.NoError(t, err)

	ok := waitForCondition(2*time.Second, 10*time.Millisecond, func() bool {
		return sessionMgr.createdCount() == 1
	})
	require.True(t, ok, "session should be created")

	sess := sessionMgr.getCreatedSession(0)
	assert.Equal(t, "myorg/configured-repo", sess.tags["repository"],
		"configured repo_full_name should take priority over message-detected repository")
	require.NotNil(t, sess.repoInfo, "RepoInfo should be set from configured repo")
	assert.Equal(t, "myorg/configured-repo", sess.repoInfo.FullName)
}

// TestProcessEvent_NoConfiguredRepo_FallsBackToMessageDetection verifies that when
// repo_full_name is NOT configured in session params, the message auto-detection still works.
func TestProcessEvent_NoConfiguredRepo_FallsBackToMessageDetection(t *testing.T) {
	const (
		botID     = "no-config-repo-bot"
		channelID = "C-noConfigRepo"
	)

	repo := newMockSlackBotRepository()
	bot := entities.NewSlackBot(botID, "No Config Repo Bot", "user-1")

	// Configure session params WITHOUT repo_full_name.
	sc := entities.NewWebhookSessionConfig()
	sc.SetParams(&entities.SessionParams{
		AgentType: "claude-acp",
	})
	bot.SetSessionConfig(sc)
	repo.bots[botID] = bot

	sessionMgr := &mockSessionManager{}
	handler := NewSlackBotEventHandler(repo, sessionMgr, "", "", nil, "", false, nil)

	// Message contains an org/repo identifier.
	payload := buildEventPayload(channelID, "myorg/detected-repo\nPlease fix this.")
	err := handler.ProcessEvent(context.Background(), botID, payload)
	require.NoError(t, err)

	ok := waitForCondition(2*time.Second, 10*time.Millisecond, func() bool {
		return sessionMgr.createdCount() == 1
	})
	require.True(t, ok, "session should be created")

	sess := sessionMgr.getCreatedSession(0)
	assert.Equal(t, "myorg/detected-repo", sess.tags["repository"],
		"message-detected repository should be used when repo_full_name is not configured")
	require.NotNil(t, sess.repoInfo, "RepoInfo should be set from message-detected repo")
	assert.Equal(t, "myorg/detected-repo", sess.repoInfo.FullName)
}

func TestProcessEvent_ForwardsSlackCredentials(t *testing.T) {
	for _, tc := range []struct {
		name, customName, customKey, wantName, wantKey string
	}{
		{"worker default", "", "", "worker-slack", "bot-token"},
		{"custom token", "custom-slack", "custom-token", "custom-slack", "custom-token"},
		{"custom name with default key", "custom-slack", "", "custom-slack", "bot-token"},
		{"custom key with default name", "", "custom-token", "worker-slack", "custom-token"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			repo := newMockSlackBotRepository()
			bot := entities.NewSlackBot("bot", "Bot", "user")
			bot.SetBotTokenSecretName(tc.customName)
			bot.SetBotTokenSecretKey(tc.customKey)
			repo.bots[bot.ID()] = bot
			manager := &mockSessionManager{}
			handler := NewSlackBotEventHandler(repo, manager, "worker-slack", "worker-token", nil, "", false, nil)
			payload := buildEventPayload("channel", "hello")
			payload.Event.ThreadTs = "123.456"
			require.NoError(t, handler.ProcessEvent(context.Background(), bot.ID(), payload))
			require.Eventually(t, func() bool { return manager.createdCount() == 1 }, 2*time.Second, 10*time.Millisecond)
			params := manager.getCreatedSession(0).slackParams
			require.NotNil(t, params)
			assert.Equal(t, "channel", params.Channel)
			assert.Equal(t, "123.456", params.ThreadTS)
			assert.Equal(t, tc.wantName, params.BotTokenSecretName)
			assert.Equal(t, tc.wantKey, params.BotTokenSecretKey)
		})
	}
}
