package controllers

import (
	"bytes"
	"encoding/json"
	"fmt"

	"github.com/takutakahashi/agentapi-proxy/internal/domain/entities"
	"github.com/takutakahashi/agentapi-proxy/pkg/modelprovider"
)

func applyProfileConnections(cfg *entities.SessionProfileConfig, previous entities.SessionProfileConfig, request SessionProfileConfigRequest) error {
	codex, err := mergeProfileConnection(previous.CodexConnection(), request.CodexConnection, "codex")
	if err != nil {
		return err
	}
	claude, err := mergeProfileConnection(previous.ClaudeConnection(), request.ClaudeConnection, "claude")
	if err != nil {
		return err
	}
	cfg.SetCodexConnection(codex)
	cfg.SetClaudeConnection(claude)
	return nil
}

// Omitted retains the stored connection; null explicitly restores inheritance.
// An omitted API key keeps only this profile's key, never a settings key.
func mergeProfileConnection(previous *modelprovider.Connection, raw json.RawMessage, agent string) (*modelprovider.Connection, error) {
	if len(raw) == 0 {
		return previous.Clone(), nil
	}
	if bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
		return nil, nil
	}
	var patch map[string]json.RawMessage
	if err := json.Unmarshal(raw, &patch); err != nil {
		return nil, fmt.Errorf("invalid profile connection")
	}
	seed := previous.Clone()
	if seed == nil {
		seed = &modelprovider.Connection{}
	}
	// A profile may inherit just the default model. All other compatible API
	// validation (URL, key, auth scheme) remains identical to settings.
	model := seed.Model
	if value, ok := patch["model"]; ok {
		if err := json.Unmarshal(value, &model); err != nil {
			return nil, fmt.Errorf("invalid profile model")
		}
	}
	if model == "" {
		seed.Model = "inherited-model"
		delete(patch, "model")
	}
	result, err := mergeModelConnection(seed, patch, agent)
	if err != nil {
		return nil, err
	}
	if !result.Compatible() {
		return nil, fmt.Errorf("profile connections require a compatible API mode")
	}
	if model == "" {
		result.Model = ""
	}
	return result, nil
}
