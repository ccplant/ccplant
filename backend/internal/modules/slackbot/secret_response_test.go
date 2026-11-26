package slackbot

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/takutakahashi/agentapi-proxy/internal/domain/entities"
)

func TestSecretResponseSlackBotIsWriteOnly(t *testing.T) {
	const secret = "formal-secret-sentinel"
	bot := entities.NewSlackBot("id", "bot", "user")
	config := entities.NewWebhookSessionConfig()
	config.SetEnvironment(map[string]string{"TOKEN": secret})
	bot.SetSessionConfig(config)
	encoded, err := json.Marshal(ToResponse(bot))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(encoded), secret) {
		t.Fatalf("slackbot response disclosed secret: %s", encoded)
	}
}

func TestSecretPreserveSlackBotEnvironmentOnOmittedUpdate(t *testing.T) {
	got := mergeSlackBotSecretMap(map[string]string{"TOKEN": "old"}, nil)
	if got["TOKEN"] != "old" {
		t.Fatalf("omitted environment erased existing secret: %#v", got)
	}
}
