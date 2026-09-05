package controllers

import (
 "testing"
 "github.com/stretchr/testify/require"
 "github.com/takutakahashi/agentapi-proxy/internal/domain/entities"
)

func TestProfileAuthMethodsValidationAndMerge(t *testing.T) {
 cfg := entities.NewSessionProfileConfig()
 cfg.SetParams(&entities.SessionParams{CodexAuthMode: "oauth"})
 require.Error(t, validateSessionProfileConfig(cfg))
 cfg.SetParams(&entities.SessionParams{CodexAuthMode: "auth_json", ClaudeAuthMode: "anthropic_compatible"})
 require.NoError(t, validateSessionProfileConfig(cfg))
 merged := mergeSessionParams(cfg.Params(), &entities.SessionParams{ClaudeAuthMode: "oauth"})
 require.Equal(t, "auth_json", merged.CodexAuthMode)
 require.Equal(t, "oauth", merged.ClaudeAuthMode)
 require.Equal(t, "anthropic_compatible", cfg.Params().ClaudeAuthMode)
}
