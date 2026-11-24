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
	portrepos "github.com/takutakahashi/agentapi-proxy/internal/usecases/ports/repositories"
)

// --- Mocks ---

// mockSessionManager implements repositories.SessionManager for testing
type mockSessionManager struct {
	mu               sync.Mutex
	createdSessions  []*mockSession
	createErr        error
	sentMessages     []string
	stoppedSessions  []string           // IDs of sessions stopped via StopAgent
	existingSessions []entities.Session // pre-seeded sessions returned by ListSessions
	createDelay      time.Duration      // optional delay to simulate slow session creation
}

func (m *mockSessionManager) CreateSession(_ context.Context, id string, req *entities.RunServerRequest, _ []byte) (entities.Session, error) {
	if m.createDelay > 0 {
		time.Sleep(m.createDelay)
	}
	if m.createErr != nil {
		return nil, m.createErr
	}
	sess := &mockSession{
		id:              id,
		tags:            req.Tags,
		scope:           req.Scope,
		initialMessage:  req.InitialMessage,
		reuseMatchTags:  req.ReuseMatchTags,
		reuseMessage:    req.ReuseMessage,
		stopBeforeReuse: req.StopBeforeReuse,
		limitMatchTags:  req.LimitMatchTags,
		maxSessions:     req.MaxSessions,
		repoInfo:        req.RepoInfo,
		slackParams:     req.SlackParams,
		pool:            req.Pool,
		managerID:       req.ManagerID,
	}
	m.mu.Lock()
	m.createdSessions = append(m.createdSessions, sess)
	m.mu.Unlock()
	return sess, nil
}

func (m *mockSessionManager) GetSession(_ string) entities.Session { return nil }
func (m *mockSessionManager) DeleteSession(_ string) error         { return nil }
func (m *mockSessionManager) Shutdown(_ time.Duration) error       { return nil }

func (m *mockSessionManager) ListSessions(filter entities.SessionFilter) []entities.Session {
	var result []entities.Session
	for _, s := range m.existingSessions {
		if filter.Status != "" && s.Status() != filter.Status {
			continue
		}
		if len(filter.Tags) > 0 {
			match := true
			for k, v := range filter.Tags {
				if s.Tags()[k] != v {
					match = false
					break
				}
			}
			if !match {
				continue
			}
		}
		result = append(result, s)
	}
	return result
}

func (m *mockSessionManager) SendMessage(_ context.Context, _ string, msg string) error {
	m.mu.Lock()
	m.sentMessages = append(m.sentMessages, msg)
	m.mu.Unlock()
	return nil
}

func (m *mockSessionManager) StopAgent(_ context.Context, id string) error {
	m.mu.Lock()
	m.stoppedSessions = append(m.stoppedSessions, id)
	m.mu.Unlock()
	return nil
}

// stoppedCount returns the number of sessions stopped so far (thread-safe).
func (m *mockSessionManager) stoppedCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.stoppedSessions)
}

// sentCount returns the number of messages sent so far (thread-safe).
func (m *mockSessionManager) sentCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.sentMessages)
}

// getSentMessage returns the sent message at index i (thread-safe).
func (m *mockSessionManager) getSentMessage(i int) string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.sentMessages[i]
}

func (m *mockSessionManager) GetMessages(_ context.Context, _ string) ([]portrepos.Message, error) {
	return nil, nil
}

// createdCount returns the number of sessions created so far (thread-safe).
func (m *mockSessionManager) createdCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.createdSessions)
}

// getCreatedSession returns the created session at index i (thread-safe).
func (m *mockSessionManager) getCreatedSession(i int) *mockSession {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.createdSessions[i]
}

// mockSession implements entities.Session
type mockSession struct {
	id              string
	tags            map[string]string
	scope           entities.ResourceScope
	status          string // defaults to "active" when empty
	initialMessage  string // captured from RunServerRequest.InitialMessage
	reuseMatchTags  map[string]string
	reuseMessage    string
	stopBeforeReuse bool
	limitMatchTags  map[string]string
	maxSessions     int
	repoInfo        *entities.RepositoryInfo
	slackParams     *entities.SlackParams
	pool            string
	managerID       string
}

func (s *mockSession) ID() string                    { return s.id }
func (s *mockSession) Addr() string                  { return "" }
func (s *mockSession) UserID() string                { return "" }
func (s *mockSession) Scope() entities.ResourceScope { return s.scope }
func (s *mockSession) TeamID() string                { return "" }
func (s *mockSession) Tags() map[string]string       { return s.tags }
func (s *mockSession) Status() string {
	if s.status == "" {
		return "active"
	}
	return s.status
}
func (s *mockSession) StartedAt() time.Time     { return time.Time{} }
func (s *mockSession) UpdatedAt() time.Time     { return time.Time{} }
func (s *mockSession) LastMessageAt() time.Time { return time.Time{} }
func (s *mockSession) Description() string      { return "" }
func (s *mockSession) Cancel()                  {}

// --- Helpers ---

// waitForCondition polls fn up to maxWait in pollInterval steps.
func waitForCondition(maxWait, pollInterval time.Duration, fn func() bool) bool {
	deadline := time.Now().Add(maxWait)
	for time.Now().Before(deadline) {
		if fn() {
			return true
		}
		time.Sleep(pollInterval)
	}
	return fn()
}

// buildEventPayload builds a minimal SlackPayload for testing.
func buildEventPayload(channel, text string) SlackPayload {
	return SlackPayload{
		Type:   "event_callback",
		TeamID: "T1",
		Event: &SlackEvent{
			Type:    "message",
			Text:    text,
			User:    "U1",
			Channel: channel,
			Ts:      "123.456",
		},
	}
}

// ---- Tests for resolveBotByChannel ----

func TestResolveBotByChannel_Success(t *testing.T) {
	const (
		namespace  = "test-ns"
		secretName = "bot-token-secret"
		channelID  = "C-test-channel"
	)

	fakeClient := fake.NewSimpleClientset(
		&corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{Name: secretName, Namespace: namespace},
			Data:       map[string][]byte{"bot-token": []byte("xoxb-test-token")},
		},
		&corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{Name: "agentapi-slack-channel-cache", Namespace: namespace},
			Data:       map[string]string{channelID: "dev-alerts"},
		},
	)
	resolver := NewSlackChannelResolver(fakeClient, namespace)

	// Bot with matching AllowedChannelNames and no custom bot token
	repo := newMockSlackBotRepository()
	bot := entities.NewSlackBot("bot-uuid-1", "Dev Alerts Bot", "user-1")
	bot.SetAllowedChannelNames([]string{"dev"}) // partial match: "dev" ⊆ "dev-alerts"
	// BotTokenSecretName = "" → uses default
	repo.bots["bot-uuid-1"] = bot

	handler := NewSlackBotEventHandler(repo, &mockSessionManager{}, secretName, "bot-token", resolver, "", false, nil)

	resolved := handler.resolveBotByChannel(context.Background(), channelID)
	require.NotNil(t, resolved, "should resolve bot by channel name")
	assert.Equal(t, "bot-uuid-1", resolved.ID())
}

func TestResolveBotByChannel_TeamScopedBotSuccess(t *testing.T) {
	const (
		namespace  = "test-ns"
		secretName = "bot-token-secret"
		channelID  = "C0AJABWPZMF"
	)

	fakeClient := fake.NewSimpleClientset(
		&corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{Name: secretName, Namespace: namespace},
			Data:       map[string][]byte{"bot-token": []byte("xoxb-test-token")},
		},
		&corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{Name: "agentapi-slack-channel-cache", Namespace: namespace},
			Data:       map[string]string{channelID: "dev-alerts"},
		},
	)
	resolver := NewSlackChannelResolver(fakeClient, namespace)

	repo := newMockSlackBotRepository()
	bot := entities.NewSlackBot("bot-team-uuid", "Team Dev Alerts Bot", "user-1")
	bot.SetScope(entities.ScopeTeam)
	bot.SetTeamID("myorg/backend")
	bot.SetAllowedChannelNames([]string{"dev-alerts"})
	repo.bots["bot-team-uuid"] = bot

	handler := NewSlackBotEventHandler(repo, &mockSessionManager{}, secretName, "bot-token", resolver, "", false, nil)

	resolved := handler.resolveBotByChannel(context.Background(), channelID)
	require.NotNil(t, resolved, "should resolve team-scoped bot by channel name")
	assert.Equal(t, "bot-team-uuid", resolved.ID())
}

func TestResolveBotByChannel_NoMatch(t *testing.T) {
	const (
		namespace  = "test-ns"
		secretName = "bot-token-secret"
		channelID  = "C-other-channel"
	)

	fakeClient := fake.NewSimpleClientset(
		&corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{Name: secretName, Namespace: namespace},
			Data:       map[string][]byte{"bot-token": []byte("xoxb-test")},
		},
		&corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{Name: "agentapi-slack-channel-cache", Namespace: namespace},
			Data:       map[string]string{channelID: "general"}, // "dev" not in "general"
		},
	)
	resolver := NewSlackChannelResolver(fakeClient, namespace)

	repo := newMockSlackBotRepository()
	bot := entities.NewSlackBot("bot-uuid-1", "Dev Bot", "user-1")
	bot.SetAllowedChannelNames([]string{"dev"})
	repo.bots["bot-uuid-1"] = bot

	handler := NewSlackBotEventHandler(repo, &mockSessionManager{}, secretName, "bot-token", resolver, "", false, nil)

	resolved := handler.resolveBotByChannel(context.Background(), channelID)
	assert.Nil(t, resolved, "should not match any bot when channel name doesn't match AllowedChannelNames")
}

func TestResolveBotByChannel_BotWithCustomToken_Skipped(t *testing.T) {
	const (
		namespace  = "test-ns"
		secretName = "bot-token-secret"
		channelID  = "C-dev"
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
	bot := entities.NewSlackBot("bot-uuid-1", "Custom Token Bot", "user-1")
	bot.SetAllowedChannelNames([]string{"dev"})
	bot.SetBotTokenSecretName("custom-k8s-secret") // has custom bot token → must be skipped
	repo.bots["bot-uuid-1"] = bot

	handler := NewSlackBotEventHandler(repo, &mockSessionManager{}, secretName, "bot-token", resolver, "", false, nil)

	resolved := handler.resolveBotByChannel(context.Background(), channelID)
	assert.Nil(t, resolved, "bot with custom bot token must not be matched via default endpoint")
}

func TestResolveBotByChannel_NilResolver_ReturnsNil(t *testing.T) {
	repo := newMockSlackBotRepository()
	// channelResolver = nil → early return
	handler := NewSlackBotEventHandler(repo, &mockSessionManager{}, "secret-name", "bot-token", nil, "", false, nil)

	resolved := handler.resolveBotByChannel(context.Background(), "C-some-channel")
	assert.Nil(t, resolved, "should return nil when channelResolver is nil")
}

func TestResolveBotByChannel_EmptyDefaultTokenSecret_ReturnsNil(t *testing.T) {
	repo := newMockSlackBotRepository()
	fakeClient := fake.NewSimpleClientset()
	resolver := NewSlackChannelResolver(fakeClient, "test-ns")
	// defaultBotTokenSecretName = "" → early return
	handler := NewSlackBotEventHandler(repo, &mockSessionManager{}, "", "bot-token", resolver, "", false, nil)

	resolved := handler.resolveBotByChannel(context.Background(), "C-some-channel")
	assert.Nil(t, resolved, "should return nil when defaultBotTokenSecretName is empty")
}

func TestResolveBotByChannel_EmptyAllowedChannelNames_Skipped(t *testing.T) {
	const (
		namespace  = "test-ns"
		secretName = "bot-token-secret"
		channelID  = "C-dev"
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
	bot := entities.NewSlackBot("bot-uuid-1", "All Channels Bot", "user-1")
	// AllowedChannelNames is empty → not identifiable via channel filter
	repo.bots["bot-uuid-1"] = bot

	handler := NewSlackBotEventHandler(repo, &mockSessionManager{}, secretName, "bot-token", resolver, "", false, nil)

	resolved := handler.resolveBotByChannel(context.Background(), channelID)
	assert.Nil(t, resolved, "bot with empty AllowedChannelNames cannot be identified via default endpoint")
}

// ---- Tests for ProcessEvent ----

// TestProcessEvent_NonEventCallbackIgnored verifies that non-event_callback payloads are silently ignored.
