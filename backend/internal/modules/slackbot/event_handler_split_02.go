package slackbot

import (
	"context"
	"fmt"
	"log"
	"strings"

	"github.com/takutakahashi/agentapi-proxy/internal/core/configrender"
	"github.com/takutakahashi/agentapi-proxy/internal/domain/entities"
	"github.com/takutakahashi/agentapi-proxy/internal/usecases/ports/repositories"
	"github.com/takutakahashi/agentapi-proxy/pkg/telemetry"
)

func (h *SlackBotEventHandler) resolveSlackBot(ctx context.Context, id, channelID string) (*entities.SlackBot, error) {
	if id == slackBotDefaultID {
		return nil, nil
	}
	if strings.HasPrefix(id, settingsSecretBotKeyPrefix) {
		return h.resolveBotBySettingsSecret(ctx, strings.TrimPrefix(id, settingsSecretBotKeyPrefix), channelID)
	}

	bot, err := h.repo.Get(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("slackbot not found: %s", id)
	}
	return bot, nil
}

func (h *SlackBotEventHandler) resolveBotBySettingsSecret(ctx context.Context, secretID, channelID string) (*entities.SlackBot, error) {
	internalRepo, ok := h.repo.(repositories.SlackBotInternalRepository)
	if !ok {
		return nil, fmt.Errorf("internal slackbot repository is not configured")
	}
	bots, err := internalRepo.ListAll(ctx)
	if err != nil {
		return nil, err
	}
	candidates := make([]*entities.SlackBot, 0)
	for _, bot := range bots {
		if bot.SettingsSecretID() == secretID && bot.Status() == entities.SlackBotStatusActive {
			if bot.IsChannelNameAllowed(channelID) {
				return bot, nil
			}
			candidates = append(candidates, bot)
		}
	}
	if len(candidates) > 0 {
		resolver, ok := h.channelResolver.(interface {
			GetSettingsSecretValue(context.Context, string, string, string) (string, error)
		})
		if ok {
			owner := candidates[0].UserID()
			if candidates[0].Scope() == entities.ScopeTeam {
				owner = candidates[0].TeamID()
			}
			token, tokenErr := resolver.GetSettingsSecretValue(ctx, owner, secretID, candidates[0].SettingsBotTokenKey())
			if tokenErr == nil {
				if name, resolveErr := h.channelResolver.ResolveChannelName(ctx, channelID, token); resolveErr == nil {
					for _, candidate := range candidates {
						if candidate.IsChannelNameAllowed(name) {
							return candidate, nil
						}
					}
				}
			}
		}
	}
	return nil, fmt.Errorf("no active slackbot uses settings secret %s", secretID)
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
	rendered, err := renderSlackBotMessage(bot, payload, fallbackText, isReuse)
	if err != nil {
		log.Printf("[SLACKBOT] Failed to render message template: %v", err)
		return fallbackText
	}
	return rendered
}

func renderSlackBotMessage(bot *entities.SlackBot, payload map[string]interface{}, fallback string, reuse bool) (string, error) {
	if bot != nil && bot.SessionConfig() != nil {
		tmpl := bot.SessionConfig().InitialMessageTemplate()
		if reuse && bot.SessionConfig().ReuseMessageTemplate() != "" {
			tmpl = bot.SessionConfig().ReuseMessageTemplate()
		}
		if tmpl != "" {
			return configrender.RenderTemplate(tmpl, payload)
		}
	}
	return fallback, nil
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
		if bot.SettingsSecretID() != "" {
			settingsName := bot.UserID()
			if bot.Scope() == entities.ScopeTeam {
				settingsName = bot.TeamID()
			}
			resolver, ok := h.channelResolver.(interface {
				GetSettingsSecretValue(context.Context, string, string, string) (string, error)
			})
			if !ok {
				return "", fmt.Errorf("channel resolver does not support settings secrets")
			}
			return resolver.GetSettingsSecretValue(ctx, settingsName, bot.SettingsSecretID(), bot.SettingsBotTokenKey())
		}
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
	h.runDeferred(func() {
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
	})
}

// postStopConfirmationToSlack posts a confirmation message to the Slack thread
// after successfully sending the stop signal to the agent.
