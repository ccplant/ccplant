package configrender

import (
	"testing"

	"github.com/takutakahashi/agentapi-proxy/internal/domain/entities"
)

func TestRenderSessionParamsPreservesCredentialSource(t *testing.T) {
	config := entities.NewWebhookSessionConfig()
	config.SetParams(&entities.SessionParams{CredentialSource: "triggered_user", Model: "{{ .model }}"})

	got, err := RenderSessionParams(config, map[string]interface{}{"model": "gpt-test"})
	if err != nil {
		t.Fatalf("RenderSessionParams() error = %v", err)
	}
	if got == nil {
		t.Fatal("RenderSessionParams() returned nil")
	}
	if got.CredentialSource != "triggered_user" {
		t.Fatalf("CredentialSource = %q, want triggered_user", got.CredentialSource)
	}
	if got.Model != "gpt-test" {
		t.Fatalf("Model = %q, want gpt-test", got.Model)
	}
}
