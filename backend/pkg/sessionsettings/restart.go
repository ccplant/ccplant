package sessionsettings

import (
	"fmt"
	"github.com/pelletier/go-toml/v2"
	"github.com/takutakahashi/agentapi-proxy/pkg/modelprovider"
	"reflect"
)

// ValidateRestart rejects incompatible changes before stopping the running agent.
func ValidateRestart(old, next *SessionSettings) error {
	if old == nil || next == nil {
		return fmt.Errorf("session settings are missing")
	}
	if old.Session.AgentType != "claude-acp" && old.Session.AgentType != "codex-acp" {
		return fmt.Errorf("conversation resume supports only Claude ACP and Codex ACP")
	}
	if old.Session.AgentType != next.Session.AgentType {
		return fmt.Errorf("agent type cannot change when resuming a conversation")
	}
	if old.Session.Oneshot || next.Session.Oneshot {
		return fmt.Errorf("oneshot sessions cannot restart")
	}
	if !reflect.DeepEqual(old.Repository, next.Repository) {
		return fmt.Errorf("repository settings cannot change when resuming a conversation")
	}
	oldRouting, err := restartRouting(old)
	if err != nil {
		return err
	}
	newRouting, err := restartRouting(next)
	if err != nil {
		return err
	}
	if !reflect.DeepEqual(oldRouting, newRouting) {
		return fmt.Errorf("model or provider routing cannot change when resuming")
	}
	// Match the persisted model-connection identity without comparing secret values.
	a, b := old.CodexConnection.Clone(), next.CodexConnection.Clone()
	if !sameConnectionIdentity(a, b) {
		return fmt.Errorf("Codex connection or model cannot change when resuming")
	}
	a, b = old.ClaudeConnection.Clone(), next.ClaudeConnection.Clone()
	if !sameConnectionIdentity(a, b) {
		return fmt.Errorf("Claude connection or model cannot change when resuming")
	}
	return nil
}

func sameConnectionIdentity(a, b *modelprovider.Connection) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	if a.Mode != b.Mode {
		return false
	}
	return !a.Compatible() || (a.BaseURL == b.BaseURL && a.Model == b.Model && a.Authentication == b.Authentication)
}

// PrepareRestart removes outputs owned by the previous snapshot which are no
// longer present. It never removes a path supplied by the new settings.
func PrepareRestart(old, next *SessionSettings) {
	files := map[string]bool{}
	for _, f := range next.Files {
		files[f.Path] = true
	}
	for _, f := range old.Files {
		if !files[f.Path] {
			next.RemoveFiles = append(next.RemoveFiles, f.Path)
		}
	}
	for key := range old.Env {
		if _, ok := next.Env[key]; !ok {
			next.UnsetEnv = append(next.UnsetEnv, key)
		}
	}
	next.Session.ID = old.Session.ID
	next.Session.PersistenceEnabled = old.Session.PersistenceEnabled
	next.ParentRuntime = old.ParentRuntime
	next.Restart = true
	next.Paused = false
	next.Session.ResumeFrom = next.Session.ID
	next.InitialMessage = ""
	next.WebhookPayload = ""
}

func restartRouting(s *SessionSettings) (map[string]interface{}, error) {
	routing := map[string]interface{}{}
	keys := []string{"ANTHROPIC_MODEL", "ANTHROPIC_BASE_URL", "CLAUDE_CODE_USE_BEDROCK", "CLAUDE_CODE_USE_VERTEX", "CLAUDE_CODE_USE_FOUNDRY"}
	if s.Session.AgentType == "codex-acp" {
		keys = []string{"OPENAI_MODEL", "OPENAI_BASE_URL"}
		var config map[string]interface{}
		if err := toml.Unmarshal([]byte(s.Codex.ConfigTOML), &config); err != nil {
			return nil, fmt.Errorf("invalid Codex configuration")
		}
		routing["model"] = config["model"]
		routing["model_provider"] = config["model_provider"]
		if providers, ok := config["model_providers"].(map[string]interface{}); ok {
			if provider, ok := providers[fmt.Sprint(config["model_provider"])].(map[string]interface{}); ok {
				routing["base_url"] = provider["base_url"]
				routing["wire_api"] = provider["wire_api"]
			}
		}
	} else {
		routing["model"] = s.Claude.SettingsJSON["model"]
	}
	for _, key := range keys {
		routing[key] = s.Env[key]
	}
	return routing, nil
}

// RestartValidationRequest supplies the actual running snapshot for pooled runners,
// whose manager only retains the unassigned stock configuration.
type RestartValidationRequest struct {
	*SessionSettings
	CurrentSettings *SessionSettings `json:"current_settings,omitempty"`
}
