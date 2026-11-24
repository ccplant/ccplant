package provisioner

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/takutakahashi/agentapi-proxy/pkg/proxybinary"
	"github.com/takutakahashi/agentapi-proxy/pkg/sessionsettings"
)

func (s *Server) configureCLIBrokerForSession(settings *sessionsettings.SessionSettings) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.brokerKey = settings.Env["AGENTAPI_KEY"]
	s.brokerSessionID = settings.Session.ID
	if endpoint := strings.TrimSpace(settings.Env["AGENTAPI_PROXY_ENDPOINT"]); endpoint != "" && !strings.HasPrefix(endpoint, "unix:") && !strings.HasPrefix(endpoint, "http+unix:") {
		s.brokerURL = strings.TrimRight(endpoint, "/")
	}
}

func waitForMountedSettingsSanitized(ctx context.Context, path string, timeout time.Duration) error {
	if strings.TrimSpace(path) == "" {
		return nil
	}
	deadline := time.NewTimer(timeout)
	defer deadline.Stop()
	ticker := time.NewTicker(200 * time.Millisecond)
	defer ticker.Stop()
	for {
		data, err := os.ReadFile(path)
		if os.IsNotExist(err) {
			return nil
		}
		if err == nil {
			mounted, parseErr := sessionsettings.LoadSettingsFromBytes(data)
			if parseErr == nil {
				if _, present := mounted.Env["AGENTAPI_KEY"]; !present {
					return nil
				}
			}
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-deadline.C:
			return fmt.Errorf("timed out waiting for %s to drop AGENTAPI_KEY", path)
		case <-ticker.C:
		}
	}
}

func usesACPUnixSocket(agentType string) bool {
	switch agentType {
	case "claude-acp", "codex-acp", "pi", "cursor", "antigravity":
		return true
	default:
		return false
	}
}

func shouldImplicitlyRestoreSessionState(settings *sessionsettings.SessionSettings) bool {
	return settings != nil && settings.Session.PersistenceEnabled && strings.TrimSpace(os.Getenv("AGENTAPI_NATIVE_SESSION_ROOT")) == ""
}

func shouldRequireConversationResume(settings *sessionsettings.SessionSettings) bool {
	return settings != nil && (settings.Restart || strings.TrimSpace(settings.Session.ResumeFrom) != "")
}

func injectUsageReportingHook(settings *sessionsettings.SessionSettings) {
	if settings == nil || !settings.UsageReportingEnabled {
		return
	}
	agentType := settings.Session.AgentType
	command := fmt.Sprintf("%s client report-usage --agent-type %s >> /tmp/usage-report.log 2>&1", proxybinary.ShellReference(), shellQuote(agentType))
	hook := map[string]interface{}{"hooks": []interface{}{map[string]interface{}{"type": "command", "command": command, "timeout": 15}}}
	appendStop := func(root map[string]interface{}) map[string]interface{} {
		if root == nil {
			root = map[string]interface{}{}
		}
		hooks, _ := root["hooks"].(map[string]interface{})
		if hooks == nil {
			hooks = map[string]interface{}{}
		}
		// Usage must run before destructive oneshot hooks remove the session.
		hooks["Stop"] = append([]interface{}{hook}, asInterfaceSlice(hooks["Stop"])...)
		root["hooks"] = hooks
		return root
	}
	settings.Claude.SettingsJSON = appendStop(settings.Claude.SettingsJSON)
	settings.Codex.HooksJSON = appendStop(settings.Codex.HooksJSON)
}

func shellQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "'\\''") + "'"
}
