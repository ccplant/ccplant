package slackbot

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"

	"github.com/takutakahashi/agentapi-proxy/internal/domain/entities"
)

func TestProcessEvent_ThreadContext_NoResolver_DoesNotCrash(t *testing.T) {
	const (
		botID     = "thread-no-resolver-bot"
		channelID = "C-no-resolver"
		threadTS  = "1700100000.000001"
	)

	repo := newMockSlackBotRepository()
	bot := entities.NewSlackBot(botID, "No Resolver Bot", "user-1")
	repo.bots[botID] = bot

	sessionMgr := &mockSessionManager{}
	// No channel resolver → fetchAndFormatThreadContext returns "" immediately
	handler := NewSlackBotEventHandler(repo, sessionMgr, "", "", nil, "", false, nil)

	payload := SlackPayload{
		Type:   "event_callback",
		TeamID: "T1",
		Event: &SlackEvent{
			Type:     "app_mention",
			Text:     "@bot help me",
			User:     "U-user",
			Channel:  channelID,
			Ts:       "1700100001.000001",
			ThreadTs: threadTS,
		},
	}

	err := handler.ProcessEvent(context.Background(), botID, payload)
	require.NoError(t, err)

	ok := waitForCondition(2*time.Second, 10*time.Millisecond, func() bool {
		return sessionMgr.createdCount() == 1
	})
	require.True(t, ok, "session should still be created even when thread context cannot be fetched")
}

// TestFetchAndFormatThreadContext_FiltersMessagesAfterUntilTs verifies that messages with
// ts > untilTS are excluded from the formatted context string.
func TestFetchAndFormatThreadContext_FiltersMessagesAfterUntilTs(t *testing.T) {
	const (
		channelID  = "C-filter-ts"
		threadTS   = "1700200000.000001"
		namespace  = "test-ns"
		secretName = "bot-secret"
	)
	untilTS := "1700200001.000001" // only include messages up to this ts

	allMessages := []SlackMessage{
		{User: "U-a", Text: "first", Ts: "1700200000.000001"},
		{User: "U-b", Text: "second", Ts: "1700200001.000001"}, // = untilTS, should be included
		{User: "U-a", Text: "third", Ts: "1700200002.000001"},  // > untilTS, should be excluded
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/conversations.replies", buildThreadRepliesHandler(channelID, threadTS, allMessages))
	mockServer := httptest.NewServer(mux)
	defer mockServer.Close()

	fakeClient := fake.NewSimpleClientset(
		&corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{Name: secretName, Namespace: namespace},
			Data:       map[string][]byte{"bot-token": []byte("xoxb-token")},
		},
	)
	resolver := NewSlackChannelResolver(fakeClient, namespace).WithSlackAPIBase(mockServer.URL)

	repo := newMockSlackBotRepository()
	bot := entities.NewSlackBot("bot-id", "Bot", "user-1")
	bot.SetBotTokenSecretName(secretName)
	repo.bots["bot-id"] = bot

	sessionMgr := &mockSessionManager{}
	handler := NewSlackBotEventHandler(repo, sessionMgr, secretName, "bot-token", resolver, "", false, nil)

	result := handler.fetchAndFormatThreadContext(context.Background(), bot, channelID, threadTS, untilTS)

	assert.NotEmpty(t, result, "context should not be empty")
	assert.True(t, strings.Contains(result, "first"), "result should contain first message")
	assert.True(t, strings.Contains(result, "second"), "result should contain second message (ts == untilTS)")
	assert.False(t, strings.Contains(result, "third"), "result should NOT contain third message (ts > untilTS)")
}

// TestPostSessionURLToSlack_RepositoryInMessage verifies that when a repository is detected,
// the Slack notification message includes the repository name.
func TestPostSessionURLToSlack_RepositoryInMessage(t *testing.T) {
	const (
		botID      = "notify-repo-bot"
		channelID  = "C-notify"
		threadTS   = "1700300000.000001"
		sessionID  = "session-abc"
		namespace  = "test-ns"
		secretName = "bot-token-secret"
	)

	// Capture the posted Slack message (JSON body with "text" field).
	var capturedMessage string
	mux := http.NewServeMux()
	mux.HandleFunc("/chat.postMessage", func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Text string `json:"text"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err == nil {
			capturedMessage = body.Text
		}
		w.Header().Set("Content-Type", "application/json")
		assert.NoError(t, json.NewEncoder(w).Encode(map[string]interface{}{"ok": true}))
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
	bot := entities.NewSlackBot(botID, "Notify Bot", "user-1")
	bot.SetBotTokenSecretName(secretName)
	repo.bots[botID] = bot

	sessionMgr := &mockSessionManager{}
	baseURL := "https://example.com"
	handler := NewSlackBotEventHandler(repo, sessionMgr, secretName, "bot-token", resolver, baseURL, false, nil)

	handler.postSessionURLToSlack(context.Background(), channelID, threadTS, sessionID, "myorg/myrepo", bot)

	assert.Contains(t, capturedMessage, "myorg/myrepo",
		"notification message should include the repository name")
	assert.Contains(t, capturedMessage, sessionID,
		"notification message should include the session ID")
}

// TestPostSessionURLToSlack_NoRepositoryDefaultMessage verifies that when no repository is set,
// the notification message uses the default format without repository info.
func TestPostSessionURLToSlack_NoRepositoryDefaultMessage(t *testing.T) {
	const (
		botID      = "notify-no-repo-bot"
		channelID  = "C-notify-noRepo"
		threadTS   = "1700300001.000001"
		sessionID  = "session-xyz"
		namespace  = "test-ns"
		secretName = "bot-token-secret2"
	)

	var capturedMessage string
	mux := http.NewServeMux()
	mux.HandleFunc("/chat.postMessage", func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Text string `json:"text"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err == nil {
			capturedMessage = body.Text
		}
		w.Header().Set("Content-Type", "application/json")
		assert.NoError(t, json.NewEncoder(w).Encode(map[string]interface{}{"ok": true}))
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
	bot := entities.NewSlackBot(botID, "No Repo Notify Bot", "user-1")
	bot.SetBotTokenSecretName(secretName)
	repo.bots[botID] = bot

	sessionMgr := &mockSessionManager{}
	handler := NewSlackBotEventHandler(repo, sessionMgr, secretName, "bot-token", resolver, "https://example.com", false, nil)

	handler.postSessionURLToSlack(context.Background(), channelID, threadTS, sessionID, "", bot)

	assert.NotContains(t, capturedMessage, "repository",
		"notification message should NOT include 'repository' when none is detected")
	assert.Contains(t, capturedMessage, sessionID,
		"notification message should still include the session ID")
}

// TestProcessEvent_RepositoryTag_MultiLine verifies that when the first line of a Slack message
// is in "org/repo" format, the "repository" session tag is automatically set.
func TestProcessEvent_RepositoryTag_MultiLine(t *testing.T) {
	const (
		botID     = "repo-tag-bot"
		channelID = "C-repo"
	)

	repo := newMockSlackBotRepository()
	bot := entities.NewSlackBot(botID, "Repo Bot", "user-1")
	repo.bots[botID] = bot

	sessionMgr := &mockSessionManager{}
	handler := NewSlackBotEventHandler(repo, sessionMgr, "", "", nil, "", false, nil)

	// First line is "org/repo", rest is the task description.
	payload := buildEventPayload(channelID, "myorg/myrepo\nPlease fix the bug in the authentication module.")
	err := handler.ProcessEvent(context.Background(), botID, payload)
	require.NoError(t, err)

	ok := waitForCondition(2*time.Second, 10*time.Millisecond, func() bool {
		return sessionMgr.createdCount() == 1
	})
	require.True(t, ok, "session should be created")

	sess := sessionMgr.getCreatedSession(0)
	assert.Equal(t, "myorg/myrepo", sess.tags["repository"],
		"repository tag should be automatically set from the first line")
	require.NotNil(t, sess.repoInfo, "RepoInfo should be set in LaunchRequest")
	assert.Equal(t, "myorg/myrepo", sess.repoInfo.FullName)
	assert.Equal(t, "/home/agentapi/workdir/repo", sess.repoInfo.CloneDir)
}

// TestProcessEvent_RepositoryTag_SingleLine verifies that a single-line "org/repo" message
// also sets the repository tag correctly.
func TestProcessEvent_RepositoryTag_SingleLine(t *testing.T) {
	const (
		botID     = "repo-tag-single-bot"
		channelID = "C-single"
	)

	repo := newMockSlackBotRepository()
	bot := entities.NewSlackBot(botID, "Single Line Bot", "user-1")
	repo.bots[botID] = bot

	sessionMgr := &mockSessionManager{}
	handler := NewSlackBotEventHandler(repo, sessionMgr, "", "", nil, "", false, nil)

	payload := buildEventPayload(channelID, "myorg/myrepo")
	err := handler.ProcessEvent(context.Background(), botID, payload)
	require.NoError(t, err)

	ok := waitForCondition(2*time.Second, 10*time.Millisecond, func() bool {
		return sessionMgr.createdCount() == 1
	})
	require.True(t, ok, "session should be created")

	sess := sessionMgr.getCreatedSession(0)
	assert.Equal(t, "myorg/myrepo", sess.tags["repository"],
		"single-line org/repo message should set repository tag")
}

// TestProcessEvent_RepositoryTag_WithMention verifies that bot mentions are stripped
// before the "org/repo" check, so "@bot myorg/myrepo" still sets the repository tag.
func TestProcessEvent_RepositoryTag_WithMention(t *testing.T) {
	const (
		botID     = "repo-tag-mention-bot"
		channelID = "C-mention"
	)

	repo := newMockSlackBotRepository()
	bot := entities.NewSlackBot(botID, "Mention Bot", "user-1")
	repo.bots[botID] = bot

	sessionMgr := &mockSessionManager{}
	handler := NewSlackBotEventHandler(repo, sessionMgr, "", "", nil, "", false, nil)

	// Slack app_mention events include the mention token in the text.
	payload := buildEventPayload(channelID, "<@UBOTID> myorg/myrepo\nPlease fix the login bug.")
	err := handler.ProcessEvent(context.Background(), botID, payload)
	require.NoError(t, err)

	ok := waitForCondition(2*time.Second, 10*time.Millisecond, func() bool {
		return sessionMgr.createdCount() == 1
	})
	require.True(t, ok, "session should be created")

	sess := sessionMgr.getCreatedSession(0)
	assert.Equal(t, "myorg/myrepo", sess.tags["repository"],
		"mention should be stripped and org/repo should be set as repository tag")
}

// TestProcessEvent_RepositoryTag_NotSet verifies that when the first line is not in
// "org/repo" format, the repository tag is NOT set.
func TestProcessEvent_RepositoryTag_NotSet(t *testing.T) {
	const (
		botID     = "repo-tag-none-bot"
		channelID = "C-noRepo"
	)

	repo := newMockSlackBotRepository()
	bot := entities.NewSlackBot(botID, "No Repo Bot", "user-1")
	repo.bots[botID] = bot

	sessionMgr := &mockSessionManager{}
	handler := NewSlackBotEventHandler(repo, sessionMgr, "", "", nil, "", false, nil)

	payload := buildEventPayload(channelID, "こんにちは！バグを直してほしいんですが")
	err := handler.ProcessEvent(context.Background(), botID, payload)
	require.NoError(t, err)

	ok := waitForCondition(2*time.Second, 10*time.Millisecond, func() bool {
		return sessionMgr.createdCount() == 1
	})
	require.True(t, ok, "session should be created")

	sess := sessionMgr.getCreatedSession(0)
	_, hasRepo := sess.tags["repository"]
	assert.False(t, hasRepo, "repository tag should NOT be set when first line is not org/repo format")
	assert.Nil(t, sess.repoInfo, "RepoInfo should be nil when no repository is detected")
}

// TestParseRepository unit-tests the parseRepository helper directly.
func TestParseRepository(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		// Basic patterns
		{"org/repo only", "myorg/myrepo", "myorg/myrepo"},
		{"org/repo on first line", "myorg/myrepo\nPlease fix the bug.", "myorg/myrepo"},
		{"org/repo on second line", "Please fix this:\nmyorg/myrepo", "myorg/myrepo"},
		{"org/repo in middle of text", "Please fix myorg/myrepo thanks", "myorg/myrepo"},
		// Mention stripping
		{"mention then org/repo", "<@UBOTID> myorg/myrepo\nFix it.", "myorg/myrepo"},
		{"mention only with org/repo", "<@UBOTID> myorg/myrepo", "myorg/myrepo"},
		// Multiple matches: first wins
		{"multiple org/repo", "aaa/bbb and ccc/ddd", "aaa/bbb"},
		// No match
		{"plain text", "こんにちは！", ""},
		{"plain english text", "Please help me", ""},
		{"only mention", "<@UBOTID>", ""},
		{"mention with plain text", "<@UBOTID> hello world", ""},
		// URL should NOT match (preceded by /)
		{"github URL not matched", "see https://github.com/org/repo for details", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := parseRepository(tt.input)
			assert.Equal(t, tt.want, got)
		})
	}
}

// TestProcessEvent_ConfiguredRepo_UsedWhenNoMessageRepo verifies that when session_config.params.repo_full_name
// is set on the SlackBot and the message does not contain an org/repo identifier,
// the configured repository is used.
