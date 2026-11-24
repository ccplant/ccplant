package slackbot

import (
	"context"
	"fmt"
	"log"
	"os"
	"strings"

	"github.com/takutakahashi/agentapi-proxy/internal/domain/entities"
)

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
