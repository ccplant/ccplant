package webhook

import (
	"fmt"
	"strings"

	"github.com/takutakahashi/agentapi-proxy/internal/core/configrender"
	"github.com/takutakahashi/agentapi-proxy/internal/domain/entities"
)

func (s *WebhookSessionService) renderConfigMap(
	sessionConfig *entities.WebhookSessionConfig,
	payload map[string]interface{},
	getter func(*entities.WebhookSessionConfig) map[string]string,
) (map[string]string, error) {
	if sessionConfig == nil {
		return make(map[string]string), nil
	}
	values := getter(sessionConfig)
	if values == nil {
		return make(map[string]string), nil
	}
	return configrender.RenderTemplateMap(values, payload)
}

// determineInitialMessage determines the initial message to use for a session.
// Priority: params.message > initial_message_template > defaultMessage
func (s *WebhookSessionService) determineInitialMessage(
	sessionConfig *entities.WebhookSessionConfig,
	renderedParams *entities.SessionParams,
	payload map[string]interface{},
	defaultMessage string,
) (string, error) {
	if renderedParams != nil && renderedParams.Message != "" {
		return renderedParams.Message, nil
	}

	if sessionConfig != nil && sessionConfig.InitialMessageTemplate() != "" {
		msg, err := configrender.RenderTemplate(sessionConfig.InitialMessageTemplate(), payload)
		if err != nil {
			return "", fmt.Errorf("failed to render initial message template: %w", err)
		}
		return msg, nil
	}

	return defaultMessage, nil
}

func firstNonEmptyTag(tags map[string]string, keys ...string) string {
	for _, key := range keys {
		if value := strings.TrimSpace(tags[key]); value != "" {
			return value
		}
	}
	return ""
}
