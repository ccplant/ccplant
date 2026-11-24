package slackbot

import (
	"context"
	"fmt"
	"log"

	"github.com/takutakahashi/agentapi-proxy/internal/domain/entities"
)

func (h *SlackBotEventHandler) resolveEventBot(ctx context.Context, botID string, event *SlackEvent, threadKey string) (*entities.SlackBot, string, error) {
	bot, err := h.resolveSlackBot(ctx, botID, event.Channel)
	if err != nil {
		return nil, botID, fmt.Errorf("failed to resolve slackbot: %w", err)
	}
	if botID != slackBotDefaultID || bot != nil {
		return bot, botID, nil
	}

	if event.ThreadTs != "" {
		bot = h.resolveBotFromReusableSession(ctx, event.Channel, threadKey)
	}
	if bot == nil {
		bot = h.resolveBotByChannel(ctx, event.Channel)
	}
	if bot == nil {
		log.Printf("[SLACKBOT] Default endpoint: no matching bot found for channel=%s, dropping event", event.Channel)
		if botToken, tokenErr := h.getBotToken(ctx, nil); tokenErr == nil {
			h.postErrorToSlack(ctx, event.Channel, threadKey, ":warning: このチャンネルに対応する bot が登録されていません。チャンネルの設定を確認してください。", botToken)
		}
		return nil, botID, nil
	}
	log.Printf("[SLACKBOT] Default endpoint: identified bot: id=%s, channel=%s", bot.ID(), event.Channel)
	return bot, bot.ID(), nil
}

func (h *SlackBotEventHandler) shouldIgnoreEvent(ctx context.Context, botID string, bot *entities.SlackBot, event *SlackEvent) (bool, error) {
	if bot == nil {
		return true, nil
	}
	if bot.Status() == entities.SlackBotStatusPaused {
		log.Printf("[SLACKBOT] Bot is paused, ignoring all events: id=%s", botID)
		return true, nil
	}
	if !bot.AllowBotMessages() && (event.BotID != "" || event.SubType == "bot_message") {
		log.Printf("[SLACKBOT] Ignoring bot message: botID=%s, event.bot_id=%s, subtype=%s", botID, event.BotID, event.SubType)
		return true, nil
	}
	if !bot.IsEventTypeAllowed(event.Type) || !bot.IsUserIDAllowed(event.User) {
		log.Printf("[SLACKBOT] Event or user not allowed: id=%s, type=%s, user=%s", botID, event.Type, event.User)
		return true, nil
	}
	if len(bot.AllowedChannelNames()) == 0 || h.channelResolver == nil {
		return false, nil
	}
	allowed, err := h.isBotAllowedInChannel(ctx, bot, event.Channel)
	if err != nil {
		if botToken, tokenErr := h.getBotToken(ctx, bot); tokenErr == nil {
			h.postErrorToSlack(ctx, event.Channel, eventThreadKey(event), ":warning: チャンネル情報を取得できませんでした。しばらく待ってから再度お試しください。", botToken)
		}
		return true, fmt.Errorf("failed to resolve channel name for channel %s: %w", event.Channel, err)
	}
	if !allowed {
		log.Printf("[SLACKBOT] Channel not allowed: id=%s, channel=%s", botID, event.Channel)
	}
	return !allowed, nil
}

func eventThreadKey(event *SlackEvent) string {
	if event.ThreadTs != "" {
		return event.ThreadTs
	}
	return event.Ts
}
