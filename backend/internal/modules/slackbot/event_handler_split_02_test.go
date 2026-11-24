package slackbot

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"

	"github.com/takutakahashi/agentapi-proxy/internal/domain/entities"
)

func TestProcessEvent_NonEventCallbackIgnored(t *testing.T) {
	repo := newMockSlackBotRepository()
	sessionMgr := &mockSessionManager{}
	handler := NewSlackBotEventHandler(repo, sessionMgr, "", "", nil, "", false, nil)

	payload := SlackPayload{Type: "url_verification", Event: nil}
	err := handler.ProcessEvent(context.Background(), "default", payload)
	require.NoError(t, err)
	assert.Equal(t, 0, sessionMgr.createdCount(), "url_verification should be ignored")
}

// TestProcessEvent_BasicEvent_CreatesSession verifies that a basic message event causes
// a session to be created asynchronously.
func TestProcessEvent_BasicEvent_CreatesSession(t *testing.T) {
	const (
		botID     = "basic-bot-uuid"
		channelID = "C-basic"
	)

	repo := newMockSlackBotRepository()
	bot := entities.NewSlackBot(botID, "Basic Bot", "user-1")
	repo.bots[botID] = bot

	sessionMgr := &mockSessionManager{}
	handler := NewSlackBotEventHandler(repo, sessionMgr, "", "", nil, "", false, nil)

	payload := buildEventPayload(channelID, "hello bot")
	err := handler.ProcessEvent(context.Background(), botID, payload)
	require.NoError(t, err)

	ok := waitForCondition(2*time.Second, 10*time.Millisecond, func() bool {
		return sessionMgr.createdCount() == 1
	})
	require.True(t, ok, "session should be created asynchronously")

	createdTags := sessionMgr.getCreatedSession(0).tags
	assert.Equal(t, botID, createdTags["slackbot_id"])
	assert.Equal(t, channelID, createdTags["slack_channel"])
}

// TestProcessEvent_PausedBot_Skipped verifies that a paused bot causes the event to be ignored.
func TestProcessEvent_PausedBot_Skipped(t *testing.T) {
	const botID = "paused-bot-uuid"

	repo := newMockSlackBotRepository()
	bot := entities.NewSlackBot(botID, "Paused Bot", "user-1")
	bot.SetStatus(entities.SlackBotStatusPaused)
	repo.bots[botID] = bot

	sessionMgr := &mockSessionManager{}
	handler := NewSlackBotEventHandler(repo, sessionMgr, "", "", nil, "", false, nil)

	payload := buildEventPayload("C-paused", "hello")
	err := handler.ProcessEvent(context.Background(), botID, payload)
	require.NoError(t, err)

	// Give goroutine a chance (should never fire for paused bot)
	time.Sleep(50 * time.Millisecond)
	assert.Equal(t, 0, sessionMgr.createdCount(), "no session should be created for paused bot")
}

// TestProcessEvent_EventTypeNotAllowed verifies that events with disallowed types are ignored.
func TestProcessEvent_EventTypeNotAllowed(t *testing.T) {
	const botID = "event-filter-bot"

	repo := newMockSlackBotRepository()
	bot := entities.NewSlackBot(botID, "Event Filter Bot", "user-1")
	bot.SetAllowedEventTypes([]string{"app_mention"}) // only app_mention; "message" should be filtered
	repo.bots[botID] = bot

	sessionMgr := &mockSessionManager{}
	handler := NewSlackBotEventHandler(repo, sessionMgr, "", "", nil, "", false, nil)

	payload := buildEventPayload("C-filter", "hello")
	// payload.Event.Type is "message", which is not in allowed list
	err := handler.ProcessEvent(context.Background(), botID, payload)
	require.NoError(t, err)

	time.Sleep(50 * time.Millisecond)
	assert.Equal(t, 0, sessionMgr.createdCount(), "disallowed event type should be filtered")
}

// TestProcessEvent_RegisteredBotTaggedCorrectly verifies that session tags contain
// the bot's UUID and proper scope/teamID from the registered bot.
func TestProcessEvent_RegisteredBotTaggedCorrectly(t *testing.T) {
	const (
		botID     = "registered-bot-uuid"
		channelID = "C-tagged"
	)

	repo := newMockSlackBotRepository()
	bot := entities.NewSlackBot(botID, "Tagged Bot", "user-1")
	bot.SetScope(entities.ScopeTeam)
	bot.SetTeamID("myorg/dev-team")
	repo.bots[botID] = bot

	sessionMgr := &mockSessionManager{}
	handler := NewSlackBotEventHandler(repo, sessionMgr, "", "", nil, "", false, nil)

	payload := buildEventPayload(channelID, "hello")
	err := handler.ProcessEvent(context.Background(), botID, payload)
	require.NoError(t, err)

	ok := waitForCondition(2*time.Second, 10*time.Millisecond, func() bool {
		return sessionMgr.createdCount() == 1
	})
	require.True(t, ok, "session should be created")

	sess := sessionMgr.getCreatedSession(0)
	assert.Equal(t, botID, sess.tags["slackbot_id"])
	assert.Equal(t, channelID, sess.tags["slack_channel"])
	assert.Equal(t, entities.ScopeTeam, sess.scope)
}

// TestProcessEvent_DefaultID_ResolveBotByChannel_UsesCorrectBotID verifies that when
// ProcessEvent is called with botID="default", it identifies the correct registered bot
// by channel name and tags the session with the bot's UUID.
func TestProcessEvent_DefaultID_ResolveBotByChannel_UsesCorrectBotID(t *testing.T) {
	const (
		namespace  = "test-ns"
		secretName = "bot-token-secret"
		channelID  = "C-dev-ch"
	)

	fakeClient := fake.NewSimpleClientset(
		&corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{Name: secretName, Namespace: namespace},
			Data:       map[string][]byte{"bot-token": []byte("xoxb-test")},
		},
		&corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{Name: "agentapi-slack-channel-cache", Namespace: namespace},
			Data:       map[string]string{channelID: "dev-alerts"},
		},
	)
	resolver := NewSlackChannelResolver(fakeClient, namespace)

	repo := newMockSlackBotRepository()
	bot := entities.NewSlackBot("registered-bot-uuid", "Dev Bot", "user-1")
	bot.SetAllowedChannelNames([]string{"dev"}) // partial match: "dev" ⊆ "dev-alerts"
	bot.SetScope(entities.ScopeTeam)
	bot.SetTeamID("myorg/dev-team")
	repo.bots["registered-bot-uuid"] = bot

	sessionMgr := &mockSessionManager{}
	handler := NewSlackBotEventHandler(repo, sessionMgr, secretName, "bot-token", resolver, "", false, nil)

	payload := buildEventPayload(channelID, "hello bot")
	err := handler.ProcessEvent(context.Background(), "default", payload)
	require.NoError(t, err)

	ok := waitForCondition(2*time.Second, 10*time.Millisecond, func() bool {
		return sessionMgr.createdCount() == 1
	})
	require.True(t, ok, "session should be created")

	createdTags := sessionMgr.getCreatedSession(0).tags
	assert.Equal(t, "registered-bot-uuid", createdTags["slackbot_id"],
		"session must be tagged with the registered bot's UUID, not 'default'")
}

func TestProcessEvent_DefaultID_ReusedThreadSkipsBotDiscovery(t *testing.T) {
	repo := newMockSlackBotRepository()
	bot := entities.NewSlackBot("registered-bot-uuid", "Existing Bot", "user-1")
	repo.bots[bot.ID()] = bot
	sessionMgr := &mockSessionManager{existingSessions: []entities.Session{&mockSession{
		id: "existing-session",
		tags: map[string]string{
			"slackbot_id":     bot.ID(),
			"slack_channel":   "C-existing",
			"slack_thread_ts": "111.222",
		},
	}}}
	handler := NewSlackBotEventHandler(repo, sessionMgr, "", "", nil, "", false, nil)
	payload := buildEventPayload("C-existing", "follow up")
	payload.Event.ThreadTs = "111.222"

	require.NoError(t, handler.ProcessEvent(context.Background(), slackBotDefaultID, payload))
	require.True(t, waitForCondition(2*time.Second, 10*time.Millisecond, func() bool {
		return sessionMgr.createdCount() == 1
	}))
	assert.Equal(t, 0, repo.listAllCalls, "reused threads must not enumerate every SlackBot")
	assert.Equal(t, bot.ID(), sessionMgr.getCreatedSession(0).tags["slackbot_id"])
}

// TestProcessEvent_DefaultID_NoBotMatch_DropsEvent verifies that when no registered bot
// matches the channel, the event is dropped (no session is created).
func TestProcessEvent_DefaultID_NoBotMatch_DropsEvent(t *testing.T) {
	const (
		namespace  = "test-ns"
		secretName = "bot-token-secret"
		channelID  = "C-other"
	)

	fakeClient := fake.NewSimpleClientset(
		&corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{Name: secretName, Namespace: namespace},
			Data:       map[string][]byte{"bot-token": []byte("xoxb-test")},
		},
		&corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{Name: "agentapi-slack-channel-cache", Namespace: namespace},
			Data:       map[string]string{channelID: "general"},
		},
	)
	resolver := NewSlackChannelResolver(fakeClient, namespace)

	repo := newMockSlackBotRepository()
	// Bot only allows "dev-alerts", not "general" → no match
	bot := entities.NewSlackBot("dev-bot-uuid", "Dev Bot", "user-1")
	bot.SetAllowedChannelNames([]string{"dev-alerts"})
	repo.bots["dev-bot-uuid"] = bot

	sessionMgr := &mockSessionManager{}
	handler := NewSlackBotEventHandler(repo, sessionMgr, secretName, "bot-token", resolver, "", false, nil)

	payload := buildEventPayload(channelID, "hello")
	err := handler.ProcessEvent(context.Background(), "default", payload)
	require.NoError(t, err)

	// No session should be created when no registered bot matches the channel.
	time.Sleep(50 * time.Millisecond)
	assert.Equal(t, 0, sessionMgr.createdCount(),
		"event should be dropped when no registered bot matches the channel")
}

// TestProcessEvent_ThreadTs_UsedAsThreadKey verifies that when thread_ts is present,
// it is used as the slack_thread_ts tag instead of ts.
func TestProcessEvent_ThreadTs_UsedAsThreadKey(t *testing.T) {
	const (
		botID     = "thread-bot-uuid"
		channelID = "C-thread"
	)

	repo := newMockSlackBotRepository()
	bot := entities.NewSlackBot(botID, "Thread Bot", "user-1")
	repo.bots[botID] = bot

	sessionMgr := &mockSessionManager{}
	handler := NewSlackBotEventHandler(repo, sessionMgr, "", "", nil, "", false, nil)

	payload := SlackPayload{
		Type:   "event_callback",
		TeamID: "T1",
		Event: &SlackEvent{
			Type:     "message",
			Text:     "reply in thread",
			User:     "U1",
			Channel:  channelID,
			Ts:       "200.001",
			ThreadTs: "100.000", // reply to thread root
		},
	}

	err := handler.ProcessEvent(context.Background(), botID, payload)
	require.NoError(t, err)

	ok := waitForCondition(2*time.Second, 10*time.Millisecond, func() bool {
		return sessionMgr.createdCount() == 1
	})
	require.True(t, ok, "session should be created")

	createdTags := sessionMgr.getCreatedSession(0).tags
	assert.Equal(t, "100.000", createdTags["slack_thread_ts"],
		"thread_ts should be used as the thread key when present")
}

// TestProcessEvent_SessionLimit_Reached verifies that the limit is delegated to
// /start, where reuse is evaluated before rejecting a new session.
func TestProcessEvent_SessionLimit_Reached(t *testing.T) {
	const botID = "limited-bot"

	repo := newMockSlackBotRepository()
	bot := entities.NewSlackBot(botID, "Limited Bot", "user-1")
	bot.SetMaxSessions(2)
	repo.bots[botID] = bot

	// Pre-seed 2 sessions (at the limit). Note: these sessions must NOT include
	// the slack_channel/slack_thread_ts tags so the duplicate-session check passes first.
	existingSessions := []entities.Session{
		&mockSession{id: "s1", tags: map[string]string{"slackbot_id": botID}},
		&mockSession{id: "s2", tags: map[string]string{"slackbot_id": botID}},
	}
	sessionMgr := &mockSessionManager{existingSessions: existingSessions}
	handler := NewSlackBotEventHandler(repo, sessionMgr, "", "", nil, "", false, nil)

	payload := buildEventPayload("C-limited", "hello")
	err := handler.ProcessEvent(context.Background(), botID, payload)
	require.NoError(t, err)
	require.True(t, waitForCondition(2*time.Second, 10*time.Millisecond, func() bool {
		return sessionMgr.createdCount() == 1
	}))
	created := sessionMgr.getCreatedSession(0)
	assert.Equal(t, 2, created.maxSessions)
	assert.Equal(t, map[string]string{"slackbot_id": botID}, created.limitMatchTags)
}

// TestProcessEvent_BotMessage_BotID_Ignored verifies that events with a non-empty bot_id
// (i.e. messages posted by bots) are silently ignored to prevent recursive session creation.
func TestProcessEvent_BotMessage_BotID_Ignored(t *testing.T) {
	repo := newMockSlackBotRepository()
	sessionMgr := &mockSessionManager{}
	handler := NewSlackBotEventHandler(repo, sessionMgr, "", "", nil, "", false, nil)
	payload := SlackPayload{
		Type:   "event_callback",
		TeamID: "T1",
		Event: &SlackEvent{
			Type:    "message",
			BotID:   "B-some-bot-id", // bot-posted message
			Text:    "セッションを作成しました :robot_face:\nhttp://example.com/sessions/abc",
			Channel: "C-channel",
			Ts:      "111.222",
		},
	}

	err := handler.ProcessEvent(context.Background(), "default", payload)
	require.NoError(t, err)

	time.Sleep(50 * time.Millisecond)
	assert.Equal(t, 0, sessionMgr.createdCount(), "bot message (bot_id set) must be ignored to prevent recursion")
}

// TestProcessEvent_BotMessage_Subtype_Ignored verifies that events with subtype="bot_message"
// are silently ignored even when bot_id is empty.
func TestProcessEvent_BotMessage_Subtype_Ignored(t *testing.T) {
	repo := newMockSlackBotRepository()
	sessionMgr := &mockSessionManager{}
	handler := NewSlackBotEventHandler(repo, sessionMgr, "", "", nil, "", false, nil)
	payload := SlackPayload{
		Type:   "event_callback",
		TeamID: "T1",
		Event: &SlackEvent{
			Type:    "message",
			SubType: "bot_message", // bot_message subtype
			Text:    "some bot reply",
			Channel: "C-channel",
			Ts:      "111.333",
		},
	}

	err := handler.ProcessEvent(context.Background(), "default", payload)
	require.NoError(t, err)

	time.Sleep(50 * time.Millisecond)
	assert.Equal(t, 0, sessionMgr.createdCount(), "bot_message subtype must be ignored to prevent recursion")
}

// TestProcessEvent_PausedBot_NeverResponds verifies that a paused bot never responds to any event,
// even when allow_bot_messages=true. The paused state is a hard guardrail.
