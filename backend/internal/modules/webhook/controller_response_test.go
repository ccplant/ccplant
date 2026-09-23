package webhook

import (
	"testing"

	"github.com/takutakahashi/agentapi-proxy/internal/domain/entities"
)

func TestSessionConfigToResponseIncludesNonSensitiveParams(t *testing.T) {
	config := entities.NewWebhookSessionConfig()
	config.SetParams(&entities.SessionParams{
		CredentialSource: "triggered_user",
		SessionTTL:       "48h",
	})

	response := (&WebhookController{}).sessionConfigToResponse(config)

	if response.Params == nil {
		t.Fatal("response params must not be nil")
	}
	if got := response.Params.CredentialSource; got != "triggered_user" {
		t.Fatalf("credential source = %q, want triggered_user", got)
	}
	if got := response.Params.SessionTTL; got != "48h" {
		t.Fatalf("session TTL = %q, want 48h", got)
	}
}
