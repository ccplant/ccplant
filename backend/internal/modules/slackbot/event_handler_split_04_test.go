package slackbot

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"

	"github.com/takutakahashi/agentapi-proxy/internal/domain/entities"
)

func TestProcessEvent_DistinctMessagesInSameThread_AreQueued(t *testing.T) {
	const (
		botID     = "queued-thread-bot"
		channelID = "C-queued-thread"
		threadTS  = "705.000"
	)

	repo := newMockSlackBotRepository()
	repo.bots[botID] = entities.NewSlackBot(botID, "Queued Thread Bot", "user-1")
	sessionMgr := &mockSessionManager{createDelay: 75 * time.Millisecond}
	handler := NewSlackBotEventHandler(repo, sessionMgr, "", "", nil, "", false, nil)
	makePayload := func(ts, text string) SlackPayload {
		return SlackPayload{Type: "event_callback", Event: &SlackEvent{
			Type: "app_mention", Text: text, User: "U1",
			Channel: channelID, Ts: ts, ThreadTs: threadTS,
		}}
	}

	require.NoError(t, handler.ProcessEvent(context.Background(), botID, makePayload("705.001", "first request")))
	require.NoError(t, handler.ProcessEvent(context.Background(), botID, makePayload("705.002", "second request")))

	require.True(t, waitForCondition(2*time.Second, 10*time.Millisecond, func() bool {
		return sessionMgr.createdCount() == 2
	}), "both distinct messages should be processed")
	assert.Equal(t, "first request", sessionMgr.getCreatedSession(0).initialMessage)
	assert.Equal(t, "second request", sessionMgr.getCreatedSession(1).initialMessage)
}

func TestProcessEvent_SequentialDuplicateCallbacks_CreateOneSession(t *testing.T) {
	const botID = "sequential-duplicate-bot"
	repo := newMockSlackBotRepository()
	bot := entities.NewSlackBot(botID, "Sequential Duplicate Bot", "user-1")
	repo.bots[botID] = bot
	sessionMgr := &mockSessionManager{}
	handler := NewSlackBotEventHandler(repo, sessionMgr, "", "", nil, "", false, nil)
	makePayload := func(eventType string) SlackPayload {
		return SlackPayload{Type: "event_callback", Event: &SlackEvent{
			Type: eventType, Text: "<@UBOT> hello", User: "U1",
			Channel: "C-sequential", Ts: "710.001", ThreadTs: "710.000",
		}}
	}

	require.NoError(t, handler.ProcessEvent(context.Background(), botID, makePayload("message")))
	require.True(t, waitForCondition(2*time.Second, 10*time.Millisecond, func() bool {
		return sessionMgr.createdCount() == 1
	}))
	// Simulate app_mention arriving after CreateSession returned but before the
	// session manager list/cache can expose the new session.
	require.NoError(t, handler.ProcessEvent(context.Background(), botID, makePayload("app_mention")))
	time.Sleep(50 * time.Millisecond)
	assert.Equal(t, 1, sessionMgr.createdCount())
}

func TestProcessEvent_SameMessageForDifferentBots_CreatesOneSessionPerBot(t *testing.T) {
	const firstBotID = "first-bot"
	const secondBotID = "second-bot"
	repo := newMockSlackBotRepository()
	repo.bots[firstBotID] = entities.NewSlackBot(firstBotID, "First Bot", "user-1")
	repo.bots[secondBotID] = entities.NewSlackBot(secondBotID, "Second Bot", "user-1")
	sessionMgr := &mockSessionManager{}
	handler := NewSlackBotEventHandler(repo, sessionMgr, "", "", nil, "", false, nil)
	payload := SlackPayload{Type: "event_callback", Event: &SlackEvent{
		Type: "app_mention", Text: "<@UBOT> hello", User: "U1",
		Channel: "C-shared", Ts: "720.001", ThreadTs: "720.000",
	}}

	require.NoError(t, handler.ProcessEvent(context.Background(), firstBotID, payload))
	require.NoError(t, handler.ProcessEvent(context.Background(), secondBotID, payload))
	require.True(t, waitForCondition(2*time.Second, 10*time.Millisecond, func() bool {
		return sessionMgr.createdCount() == 2
	}))
}

// ---- Tests for /stop command ----

// TestIsStopCommand verifies that isStopCommand correctly identifies /stop messages
// with or without bot mention tokens.
func TestIsStopCommand(t *testing.T) {
	cases := []struct {
		text     string
		expected bool
	}{
		{"/stop", true},
		{"  /stop  ", true},
		{"<@U1234567> /stop", true},
		{"<@U1234567|botname> /stop", true},
		{"/stop extra", false},
		{"stop", false},
		{"hello /stop", false},
		{"", false},
		{"<@U1234567>", false},
	}
	for _, tc := range cases {
		t.Run(tc.text, func(t *testing.T) {
			assert.Equal(t, tc.expected, isStopCommand(tc.text))
		})
	}
}

// TestProcessEvent_StopCommand_StopsExistingSession verifies that a /stop message
// calls StopAgent on the active session for the same channel+thread.
func TestProcessEvent_StopCommand_StopsExistingSession(t *testing.T) {
	const (
		botID     = "stop-bot-uuid"
		channelID = "C-stop"
		threadTS  = "800.000"
		sessionID = "existing-stop-session"
	)

	repo := newMockSlackBotRepository()
	bot := entities.NewSlackBot(botID, "Stop Bot", "user-1")
	repo.bots[botID] = bot

	existingSessions := []entities.Session{
		&mockSession{
			id:     sessionID,
			status: "active",
			tags: map[string]string{
				"slack_channel":   channelID,
				"slack_thread_ts": threadTS,
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
			Text:     "/stop",
			User:     "U1",
			Channel:  channelID,
			Ts:       "800.001",
			ThreadTs: threadTS,
		},
	}

	err := handler.ProcessEvent(context.Background(), botID, payload)
	require.NoError(t, err)

	ok := waitForCondition(2*time.Second, 10*time.Millisecond, func() bool {
		return sessionMgr.stoppedCount() == 1
	})
	require.True(t, ok, "StopAgent should be called for the active session")

	sessionMgr.mu.Lock()
	stoppedID := sessionMgr.stoppedSessions[0]
	sessionMgr.mu.Unlock()
	assert.Equal(t, sessionID, stoppedID, "the active session should be stopped")
	assert.Equal(t, 0, sessionMgr.createdCount(), "no new session should be created for /stop")
	assert.Equal(t, 0, sessionMgr.sentCount(), "no message should be sent to existing session for /stop")
}

func TestProcessEvent_StopCommand_StopsRunningSession(t *testing.T) {
	const (
		botID     = "stop-running-bot-uuid"
		channelID = "C-stop-running"
		threadTS  = "801.000"
		sessionID = "existing-running-session"
	)

	repo := newMockSlackBotRepository()
	bot := entities.NewSlackBot(botID, "Stop Running Bot", "user-1")
	repo.bots[botID] = bot
	sessionMgr := &mockSessionManager{existingSessions: []entities.Session{
		&mockSession{
			id:     sessionID,
			status: "running",
			tags: map[string]string{
				"slack_channel":   channelID,
				"slack_thread_ts": threadTS,
			},
		},
	}}
	handler := NewSlackBotEventHandler(repo, sessionMgr, "", "", nil, "", false, nil)
	payload := SlackPayload{
		Type: "event_callback",
		Event: &SlackEvent{
			Type:     "app_mention",
			Text:     "<@UBOTID> /stop",
			User:     "U1",
			Channel:  channelID,
			Ts:       "801.001",
			ThreadTs: threadTS,
		},
	}

	require.NoError(t, handler.ProcessEvent(context.Background(), botID, payload))
	require.True(t, waitForCondition(2*time.Second, 10*time.Millisecond, func() bool {
		return sessionMgr.stoppedCount() == 1
	}))
	assert.Equal(t, sessionID, sessionMgr.stoppedSessions[0])
}

// TestProcessEvent_StopCommand_WithMention verifies that /stop with a bot mention is also handled.
func TestProcessEvent_StopCommand_WithMention(t *testing.T) {
	const (
		botID     = "stop-mention-bot-uuid"
		channelID = "C-stop-mention"
		threadTS  = "900.000"
		sessionID = "existing-mention-session"
	)

	repo := newMockSlackBotRepository()
	bot := entities.NewSlackBot(botID, "Stop Mention Bot", "user-1")
	repo.bots[botID] = bot

	existingSessions := []entities.Session{
		&mockSession{
			id:     sessionID,
			status: "active",
			tags: map[string]string{
				"slack_channel":   channelID,
				"slack_thread_ts": threadTS,
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
			Text:     "<@UBOTID> /stop",
			User:     "U1",
			Channel:  channelID,
			Ts:       "900.001",
			ThreadTs: threadTS,
		},
	}

	err := handler.ProcessEvent(context.Background(), botID, payload)
	require.NoError(t, err)

	ok := waitForCondition(2*time.Second, 10*time.Millisecond, func() bool {
		return sessionMgr.stoppedCount() == 1
	})
	require.True(t, ok, "StopAgent should be called even when message contains a bot mention")
	assert.Equal(t, 0, sessionMgr.createdCount(), "no new session should be created for /stop with mention")
}

// TestProcessEvent_StopCommand_NoActiveSession verifies that /stop when there is no
// active session does not crash and does not create a new session.
func TestProcessEvent_StopCommand_NoActiveSession(t *testing.T) {
	const (
		botID     = "stop-empty-bot-uuid"
		channelID = "C-stop-empty"
	)

	repo := newMockSlackBotRepository()
	bot := entities.NewSlackBot(botID, "Stop Empty Bot", "user-1")
	repo.bots[botID] = bot

	// No existing sessions
	sessionMgr := &mockSessionManager{}
	handler := NewSlackBotEventHandler(repo, sessionMgr, "", "", nil, "", false, nil)

	payload := buildEventPayload(channelID, "/stop")

	err := handler.ProcessEvent(context.Background(), botID, payload)
	require.NoError(t, err)

	time.Sleep(50 * time.Millisecond)
	assert.Equal(t, 0, sessionMgr.createdCount(), "no new session should be created for /stop with no active session")
	assert.Equal(t, 0, sessionMgr.stoppedCount(), "StopAgent should not be called when there is no active session")
}

// ---- Tests for thread context feature ----

// buildThreadRepliesHandler returns an httptest handler that serves a mock conversations.replies response.
func buildThreadRepliesHandler(channel, threadTS string, messages []SlackMessage) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/conversations.replies" {
			http.Error(w, "unexpected path: "+r.URL.Path, http.StatusNotFound)
			return
		}
		if r.URL.Query().Get("channel") != channel || r.URL.Query().Get("ts") != threadTS {
			http.Error(w, "unexpected channel/ts", http.StatusBadRequest)
			return
		}
		resp := struct {
			OK       bool           `json:"ok"`
			Messages []SlackMessage `json:"messages"`
		}{OK: true, Messages: messages}
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(resp); err != nil {
			http.Error(w, "encode error", http.StatusInternalServerError)
		}
	}
}

// TestProcessEvent_ThreadContext_AvailableInPayload verifies that when the triggering
// message is already inside an existing Slack thread (thread_ts != ""), thread_messages
// is fetched and made available as a template variable.
func TestProcessEvent_ThreadContext_PrependedWhenNoTemplate(t *testing.T) {
	const (
		botID      = "thread-ctx-bot-uuid"
		channelID  = "C-thread-ctx"
		threadTS   = "1700000000.000001"
		namespace  = "test-ns"
		secretName = "bot-token-secret"
	)

	threadMessages := []SlackMessage{
		{User: "U-alice", Text: "Hey team, can someone look at this?", Ts: threadTS},
		{User: "U-bob", Text: "Sure, what's the issue?", Ts: "1700000001.000001"},
	}

	// Mux for the mock Slack API server: handle both conversations.replies and chat.postMessage
	mux := http.NewServeMux()
	mux.HandleFunc("/conversations.replies", buildThreadRepliesHandler(channelID, threadTS, threadMessages))
	// Stub chat.postMessage to avoid noise
	mux.HandleFunc("/chat.postMessage", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(map[string]interface{}{"ok": true}); err != nil {
			http.Error(w, "encode error", http.StatusInternalServerError)
		}
	})
	// Stub conversations.info for channel name resolution (not needed here but prevent 404 noise)
	mux.HandleFunc("/conversations.info", func(w http.ResponseWriter, _ *http.Request) {
		resp := map[string]interface{}{"ok": true, "channel": map[string]string{"id": channelID, "name": "test-channel"}}
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(resp); err != nil {
			http.Error(w, "encode error", http.StatusInternalServerError)
		}
	})
	mockServer := httptest.NewServer(mux)
	defer mockServer.Close()

	fakeClient := fake.NewSimpleClientset(
		&corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{Name: secretName, Namespace: namespace},
			Data:       map[string][]byte{"bot-token": []byte("xoxb-test-token")},
		},
	)
	resolver := NewSlackChannelResolver(fakeClient, namespace).WithSlackAPIBase(mockServer.URL)

	repo := newMockSlackBotRepository()
	bot := entities.NewSlackBot(botID, "Thread Ctx Bot", "user-1")
	bot.SetBotTokenSecretName(secretName)
	repo.bots[botID] = bot

	sessionMgr := &mockSessionManager{}
	handler := NewSlackBotEventHandler(repo, sessionMgr, secretName, "bot-token", resolver, "", false, nil)

	// Triggering message is a reply in an existing thread
	triggerTs := "1700000002.000001"
	payload := SlackPayload{
		Type:   "event_callback",
		TeamID: "T1",
		Event: &SlackEvent{
			Type:     "app_mention",
			Text:     "@bot please help",
			User:     "U-alice",
			Channel:  channelID,
			Ts:       triggerTs,
			ThreadTs: threadTS,
		},
	}

	err := handler.ProcessEvent(context.Background(), botID, payload)
	require.NoError(t, err)

	ok := waitForCondition(2*time.Second, 10*time.Millisecond, func() bool {
		return sessionMgr.createdCount() == 1
	})
	require.True(t, ok, "session should be created")

	// Verify the initial message passed to CreateSession contains the full thread history.
	sessionMgr.mu.Lock()
	createdSession := sessionMgr.createdSessions[0]
	sessionMgr.mu.Unlock()

	assert.Equal(t, channelID, createdSession.tags["slack_channel"])
	assert.Equal(t, threadTS, createdSession.tags["slack_thread_ts"])

	// Without a template, the initial message is just the triggering event text.
	// thread_messages is available via payloadMap for templates that reference {{ .thread_messages }}.
	assert.Equal(t, "@bot please help", createdSession.initialMessage,
		"without a template, initial message should be the plain event text")
}

// TestProcessEvent_ThreadContext_NoResolver_DoesNotCrash verifies that when no channel
// resolver is configured, a message in a thread still creates a session normally (no crash,
// no thread context - just the plain message text).
