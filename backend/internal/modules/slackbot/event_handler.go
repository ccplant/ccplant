package slackbot

import (
	"context"
	"fmt"
	"log"
	"os"
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
	channelResolver *SlackChannelResolver
	// Default SlackBot configuration (from server startup config)
	defaultBotTokenSecretName string
	defaultBotTokenSecretKey  string
	// baseURL is used to construct session URLs posted back to Slack threads.
	// If empty, NOTIFICATION_BASE_URL env var is checked as a fallback.
	baseURL string
	// dryRun disables actual session creation and Slack posts; actions are only logged.
	// Enabled via AGENTAPI_SLACK_DRY_RUN environment variable.
	dryRun bool
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
	channelResolver *SlackChannelResolver,
	baseURL string,
	dryRun bool,
	memoryRepo repositories.MemoryRepository,
	sessionProfileRepo repositories.SessionProfileRepository,
) *SlackBotEventHandler {
	return &SlackBotEventHandler{
		repo:           repo,
		sessionManager: sessionManager,
		launcher: sessionuc.NewLaunchUseCase(sessionManager).
			WithMemoryRepository(memoryRepo).
			WithSessionProfileRepository(sessionProfileRepo),
		channelResolver:           channelResolver,
		defaultBotTokenSecretName: defaultBotTokenSecretName,
		defaultBotTokenSecretKey:  defaultBotTokenSecretKey,
		baseURL:                   baseURL,
		dryRun:                    dryRun,
		pendingThreads:            make(map[string]*pendingThreadQueue),
	}
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

	// Resolve the bot entity (nil for "default" when no registered bot matches)
	bot, err := h.resolveSlackBot(ctx, botID)
	if err != nil {
		return fmt.Errorf("failed to resolve slackbot: %w", err)
	}

	// For default ID: try to identify the registered bot by channel name filter.
	// If no matching bot is found, notify the user via Slack and drop the event
	// to avoid creating sessions with empty userID.
	if botID == slackBotDefaultID && bot == nil {
		// Replies overwhelmingly target an already-created session. Resolve its
		// exact bot ID from indexed session tags before doing the expensive default
		// channel-to-bot discovery (which lists bot definitions and may call Slack).
		var resolvedBot *entities.SlackBot
		if event.ThreadTs != "" {
			resolvedBot = h.resolveBotFromReusableSession(ctx, event.Channel, threadKey)
		}
		if resolvedBot != nil {
			log.Printf("[SLACKBOT] Default endpoint: identified bot from reusable session: id=%s, channel=%s, thread=%s", resolvedBot.ID(), event.Channel, threadKey)
		} else {
			resolvedBot = h.resolveBotByChannel(ctx, event.Channel)
		}
		if resolvedBot == nil {
			log.Printf("[SLACKBOT] Default endpoint: no matching bot found for channel=%s, dropping event", event.Channel)
			if botToken, tokenErr := h.getBotToken(ctx, nil); tokenErr == nil {
				h.postErrorToSlack(ctx, event.Channel, threadKey,
					":warning: このチャンネルに対応する bot が登録されていません。チャンネルの設定を確認してください。",
					botToken)
			}
			return nil
		}
		bot = resolvedBot
		botID = resolvedBot.ID()
		log.Printf("[SLACKBOT] Default endpoint: identified bot by channel filter: id=%s, channel=%s", botID, event.Channel)
	}

	// Hard guardrail: paused bots never respond to any event, regardless of allow_bot_messages
	// or any other setting. This check must remain first among all bot-level filters.
	if bot != nil && bot.Status() == entities.SlackBotStatusPaused {
		log.Printf("[SLACKBOT] Bot is paused, ignoring all events: id=%s", botID)
		return nil
	}

	// Filter bot messages unless the bot explicitly opts in via AllowBotMessages.
	// By default, messages from bots are ignored to prevent recursive session creation:
	// bot posts "session created" → triggers another event → creates another session → infinite loop.
	// When allow_bot_messages is true on the bot config, this filter is skipped.
	allowBotMessages := bot != nil && bot.AllowBotMessages()
	if !allowBotMessages && (event.BotID != "" || event.SubType == "bot_message") {
		log.Printf("[SLACKBOT] Ignoring bot message: botID=%s, event.bot_id=%s, subtype=%s", botID, event.BotID, event.SubType)
		return nil
	}

	// Apply remaining filters (if this is a registered bot, not default)
	if bot != nil {
		if !bot.IsEventTypeAllowed(event.Type) {
			log.Printf("[SLACKBOT] Event type not allowed: id=%s, type=%s", botID, event.Type)
			return nil
		}
		// User ID filter: check if the sender's Slack user ID is allowed
		if !bot.IsUserIDAllowed(event.User) {
			log.Printf("[SLACKBOT] User ID not allowed: id=%s, user=%s", botID, event.User)
			return nil
		}
		// Channel filter: allow either Slack channel ID or resolved channel name.
		if len(bot.AllowedChannelNames()) > 0 && h.channelResolver != nil {
			allowed, tokenErr := h.isBotAllowedInChannel(ctx, bot, event.Channel)
			if tokenErr != nil {
				log.Printf("[SLACKBOT] Failed to check channel filter: id=%s, channel=%s, err=%v", botID, event.Channel, tokenErr)
				threadTS := event.ThreadTs
				if threadTS == "" {
					threadTS = event.Ts
				}
				if botToken, err := h.getBotToken(ctx, bot); err == nil {
					h.postErrorToSlack(ctx, event.Channel, threadTS,
						":warning: チャンネル情報を取得できませんでした。しばらく待ってから再度お試しください。",
						botToken)
				}
				return fmt.Errorf("failed to resolve channel name for channel %s: %w", event.Channel, tokenErr)
			}
			if !allowed {
				log.Printf("[SLACKBOT] Channel not allowed: id=%s, channel=%s", botID, event.Channel)
				return nil
			}
		}
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
	go func(asyncCtx context.Context) {
		queueStartedAt := time.Now()
		waitForTurn()
		log.Printf("[SLACKBOT_TIMING] stage=thread_queue_wait duration_ms=%d bot_id=%s channel=%s thread=%s",
			time.Since(queueStartedAt).Milliseconds(), botID, channel, threadKey)
		defer releaseTurn()
		bgCtx := context.WithoutCancel(asyncCtx)

		if h.dryRun {
			log.Printf("[SLACKBOT] [DRY-RUN] Would create session: id=%s, channel=%s, thread=%s, agentType=%s, scope=%s",
				sessionID, channel, threadKey, agentType, scope)
			if bot.NotifyOnSessionCreated() {
				h.postSessionURLToSlack(bgCtx, channel, threadKey, sessionID, tags["repository"], bot)
			}
			return
		}

		// Build memory key by rendering Go templates from session_config.memory_key values.
		// This allows values like {{ .event.channel }} to be resolved at runtime.
		var memoryKey map[string]string
		if bot != nil && bot.SessionConfig() != nil && bot.SessionConfig().MemoryKey() != nil {
			renderedMemoryKey, renderErr := configrender.RenderTemplateMap(bot.SessionConfig().MemoryKey(), payloadMap)
			if renderErr != nil {
				log.Printf("[SLACKBOT] Failed to render memory_key: %v", renderErr)
			} else {
				memoryKey = renderedMemoryKey
			}
		}

		// Build RepoInfo for the session.
		// Use the already-computed effective repo (configuredRepo takes priority, then detectedRepo).
		var repoInfo *entities.RepositoryInfo
		effectiveRepo := configuredRepo
		if effectiveRepo == "" {
			effectiveRepo = detectedRepo
		}
		if effectiveRepo != "" {
			repoInfo = &entities.RepositoryInfo{
				FullName: effectiveRepo,
				CloneDir: "/home/agentapi/workdir/repo",
			}
		}

		var slackSessionProfileID string
		if bot != nil && bot.SessionConfig() != nil {
			slackSessionProfileID = bot.SessionConfig().SessionProfileID()
		}

		var slackSandbox *entities.SandboxParams
		var slackDocker *entities.DockerParams
		var slackAuthProxy *bool
		var slackInitialMessageWaitSecond *int
		var slackCycleMessage, slackSessionTTL string
		var slackCycleMaxCount int
		if bot != nil && bot.SessionConfig() != nil && bot.SessionConfig().Params() != nil {
			params := bot.SessionConfig().Params()
			slackSandbox = params.Sandbox
			slackDocker = params.Docker
			slackAuthProxy = params.AuthProxy
			slackInitialMessageWaitSecond = params.InitialMessageWaitSecond
			slackCycleMessage = params.CycleMessage
			slackCycleMaxCount = params.CycleMaxCount
			slackSessionTTL = params.SessionTTL
		}

		result, err := telemetry.LoggedOperation(bgCtx, "slackbot.LaunchSession", func(launchCtx context.Context) (sessionuc.LaunchResult, error) {
			return h.launcher.Launch(launchCtx, sessionID, sessionuc.LaunchRequest{
				UserID:                   userID,
				TriggeredUserID:          triggeredUserID,
				Scope:                    scope,
				TeamID:                   teamID,
				Teams:                    teams,
				Environment:              env,
				Tags:                     tags,
				InitialMessage:           initialMessage,
				ReuseSession:             true,
				ReuseMatchTags:           reuseFilter.Tags,
				ReuseMessage:             h.buildMessage(bot, payloadMap, event.Text, true),
				StopBeforeReuse:          true,
				DeferReuseToStart:        true,
				MaxSessions:              maxSessions,
				LimitMatchTags:           map[string]string{"slackbot_id": botID},
				AgentType:                agentType,
				Model:                    model,
				MemoryKey:                memoryKey,
				RepoInfo:                 repoInfo,
				Sandbox:                  slackSandbox,
				Docker:                   slackDocker,
				AuthProxy:                slackAuthProxy,
				InitialMessageWaitSecond: slackInitialMessageWaitSecond,
				CycleMessage:             slackCycleMessage,
				CycleMaxCount:            slackCycleMaxCount,
				SessionTTL:               slackSessionTTL,
				SessionProfileID:         slackSessionProfileID,
				CredentialSource: func() string {
					if bot != nil && bot.SessionConfig() != nil && bot.SessionConfig().Params() != nil {
						return bot.SessionConfig().Params().CredentialSource
					}
					return ""
				}(),
				SlackParams: func() *entities.SlackParams {
					sp := &entities.SlackParams{
						Channel:            channel,
						ThreadTS:           threadKey,
						BotTokenSecretName: h.defaultBotTokenSecretName,
						BotTokenSecretKey:  h.defaultBotTokenSecretKey,
					}
					if bot != nil && bot.BotTokenSecretName() != "" {
						sp.BotTokenSecretName = bot.BotTokenSecretName()
					}
					if bot != nil && bot.BotTokenSecretKey() != "" {
						sp.BotTokenSecretKey = bot.BotTokenSecretKey()
					}
					return sp
				}(),
			})
		}, telemetry.String("slackbot.id", botID), telemetry.String("slack.channel", channel), telemetry.String("slack.thread_ts", threadKey))
		if err != nil {
			h.processedEvents.Delete(eventKey)
			log.Printf("[SLACKBOT] Failed to create session: %v", err)
			return
		}
		if result.SessionReused {
			log.Printf("[SLACKBOT] Reused session %s for thread %s", result.SessionID, threadKey)
		} else {
			log.Printf("[SLACKBOT] Created session %s for thread %s", result.SessionID, threadKey)
		}
		if !result.SessionReused && bot.NotifyOnSessionCreated() {
			h.postSessionURLToSlack(bgCtx, channel, threadKey, result.SessionID, tags["repository"], bot)
		}
	}(ctx)

	return nil
}

// resolveSlackBot retrieves the SlackBot entity.
// Returns (bot, error). For id="default", returns nil bot (uses server defaults).
func (h *SlackBotEventHandler) resolveSlackBot(ctx context.Context, id string) (*entities.SlackBot, error) {
	if id == slackBotDefaultID {
		return nil, nil
	}

	bot, err := h.repo.Get(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("slackbot not found: %s", id)
	}
	return bot, nil
}

func (h *SlackBotEventHandler) resolveBotFromReusableSession(ctx context.Context, channelID, threadTS string) *entities.SlackBot {
	if channelID == "" || threadTS == "" {
		return nil
	}
	sessions, _ := telemetry.LoggedOperation(ctx, "slackbot.FindReusableSession", func(context.Context) ([]entities.Session, error) {
		return h.sessionManager.ListSessions(entities.SessionFilter{Tags: map[string]string{
			"slack_channel": channelID, "slack_thread_ts": threadTS,
		}}), nil
	}, telemetry.String("slack.channel", channelID), telemetry.String("slack.thread_ts", threadTS))
	for _, session := range sessions {
		status := strings.ToLower(session.Status())
		if status == "stopped" || status == "failed" || status == "terminated" || status == "terminating" {
			continue
		}
		id := session.Tags()["slackbot_id"]
		if id == "" || id == slackBotDefaultID {
			continue
		}
		bot, err := h.repo.Get(ctx, id)
		if err != nil {
			log.Printf("[SLACKBOT] Failed to resolve bot %s from reusable session %s: %v", id, session.ID(), err)
			continue
		}
		return bot
	}
	return nil
}

// resolveBotByChannel attempts to identify a registered SlackBot by the Slack channel ID.
// It resolves the channel ID to a name using the server-default bot token, then
// searches active bots (those using default credentials) whose AllowedChannelNames matches.
// Returns nil if the bot cannot be identified.
func (h *SlackBotEventHandler) resolveBotByChannel(ctx context.Context, channelID string) *entities.SlackBot {
	if h.channelResolver == nil || h.defaultBotTokenSecretName == "" {
		return nil
	}
	internalRepo, ok := h.repo.(repositories.SlackBotInternalRepository)
	if !ok {
		log.Printf("[SLACKBOT] resolveBotByChannel: internal slackbot repository is not configured")
		return nil
	}
	allBots, err := internalRepo.ListAll(ctx)
	if err != nil {
		log.Printf("[SLACKBOT] resolveBotByChannel: failed to list bots: %v", err)
		return nil
	}

	candidates := make([]*entities.SlackBot, 0, len(allBots))
	for _, candidate := range allBots {
		// Only match bots that rely on the default bot token
		if candidate.BotTokenSecretName() != "" {
			continue
		}
		// Must have at least one AllowedChannelName to be identifiable via the default endpoint
		if len(candidate.AllowedChannelNames()) == 0 {
			continue
		}
		candidates = append(candidates, candidate)
		if candidate.IsChannelNameAllowed(channelID) {
			log.Printf("[SLACKBOT] resolveBotByChannel: matched bot id=%s for channel=%s by channel ID",
				candidate.ID(), channelID)
			return candidate
		}
	}
	if len(candidates) == 0 {
		return nil
	}

	botToken, err := h.channelResolver.GetBotToken(
		ctx,
		h.defaultBotTokenSecretName,
		h.defaultBotTokenSecretKey,
	)
	if err != nil {
		log.Printf("[SLACKBOT] resolveBotByChannel: failed to get default bot token: %v", err)
		return nil
	}
	channelName, err := h.channelResolver.ResolveChannelName(ctx, channelID, botToken)
	if err != nil {
		log.Printf("[SLACKBOT] resolveBotByChannel: failed to resolve channel name: channelID=%s, err=%v", channelID, err)
		return nil
	}
	for _, candidate := range candidates {
		if candidate.IsChannelNameAllowed(channelName) {
			log.Printf("[SLACKBOT] resolveBotByChannel: matched bot id=%s for channel=%s (name=%s)",
				candidate.ID(), channelID, channelName)
			return candidate
		}
	}
	return nil
}

func (h *SlackBotEventHandler) isBotAllowedInChannel(ctx context.Context, bot *entities.SlackBot, channelID string) (bool, error) {
	if bot.IsChannelNameAllowed(channelID) {
		return true, nil
	}

	botToken, err := h.getBotToken(ctx, bot)
	if err != nil {
		return false, err
	}
	channelName, err := h.channelResolver.ResolveChannelName(ctx, channelID, botToken)
	if err != nil {
		return false, err
	}
	return bot.IsChannelNameAllowed(channelName), nil
}

// buildMessage constructs the message to send to the session
func (h *SlackBotEventHandler) buildMessage(bot *entities.SlackBot, payload map[string]interface{}, fallbackText string, isReuse bool) string {
	if bot != nil && bot.SessionConfig() != nil {
		var tmpl string
		if isReuse && bot.SessionConfig().ReuseMessageTemplate() != "" {
			tmpl = bot.SessionConfig().ReuseMessageTemplate()
		} else if bot.SessionConfig().InitialMessageTemplate() != "" {
			tmpl = bot.SessionConfig().InitialMessageTemplate()
		}
		if tmpl != "" {
			rendered, err := configrender.RenderTemplate(tmpl, payload)
			if err == nil {
				return rendered
			}
			log.Printf("[SLACKBOT] Failed to render message template: %v", err)
		}
	}
	return fallbackText
}

// getBotToken retrieves the Slack bot token for the given bot.
// Falls back to the default bot token secret when the bot has no custom one.
func (h *SlackBotEventHandler) getBotToken(ctx context.Context, bot *entities.SlackBot) (string, error) {
	if h.channelResolver == nil {
		return "", fmt.Errorf("channel resolver is nil; cannot get bot token")
	}
	secretName := h.defaultBotTokenSecretName
	secretKey := h.defaultBotTokenSecretKey
	if bot != nil {
		if bot.BotTokenSecretName() != "" {
			secretName = bot.BotTokenSecretName()
		}
		if bot.BotTokenSecretKey() != "" {
			secretKey = bot.BotTokenSecretKey()
		}
	}
	return h.channelResolver.GetBotToken(ctx, secretName, secretKey)
}

// postErrorToSlack posts an error message to the Slack thread where the triggering event occurred.
// This is a best-effort operation; errors are logged but never propagated.
// In dry-run mode the post is only logged and not sent to Slack.
func (h *SlackBotEventHandler) postErrorToSlack(ctx context.Context, channel, threadTS, message, botToken string) {
	if h.channelResolver == nil {
		return
	}
	if h.dryRun {
		log.Printf("[SLACKBOT] [DRY-RUN] Would post error to Slack: channel=%s, thread=%s, message=%q", channel, threadTS, message)
		return
	}
	if err := h.channelResolver.PostMessage(ctx, channel, threadTS, message, botToken); err != nil {
		log.Printf("[SLACKBOT] Failed to post error message to Slack thread: channel=%s, err=%v", channel, err)
	}
}

// fetchAndFormatThreadContext fetches all messages in the Slack thread rooted at threadTS
// and formats them as a human-readable context string. Only messages with ts <= untilTS
// are included (i.e. messages up to and including the triggering event).
// Returns an empty string when no context is available (resolver not configured, token
// error, API error, or no messages found). Errors are logged but never propagated.
func (h *SlackBotEventHandler) fetchAndFormatThreadContext(ctx context.Context, bot *entities.SlackBot, channel, threadTS, untilTS string) string {
	if h.channelResolver == nil {
		return ""
	}

	botToken, err := h.getBotToken(ctx, bot)
	if err != nil {
		log.Printf("[SLACKBOT] fetchAndFormatThreadContext: failed to get bot token: %v", err)
		return ""
	}

	messages, err := h.channelResolver.FetchThreadReplies(ctx, channel, threadTS, botToken)
	if err != nil {
		log.Printf("[SLACKBOT] fetchAndFormatThreadContext: failed to fetch thread replies: channel=%s, thread=%s, err=%v", channel, threadTS, err)
		return ""
	}

	var lines []string
	for _, msg := range messages {
		// Skip messages that are newer than the triggering event
		if untilTS != "" && msg.Ts > untilTS {
			continue
		}
		sender := msg.User
		if sender == "" && msg.BotID != "" {
			sender = fmt.Sprintf("bot(%s)", msg.BotID)
		}
		if sender == "" {
			sender = "unknown"
		}
		lines = append(lines, fmt.Sprintf("[%s]: %s", sender, msg.Text))
	}

	if len(lines) == 0 {
		return ""
	}

	return strings.Join(lines, "\n")
}

// isStopCommand checks whether the Slack message text is a /stop command.
// It strips any bot mention tokens (<@UXXXXXXX>) from the text before comparing.
func isStopCommand(text string) bool {
	// Remove all Slack mention tokens of the form <@USER_ID> or <@USER_ID|username>
	cleaned := text
	for {
		start := strings.Index(cleaned, "<@")
		if start == -1 {
			break
		}
		end := strings.Index(cleaned[start:], ">")
		if end == -1 {
			break
		}
		cleaned = cleaned[:start] + cleaned[start+end+1:]
	}
	return strings.TrimSpace(cleaned) == "/stop"
}

// liveSlackSessions returns sessions that can still accept Slack follow-up messages.
// A session normally transitions from "active" to "running" once the agent starts,
// so filtering only for "active" loses the session for almost its entire lifetime.
func liveSlackSessions(sessions []entities.Session) []entities.Session {
	live := make([]entities.Session, 0, len(sessions))
	for _, session := range sessions {
		switch session.Status() {
		case "creating", "starting", "active", "running":
			live = append(live, session)
		}
	}
	return live
}

// handleStopCommand processes a /stop command by finding a live session for the
// given channel+thread and sending a stop signal (Ctrl+C) to its agent.
// The result (success or failure) is posted back to the Slack thread.
func (h *SlackBotEventHandler) handleStopCommand(ctx context.Context, channel, threadKey string, bot *entities.SlackBot) {
	stopFilter := entities.SessionFilter{
		Tags: map[string]string{
			"slack_channel":   channel,
			"slack_thread_ts": threadKey,
		},
	}
	liveSessions := liveSlackSessions(h.sessionManager.ListSessions(stopFilter))
	if len(liveSessions) == 0 {
		log.Printf("[SLACKBOT] /stop: no live session found for channel=%s, thread=%s", channel, threadKey)
		botToken, tokenErr := h.getBotToken(ctx, bot)
		if tokenErr == nil {
			h.postErrorToSlack(ctx, channel, threadKey,
				":warning: 停止するアクティブなセッションが見つかりません。",
				botToken)
		}
		return
	}

	session := liveSessions[0]
	go func() {
		bgCtx := context.Background()

		if h.dryRun {
			log.Printf("[SLACKBOT] [DRY-RUN] Would stop agent for session %s", session.ID())
			h.postStopConfirmationToSlack(bgCtx, channel, threadKey, bot)
			return
		}

		if err := h.sessionManager.StopAgent(bgCtx, session.ID()); err != nil {
			log.Printf("[SLACKBOT] Failed to stop agent for session %s: %v", session.ID(), err)
			botToken, tokenErr := h.getBotToken(bgCtx, bot)
			if tokenErr == nil {
				h.postErrorToSlack(bgCtx, channel, threadKey,
					fmt.Sprintf(":warning: セッションの停止に失敗しました: %v", err),
					botToken)
			}
			return
		}

		log.Printf("[SLACKBOT] Successfully stopped agent for session %s", session.ID())
		h.postStopConfirmationToSlack(bgCtx, channel, threadKey, bot)
	}()
}

// postStopConfirmationToSlack posts a confirmation message to the Slack thread
// after successfully sending the stop signal to the agent.
func (h *SlackBotEventHandler) postStopConfirmationToSlack(ctx context.Context, channel, threadTS string, bot *entities.SlackBot) {
	if h.channelResolver == nil {
		return
	}
	if h.dryRun {
		log.Printf("[SLACKBOT] [DRY-RUN] Would post stop confirmation to Slack: channel=%s, thread=%s", channel, threadTS)
		return
	}

	botToken, err := h.getBotToken(ctx, bot)
	if err != nil {
		log.Printf("[SLACKBOT] Failed to get bot token for stop confirmation: %v", err)
		return
	}

	message := "セッションを停止しました :stop_sign:"
	if err := h.channelResolver.PostMessage(ctx, channel, threadTS, message, botToken); err != nil {
		log.Printf("[SLACKBOT] Failed to post stop confirmation to Slack: %v", err)
	}
}

// postSessionURLToSlack posts the session URL back to the Slack thread.
// This is a best-effort operation; errors are logged but never propagated.
// In dry-run mode the post is only logged and not sent to Slack.
// When repository is non-empty, it is included in the notification message.
func (h *SlackBotEventHandler) postSessionURLToSlack(ctx context.Context, channel, threadTS, sessionID, repository string, bot *entities.SlackBot) {
	// Determine the base URL: prefer NOTIFICATION_BASE_URL env, then h.baseURL
	sessionBaseURL := os.Getenv("NOTIFICATION_BASE_URL")
	if sessionBaseURL == "" {
		sessionBaseURL = h.baseURL
	}
	if sessionBaseURL == "" {
		log.Printf("[SLACKBOT] Skipping session URL notification: no base URL configured (set NOTIFICATION_BASE_URL or webhook.base_url)")
		return
	}

	sessionURL := fmt.Sprintf("%s/sessions/%s", strings.TrimRight(sessionBaseURL, "/"), sessionID)
	var message string
	if repository != "" {
		message = fmt.Sprintf("セッションを作成しました :robot_face: (repository: `%s`)\n%s", repository, sessionURL)
	} else {
		message = fmt.Sprintf("セッションを作成しました :robot_face:\n%s", sessionURL)
	}

	if h.dryRun {
		log.Printf("[SLACKBOT] [DRY-RUN] Would post to Slack: channel=%s, thread=%s, message=%q", channel, threadTS, message)
		return
	}

	if h.channelResolver == nil {
		return
	}

	botToken, err := h.getBotToken(ctx, bot)
	if err != nil {
		log.Printf("[SLACKBOT] Failed to get bot token for Slack notification: %v", err)
		return
	}

	if err := h.channelResolver.PostMessage(ctx, channel, threadTS, message, botToken); err != nil {
		log.Printf("[SLACKBOT] Failed to post session URL to Slack thread: %v", err)
		return
	}

	log.Printf("[SLACKBOT] Posted session URL to Slack thread: sessionID=%s, channel=%s, thread=%s", sessionID, channel, threadTS)
}

// parseRepository searches the entire message text for the first "org/repo" identifier.
// Slack mention tokens (<@UXXXXXXXX>) are removed before searching.
// When multiple matches exist, the first one is returned.
// Returns an empty string if no match is found.
func parseRepository(text string) string {
	cleaned := slackMentionRe.ReplaceAllString(text, "")
	// Pad with spaces so the word-boundary pattern matches at start/end of string.
	m := repoRe.FindStringSubmatch(" " + cleaned + " ")
	if len(m) >= 2 {
		return m[1]
	}
	return ""
}
