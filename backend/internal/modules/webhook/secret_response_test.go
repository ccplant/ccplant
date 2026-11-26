package webhook

import (
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/labstack/echo/v4"
	"github.com/takutakahashi/agentapi-proxy/internal/domain/entities"
)

func TestSecretResponseWebhookReadIsWriteOnly(t *testing.T) {
	const secret = "formal-secret-sentinel"
	hook := entities.NewWebhook("id", "hook", "user", entities.WebhookTypeCustom)
	hook.SetSecret(secret)
	config := entities.NewWebhookSessionConfig()
	config.SetEnvironment(map[string]string{"TOKEN": secret})
	hook.SetSessionConfig(config)

	e := echo.New()
	ctx := e.NewContext(httptest.NewRequest("GET", "/webhooks/id", nil), httptest.NewRecorder())
	encoded, err := json.Marshal(NewWebhookController(nil).toResponse(ctx, hook, false))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(encoded), secret) {
		t.Fatalf("webhook read response disclosed secret: %s", encoded)
	}
}

func TestSecretPreserveWebhookEnvironmentOnOmittedUpdate(t *testing.T) {
	got := mergeWebhookSecretMap(map[string]string{"TOKEN": "old"}, nil)
	if got["TOKEN"] != "old" {
		t.Fatalf("omitted environment erased existing secret: %#v", got)
	}
}
