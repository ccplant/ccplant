package webhook

import (
	"github.com/takutakahashi/agentapi-proxy/internal/domain/entities"
)

func truncateString(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}

// MatchTriggersForTest evaluates triggers against a test payload.
func (c *WebhookCustomController) MatchTriggersForTest(
	triggers []entities.WebhookTrigger,
	payload map[string]interface{},
) *entities.WebhookTrigger {
	return c.matchTriggers(triggers, payload)
}

// BuildCustomTagsForTest builds custom webhook metadata tags for a test payload.
func (c *WebhookCustomController) BuildCustomTagsForTest(
	webhook *entities.Webhook,
	trigger *entities.WebhookTrigger,
) map[string]string {
	return map[string]string{
		"webhook_id":   webhook.ID(),
		"webhook_name": webhook.Name(),
		"webhook_type": string(webhook.WebhookType()),
		"trigger_id":   trigger.ID(),
		"trigger_name": trigger.Name(),
	}
}

// BuildDefaultMessageForTest builds a default message for a test payload.
func (c *WebhookCustomController) BuildDefaultMessageForTest(payload map[string]interface{}) string {
	return buildCustomDefaultMessage(payload)
}

// SessionService returns the session service for external access.
func (c *WebhookCustomController) SessionService() *WebhookSessionService {
	return c.sessionService
}
