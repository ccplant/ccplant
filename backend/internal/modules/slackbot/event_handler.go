package slackbot

import (
	"context"
	"log"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/takutakahashi/agentapi-proxy/internal/core/configrender"
	"github.com/takutakahashi/agentapi-proxy/internal/domain/entities"
	"github.com/takutakahashi/agentapi-proxy/internal/usecases/ports/repositories"
	sessionuc "github.com/takutakahashi/agentapi-proxy/internal/usecases/session"
	"github.com/takutakahashi/agentapi-proxy/pkg/telemetry"
)

const (
	// slackBotDefaultID is the special ID for the server-configured default SlackBot
	slackBotDefaultID = "default"
	// slackEventDedupTTL covers delayed duplicate Events API deliveries and informer lag.
	slackEventDedupTTL = time.Minute
)

// slackMentionRe matches Slack user/bot mention tokens of the form <@UXXXXXXXX>.
var slackMentionRe = regexp.MustCompile(`<@[A-Z0-9]+>`)

// repoRe matches a GitHub-style "org/repo" identifier (letters, digits, hyphens, underscores, dots).
// It uses word-boundary-like anchors to avoid matching partial URLs (e.g. "https://github.com/org/repo").
var repoRe = regexp.MustCompile(`(?:^|[\s,])([A-Za-z0-9_.\-]+/[A-Za-z0-9_.\-]+)(?:$|[\s,])`)

// SlackBotEventHandler handles incoming Slack events (via Socket Mode) and manages sessions
type SlackBotEventHandler struct {
	repo            repositories.SlackBotRepository
	sessionManager  repositories.SessionManager
	launcher        *sessionuc.LaunchUseCase
	channelResolver SlackEventClient
	// Default SlackBot configuration (from server startup config)
	defaultBotTokenSecretName string
	defaultBotTokenSecretKey  string
	// baseURL is used to construct session URLs posted back to Slack threads.
	// If empty, NOTIFICATION_BASE_URL env var is checked as a fallback.
	baseURL string
	// dryRun disables actual session creation and Slack posts; actions are only logged.
	// Enabled via AGENTAPI_SLACK_DRY_RUN environment variable.
	dryRun bool
	// synchronous runs deferred session work inline. Production leaves this false;
	// the simulator enables it so it can return the exact effects of the shared path.
	synchronous bool
	outcomeMu   sync.Mutex
	outcome     string
	// pendingThreads serializes session creation/reuse requests for each Slack thread.
	// Distinct messages in one thread must be queued rather than discarded while an
	// earlier request is in flight. Exact duplicate callbacks are handled separately by
	// processedEvents using the Slack message timestamp.
	pendingThreadsMu sync.Mutex
	pendingThreads   map[string]*pendingThreadQueue
	// processedEvents tracks recently accepted Slack message timestamps.
	processedEvents sync.Map
}

// pendingThreadQueue is a small FIFO chain for one Slack thread. Each reservation
// waits for its predecessor channel to close, then closes its own channel when done.
type pendingThreadQueue struct {
	tail chan struct{}
	refs int
}

// NewSlackBotEventHandler creates a new SlackBotEventHandler
func NewSlackBotEventHandler(
	repo repositories.SlackBotRepository,
	sessionManager repositories.SessionManager,
	defaultBotTokenSecretName string,
	defaultBotTokenSecretKey string,
	channelResolver SlackEventClient,
	baseURL string,
	dryRun bool,
	sessionProfileRepo repositories.SessionProfileRepository,
) *SlackBotEventHandler {
	return &SlackBotEventHandler{
		repo:                      repo,
		sessionManager:            sessionManager,
		launcher:                  sessionuc.NewLaunchUseCase(sessionManager).WithSessionProfileRepository(sessionProfileRepo),
		channelResolver:           channelResolver,
		defaultBotTokenSecretName: defaultBotTokenSecretName,
		defaultBotTokenSecretKey:  defaultBotTokenSecretKey,
		baseURL:                   baseURL,
		dryRun:                    dryRun,
		pendingThreads:            make(map[string]*pendingThreadQueue),
	}
}

// WithSettingsRepository enables personal team-profile opt-in resolution.
func (h *SlackBotEventHandler) WithSettingsRepository(repo repositories.SettingsRepository) *SlackBotEventHandler {
	h.launcher.WithSettingsRepository(repo)
	return h
}

// SlackEventClient is the Slack boundary used after Socket Mode has decoded an event.
// Simulation replaces this boundary while retaining the complete production event path.
type SlackEventClient interface {
	ResolveChannelName(ctx context.Context, channelID, botToken string) (string, error)
	GetBotToken(ctx context.Context, secretName, secretKey string) (string, error)
	FetchThreadReplies(ctx context.Context, channel, threadTS, botToken string) ([]SlackMessage, error)
	PostMessage(ctx context.Context, channel, threadTS, message, botToken string) error
}

func (h *SlackBotEventHandler) setOutcome(outcome string) {
	h.outcomeMu.Lock()
	h.outcome = outcome
	h.outcomeMu.Unlock()
}

func (h *SlackBotEventHandler) eventOutcome() string {
	h.outcomeMu.Lock()
	defer h.outcomeMu.Unlock()
	return h.outcome
}

func (h *SlackBotEventHandler) runDeferred(fn func()) {
	if h.synchronous {
		fn()
		return
	}
	go fn()
}

// reserveThreadTurn appends work to a per-thread FIFO queue. It returns a wait
// function and a release function so callers can reserve ordering before starting
// their asynchronous goroutine without blocking Slack event acknowledgement.
func (h *SlackBotEventHandler) reserveThreadTurn(key string) (wait, release func()) {
	h.pendingThreadsMu.Lock()
	queue := h.pendingThreads[key]
	if queue == nil {
		ready := make(chan struct{})
		close(ready)
		queue = &pendingThreadQueue{tail: ready}
		h.pendingThreads[key] = queue
	}
	predecessor := queue.tail
	done := make(chan struct{})
	queue.tail = done
	queue.refs++
	h.pendingThreadsMu.Unlock()

	wait = func() { <-predecessor }
	release = func() {
		close(done)
		h.pendingThreadsMu.Lock()
		queue.refs--
		if queue.refs == 0 && queue.tail == done {
			delete(h.pendingThreads, key)
		}
		h.pendingThreadsMu.Unlock()
	}
	return wait, release
}

// SlackPayload represents the outer Slack event payload structure
type SlackPayload struct {
	Type      string      `json:"type"`
	Challenge string      `json:"challenge,omitempty"`
	TeamID    string      `json:"team_id,omitempty"`
	Event     *SlackEvent `json:"event,omitempty"`
}

// SlackEvent represents the inner Slack event
type SlackEvent struct {
	Type     string `json:"type"`
	SubType  string `json:"subtype,omitempty"`
	BotID    string `json:"bot_id,omitempty"`
	Text     string `json:"text"`
	User     string `json:"user"`
	Channel  string `json:"channel"`
	Ts       string `json:"ts"`
	ThreadTs string `json:"thread_ts,omitempty"`
}

// ProcessEvent processes a parsed Slack event received via Socket Mode.
// botID should be the SlackBot entity ID or slackBotDefaultID ("default").
// This method is called by SlackSocketWorker after acknowledging the event to Slack.
func (h *SlackBotEventHandler) ProcessEvent(ctx context.Context, botID string, payload SlackPayload) error {
	return telemetry.LoggedOperationErr(ctx, "slackbot.ProcessEvent", func(operationCtx context.Context) error {
		return h.processEvent(operationCtx, botID, payload)
	}, telemetry.String("slackbot.id", botID), telemetry.String("slack.event_type", payload.Type))
}

func (h *SlackBotEventHandler) processEvent(ctx context.Context, botID string, payload SlackPayload) error {
	h.setOutcome(simulationDecisionIgnore)
	log.Printf("[SLACKBOT] ProcessEvent called: botID=%s, type=%s", botID, payload.Type)
	// We only process event_callback type
	if payload.Type != "event_callback" || payload.Event == nil {
		log.Printf("[SLACKBOT] Ignoring non-event payload: id=%s, type=%s", botID, payload.Type)
		return nil
	}

	event := payload.Event
	threadKey := event.ThreadTs
	if threadKey == "" {
		threadKey = event.Ts
	}

	bot, resolvedBotID, err := h.resolveEventBot(ctx, botID, event, threadKey)
	if err != nil {
		return err
	}
	botID = resolvedBotID
	ignore, err := h.shouldIgnoreEvent(ctx, botID, bot, event)
	if err != nil {
		return err
	}
	if ignore {
		return nil
	}

	// The message and app_mention callbacks generated for one Slack post share
	// channel+ts. Retain this key beyond session creation so a delayed callback
	// cannot create a second session while ListSessions is still stale.
	// The handler is shared by every configured bot in this process. Include the
	// bot ID so two bots receiving the same Slack message do not suppress each
	// other merely because channel and timestamp match.
	eventKey := botID + ":" + event.Channel + ":" + event.Ts
	if _, duplicate := h.processedEvents.LoadOrStore(eventKey, struct{}{}); duplicate {
		log.Printf("[SLACKBOT] Duplicate Slack event ignored: id=%s, channel=%s, ts=%s", botID, event.Channel, event.Ts)
		return nil
	}
	time.AfterFunc(slackEventDedupTTL, func() { h.processedEvents.Delete(eventKey) })

	channel := event.Channel
	log.Printf("[SLACKBOT] Processing event: id=%s, type=%s, channel=%s, thread=%s", botID, event.Type, channel, threadKey)

	// Handle /stop command: interrupt the running agent in the associated session
	if isStopCommand(event.Text) {
		h.setOutcome(simulationDecisionStop)
		h.handleStopCommand(ctx, channel, threadKey, bot)
		return nil
	}

	// Fetch thread context if the triggering message is already inside an existing thread.
	// This gives the new session the full conversation history from the root message onward.
	// Best-effort: errors are logged and an empty string is used as fallback.
	var threadMessages string
	if event.ThreadTs != "" {
		threadMessages, _ = telemetry.LoggedOperation(ctx, "slackbot.FetchThreadContext", func(operationCtx context.Context) (string, error) {
			return h.fetchAndFormatThreadContext(operationCtx, bot, channel, event.ThreadTs, event.Ts), nil
		}, telemetry.String("slack.channel", channel), telemetry.String("slack.thread_ts", threadKey))
	}

	// Build payload map for template rendering
	payloadMap := map[string]interface{}{
		"event": map[string]interface{}{
			"type":            event.Type,
			"text":            event.Text,
			"user":            event.User,
			"channel":         event.Channel,
			"ts":              event.Ts,
			"thread_ts":       event.ThreadTs,
			"thread_messages": threadMessages,
		},
		"team_id":         payload.TeamID,
		"thread_messages": threadMessages,
		"bot_id":          botID,
	}

	// Build session tags
	tags := map[string]string{
		"slackbot_id":     botID,
		"slack_channel":   channel,
		"slack_thread_ts": threadKey,
	}

	// Auto-detect "org/repo" identifier from the message text.
	// Slack mentions (<@UXXXXXXXX>) are stripped before matching.
	// The result is used directly to populate RepoInfo in the LaunchRequest;
	// the tag is set only as informational metadata.
	detectedRepo := parseRepository(event.Text)

	// Determine the effective repository for this session.
	// Priority: session_config.params.repo_full_name (static bot config) > message auto-detection.
	// A statically configured repo means the bot always operates on that repository,
	// regardless of what is mentioned in the Slack message.
	configuredRepo := ""
	if bot != nil && bot.SessionConfig() != nil && bot.SessionConfig().Params() != nil {
		configuredRepo = bot.SessionConfig().Params().RepoFullName
	}
	effectiveRepoForTag := configuredRepo
	if effectiveRepoForTag == "" {
		effectiveRepoForTag = detectedRepo
	}
	if effectiveRepoForTag != "" {
		tags["repository"] = effectiveRepoForTag
	}

	// Apply session config tags if present
	if bot != nil && bot.SessionConfig() != nil && bot.SessionConfig().Tags() != nil {
		renderedTags, err := configrender.RenderTemplateMap(bot.SessionConfig().Tags(), payloadMap)
		if err != nil {
			log.Printf("[SLACKBOT] Failed to render tags: %v", err)
		} else {
			for k, v := range renderedTags {
				tags[k] = v
			}
		}
	}
	triggeredUserID := strings.TrimSpace(tags["username"])
	tags["triggered_user_id"] = triggeredUserID

	// Build environment variables
	var env map[string]string
	if bot != nil && bot.SessionConfig() != nil && bot.SessionConfig().Environment() != nil {
		env, err = configrender.RenderTemplateMap(bot.SessionConfig().Environment(), payloadMap)
		if err != nil {
			log.Printf("[SLACKBOT] Failed to render environment: %v", err)
			env = nil
		}
	}

	// Build initial message.
	// Thread history is available as {{ .thread_messages }} in templates, e.g.:
	//   initial_message_template: "{{ .thread_messages }}\n---\n{{ .event.text }}"
	initialMessage := h.buildMessage(bot, payloadMap, event.Text, false)

	// Determine agent type: default to "claude-acp"; bot session_config may override.
	agentType := "claude-acp"
	model := ""
	if bot != nil && bot.SessionConfig() != nil && bot.SessionConfig().Params() != nil {
		model = bot.SessionConfig().Params().Model
		if bot.SessionConfig().Params().AgentType != "" {
			agentType = bot.SessionConfig().Params().AgentType
		}
	}

	reuseFilter := entities.SessionFilter{
		Tags: map[string]string{
			"slackbot_id":       botID,
			"slack_channel":     channel,
			"slack_thread_ts":   threadKey,
			"triggered_user_id": triggeredUserID,
		},
	}

	// Determine scope and ownership.
	// ResolveTeams ensures team-level settings are always injected correctly,
	// using the same rule as schedule and webhook trigger sources.
	scope := entities.ScopeUser
	userID := ""
	teamID := ""
	var teams []string
	var maxSessions int
	if bot != nil {
		scope = bot.Scope()
		userID = bot.UserID()
		teamID = bot.TeamID()
		teams = sessionuc.ResolveTeams(scope, teamID, bot.Teams())
		maxSessions = bot.MaxSessions()
	}

	sessionID := uuid.New().String()

	// Serialize distinct requests for the same thread. The first request may still be
	// creating the session when a follow-up arrives, so the follow-up must wait until
	// reuse can resolve authoritatively instead of being discarded as a duplicate.
	pendingKey := botID + ":" + channel + ":" + threadKey + ":" + triggeredUserID
	waitForTurn, releaseTurn := h.reserveThreadTurn(pendingKey)

	// Create session asynchronously so we don't block event processing
	h.setOutcome(simulationDecisionCreateOrReuse)
	plan := slackSessionLaunchPlan{
		eventKey: eventKey, botID: botID, channel: channel, threadKey: threadKey, sessionID: sessionID,
		configuredRepo: configuredRepo, detectedRepo: detectedRepo, triggeredUserID: triggeredUserID,
		userID: userID, teamID: teamID, agentType: agentType, model: model, initialMessage: initialMessage,
		teams: teams, scope: scope, maxSessions: maxSessions, environment: env, tags: tags,
		reuseTags: reuseFilter.Tags, payload: payloadMap, eventText: event.Text, bot: bot,
	}
	h.runDeferred(func() {
		waitForTurn()
		defer releaseTurn()
		h.launchSlackSession(context.WithoutCancel(ctx), plan)
		return
	})

	return nil
}

// resolveSlackBot retrieves the SlackBot entity.
// Returns (bot, error). For id="default", returns nil bot (uses server defaults).
