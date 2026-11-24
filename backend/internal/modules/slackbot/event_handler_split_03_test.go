package slackbot

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"

	"github.com/takutakahashi/agentapi-proxy/internal/domain/entities"
)

func TestProcessEvent_PausedBot_NeverResponds(t *testing.T) {
	const botID = "paused-allow-bot-messages-uuid"
	repo := newMockSlackBotRepository()
	bot := entities.NewSlackBot(botID, "Paused Allow Bot", "user-1")
	allowBotMessages := true
	bot.SetAllowBotMessages(&allowBotMessages)
	bot.SetStatus(entities.SlackBotStatusPaused)
	repo.bots[botID] = bot

	sessionMgr := &mockSessionManager{}
	handler := NewSlackBotEventHandler(repo, sessionMgr, "", "", nil, "", false, nil)

	// Test 1: bot message (allow_bot_messages=true, but paused → must NOT respond)
	payload := SlackPayload{
		Type:   "event_callback",
		TeamID: "T1",
		Event: &SlackEvent{
			Type:    "app_mention",
			BotID:   "B-some-bot-id",
			Text:    "<@U-bot> hello",
			Channel: "C-channel",
			Ts:      "300.001",
		},
	}
	err := handler.ProcessEvent(context.Background(), botID, payload)
	require.NoError(t, err)
	time.Sleep(50 * time.Millisecond)
	assert.Equal(t, 0, sessionMgr.createdCount(), "paused bot must never respond, even when allow_bot_messages=true")

	// Test 2: human message (paused → must NOT respond)
	payload2 := SlackPayload{
		Type:   "event_callback",
		TeamID: "T1",
		Event: &SlackEvent{
			Type:    "app_mention",
			Text:    "<@U-bot> hello from human",
			User:    "U-human",
			Channel: "C-channel",
			Ts:      "300.002",
		},
	}
	err = handler.ProcessEvent(context.Background(), botID, payload2)
	require.NoError(t, err)
	time.Sleep(50 * time.Millisecond)
	assert.Equal(t, 0, sessionMgr.createdCount(), "paused bot must never respond to human messages either")
}

// TestProcessEvent_BotMessage_AllowBotMessages_BotID verifies that when allow_bot_messages=true,
// messages with a non-empty bot_id are processed and a session is created.
func TestProcessEvent_BotMessage_AllowBotMessages_BotID(t *testing.T) {
	const botID = "allow-bot-messages-bot-uuid"
	repo := newMockSlackBotRepository()
	bot := entities.NewSlackBot(botID, "Allow Bot Messages Bot", "user-1")
	allowBotMessages := true
	bot.SetAllowBotMessages(&allowBotMessages)
	repo.bots[botID] = bot

	sessionMgr := &mockSessionManager{}
	handler := NewSlackBotEventHandler(repo, sessionMgr, "", "", nil, "", false, nil)

	payload := SlackPayload{
		Type:   "event_callback",
		TeamID: "T1",
		Event: &SlackEvent{
			Type:    "app_mention",
			BotID:   "B-some-bot-id", // bot-posted message
			Text:    "<@U-bot> こんにちは",
			Channel: "C-channel",
			Ts:      "200.001",
		},
	}

	err := handler.ProcessEvent(context.Background(), botID, payload)
	require.NoError(t, err)

	time.Sleep(50 * time.Millisecond)
	assert.Equal(t, 1, sessionMgr.createdCount(), "bot message must be processed when allow_bot_messages=true")
}

// TestProcessEvent_BotMessage_AllowBotMessages_Subtype verifies that when allow_bot_messages=true,
// messages with subtype="bot_message" are processed and a session is created.
func TestProcessEvent_BotMessage_AllowBotMessages_Subtype(t *testing.T) {
	const botID = "allow-bot-messages-subtype-uuid"
	repo := newMockSlackBotRepository()
	bot := entities.NewSlackBot(botID, "Allow Bot Messages Subtype Bot", "user-1")
	allowBotMessages := true
	bot.SetAllowBotMessages(&allowBotMessages)
	repo.bots[botID] = bot

	sessionMgr := &mockSessionManager{}
	handler := NewSlackBotEventHandler(repo, sessionMgr, "", "", nil, "", false, nil)

	payload := SlackPayload{
		Type:   "event_callback",
		TeamID: "T1",
		Event: &SlackEvent{
			Type:    "message",
			SubType: "bot_message",
			Text:    "bot からのメッセージ",
			Channel: "C-channel",
			Ts:      "200.002",
		},
	}

	err := handler.ProcessEvent(context.Background(), botID, payload)
	require.NoError(t, err)

	time.Sleep(50 * time.Millisecond)
	assert.Equal(t, 1, sessionMgr.createdCount(), "bot_message subtype must be processed when allow_bot_messages=true")
}

// TestProcessEvent_ReuseSession_RoutesToExistingSession verifies that Slack delegates
// tag matching and follow-up delivery to the authoritative /start API.
func TestProcessEvent_ReuseSession_RoutesToExistingSession(t *testing.T) {
	const (
		botID     = "reuse-bot-uuid"
		channelID = "C-reuse"
		threadTS  = "600.000"
	)

	repo := newMockSlackBotRepository()
	bot := entities.NewSlackBot(botID, "Reuse Bot", "user-1")
	repo.bots[botID] = bot

	// Pre-seed an existing active session for the same channel+thread
	existingSessions := []entities.Session{
		&mockSession{
			id:     "existing-session-id",
			status: "active",
			tags: map[string]string{
				"slack_channel":     channelID,
				"slack_thread_ts":   threadTS,
				"triggered_user_id": "",
			},
		},
	}
	sessionMgr := &mockSessionManager{existingSessions: existingSessions}
	handler := NewSlackBotEventHandler(repo, sessionMgr, "", "", nil, "", false, nil)
	payload := SlackPayload{
		Type:   "event_callback",
		TeamID: "T1",
		Event: &SlackEvent{
			Type:     "message",
			Text:     "follow-up message",
			User:     "U1",
			Channel:  channelID,
			Ts:       "600.001",
			ThreadTs: threadTS,
		},
	}

	err := handler.ProcessEvent(context.Background(), botID, payload)
	require.NoError(t, err)

	require.True(t, waitForCondition(2*time.Second, 10*time.Millisecond, func() bool {
		return sessionMgr.createdCount() == 1
	}))
	created := sessionMgr.getCreatedSession(0)
	assert.Equal(t, "follow-up message", created.reuseMessage)
	assert.True(t, created.stopBeforeReuse, "Slack reuse should interrupt a running turn before prompting")
	assert.Equal(t, map[string]string{
		"slackbot_id": botID, "slack_channel": channelID,
		"slack_thread_ts": threadTS, "triggered_user_id": "",
	}, created.reuseMatchTags)
	assert.Equal(t, 0, sessionMgr.sentCount(), "worker must not use the legacy message API")
}

// TestProcessEvent_ReuseSession_RunningSession verifies the normal production state:
// once the agent starts, the session status is "running" rather than "active".
func TestProcessEvent_ReuseSession_RunningSession(t *testing.T) {
	const (
		botID     = "reuse-running-bot-uuid"
		channelID = "C-reuse-running"
		threadTS  = "601.000"
	)

	repo := newMockSlackBotRepository()
	bot := entities.NewSlackBot(botID, "Reuse Running Bot", "user-1")
	repo.bots[botID] = bot

	existingSessions := []entities.Session{
		&mockSession{
			id:     "running-session-id",
			status: "running",
			tags: map[string]string{
				"slack_channel":     channelID,
				"slack_thread_ts":   threadTS,
				"triggered_user_id": "",
			},
		},
	}
	sessionMgr := &mockSessionManager{existingSessions: existingSessions}
	handler := NewSlackBotEventHandler(repo, sessionMgr, "", "", nil, "", false, nil)
	payload := SlackPayload{
		Type: "event_callback",
		Event: &SlackEvent{
			Type:     "app_mention",
			Text:     "<@UBOT> follow-up in thread",
			User:     "U1",
			Channel:  channelID,
			Ts:       "601.001",
			ThreadTs: threadTS,
		},
	}

	require.NoError(t, handler.ProcessEvent(context.Background(), botID, payload))
	require.True(t, waitForCondition(2*time.Second, 10*time.Millisecond, func() bool {
		return sessionMgr.createdCount() == 1
	}))
	created := sessionMgr.getCreatedSession(0)
	assert.Equal(t, "<@UBOT> follow-up in thread", created.reuseMessage)
	assert.Equal(t, botID, created.reuseMatchTags["slackbot_id"])
	assert.Equal(t, 0, sessionMgr.sentCount())
}

// TestProcessEvent_ReuseSession_NewSessionWhenNoActive verifies that a new session is created
// when no active session exists for the channel+thread, even if terminated sessions exist.
func TestProcessEvent_ReuseSession_NewSessionWhenNoActive(t *testing.T) {
	const (
		botID     = "newthread-bot-uuid"
		channelID = "C-newthread"
	)

	repo := newMockSlackBotRepository()
	bot := entities.NewSlackBot(botID, "NewThread Bot", "user-1")
	repo.bots[botID] = bot

	// No pre-seeded sessions → should always create a new session
	sessionMgr := &mockSessionManager{}
	handler := NewSlackBotEventHandler(repo, sessionMgr, "", "", nil, "", false, nil)
	payload := buildEventPayload(channelID, "first message")
	err := handler.ProcessEvent(context.Background(), botID, payload)
	require.NoError(t, err)

	ok := waitForCondition(2*time.Second, 10*time.Millisecond, func() bool {
		return sessionMgr.createdCount() == 1
	})
	require.True(t, ok, "new session should be created when no active session exists")
	assert.Equal(t, 0, sessionMgr.sentCount(), "no message should be sent to existing session")
}

// TestProcessEvent_ChannelNameResolveError_ReturnsError verifies that when channel name
// resolution fails (e.g. the bot token is empty/invalid), ProcessEvent returns an error
// instead of allowing the event through as it did previously.
func TestProcessEvent_ChannelNameResolveError_ReturnsError(t *testing.T) {
	const (
		namespace  = "test-ns"
		secretName = "bot-token-secret"
		botID      = "channel-filter-bot"
		channelID  = "C-unknown"
	)

	// Secret exists but has an empty token value.
	// GetBotToken will succeed returning "", then fetchFromSlack fails with
	// "bot token is empty; cannot call Slack API" — no real HTTP call is made.
	fakeClient := fake.NewSimpleClientset(
		&corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{Name: secretName, Namespace: namespace},
			Data:       map[string][]byte{"bot-token": []byte("")},
		},
		// No ConfigMap → cache miss → falls through to Slack API (which fails)
	)
	resolver := NewSlackChannelResolver(fakeClient, namespace)

	repo := newMockSlackBotRepository()
	bot := entities.NewSlackBot(botID, "Channel Filter Bot", "user-1")
	bot.SetAllowedChannelNames([]string{"dev"})
	// No custom bot token secret → uses defaultBotTokenSecretName
	repo.bots[botID] = bot

	sessionMgr := &mockSessionManager{}
	handler := NewSlackBotEventHandler(repo, sessionMgr, secretName, "bot-token", resolver, "", false, nil)

	payload := buildEventPayload(channelID, "hello bot")
	err := handler.ProcessEvent(context.Background(), botID, payload)

	assert.Error(t, err, "ProcessEvent should return error when channel name cannot be resolved")
	assert.Contains(t, err.Error(), "failed to resolve channel name")

	// Ensure no session was created despite the resolve failure
	time.Sleep(50 * time.Millisecond)
	assert.Equal(t, 0, sessionMgr.createdCount(), "no session should be created when channel name resolution fails")
}

// TestProcessEvent_ConcurrentDuplicateEvents verifies that when Slack emits both "message"
// and "app_mention" for the same @mention (same channel+thread), only one session is created.
// This mirrors real Slack behaviour where both events arrive within milliseconds of each other,
// before any session exists, so the reuse-check alone cannot catch the duplicate.
func TestProcessEvent_ConcurrentDuplicateEvents(t *testing.T) {
	const (
		botID     = "concurrent-bot-uuid"
		channelID = "C-concurrent"
		threadTS  = "700.000"
	)

	repo := newMockSlackBotRepository()
	bot := entities.NewSlackBot(botID, "Concurrent Bot", "user-1")
	repo.bots[botID] = bot

	// Keep creation in flight while both duplicate callbacks are handled. The exact-event
	// dedup key (bot+channel+timestamp) must suppress one callback before it is queued.
	sessionMgr := &mockSessionManager{createDelay: 50 * time.Millisecond}
	handler := NewSlackBotEventHandler(repo, sessionMgr, "", "", nil, "", false, nil)
	makePayload := func(eventType string) SlackPayload {
		return SlackPayload{
			Type:   "event_callback",
			TeamID: "T1",
			Event: &SlackEvent{
				Type:    eventType,
				Text:    "@bot hello",
				User:    "U1",
				Channel: channelID,
				Ts:      threadTS,
			},
		}
	}

	// Use a start barrier to ensure both goroutines reach ProcessEvent simultaneously,
	// before the first one's async session creation goroutine can complete.
	var ready, start sync.WaitGroup
	ready.Add(2)
	start.Add(1)

	var wg sync.WaitGroup
	wg.Add(2)

	// Fire "message" and "app_mention" concurrently, simulating Slack's duplicate delivery
	go func() {
		defer wg.Done()
		ready.Done()
		start.Wait()
		_ = handler.ProcessEvent(context.Background(), botID, makePayload("message"))
	}()
	go func() {
		defer wg.Done()
		ready.Done()
		start.Wait()
		_ = handler.ProcessEvent(context.Background(), botID, makePayload("app_mention"))
	}()

	ready.Wait() // wait until both goroutines are staged
	start.Done() // release both simultaneously

	wg.Wait()

	ok := waitForCondition(2*time.Second, 10*time.Millisecond, func() bool {
		return sessionMgr.createdCount() == 1
	})
	require.True(t, ok, "exactly one session should be created even when two events fire concurrently")
}

// TestProcessEvent_DistinctMessagesInSameThread_AreQueued verifies that a follow-up
// arriving while session creation/reuse is in flight is not mistaken for a duplicate.
// The two distinct Slack timestamps must both be processed in arrival order.
