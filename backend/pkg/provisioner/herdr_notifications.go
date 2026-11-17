package provisioner

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os/exec"
	"strings"
	"time"
)

const herdrNotificationBridgeName = "ccplant-session-agent"
const herdrNotificationBridgeLabel = "ccplant.system=notification-bridge"
const localAgentRequestTimeout = 30 * time.Second

type herdrTerminal struct {
	Address string `json:"address"`
	State   string `json:"state"`
}

func ensureHerdrNotificationBridge(ctx context.Context, env []string, agentType, agentSocket string) (string, error) {
	if terminal, err := getHerdrNotificationBridge(ctx, env); err == nil {
		if terminal.State == "live" && strings.TrimSpace(terminal.Address) != "" {
			label := exec.CommandContext(ctx, "hc", "label", terminal.Address, herdrNotificationBridgeLabel)
			label.Env = env
			if output, err := label.CombinedOutput(); err != nil {
				return "", fmt.Errorf("label existing hc notification bridge: %w: %s", err, strings.TrimSpace(string(output)))
			}
			return terminal.Address, nil
		}
		// Finished terminals retain their names until removed. Clear a stale
		// bridge so the replacement can keep using the stable session name.
		rm := exec.CommandContext(ctx, "hc", "rm", herdrNotificationBridgeName)
		rm.Env = env
		if output, err := rm.CombinedOutput(); err != nil {
			return "", fmt.Errorf("remove stale hc notification bridge: %w: %s", err, strings.TrimSpace(string(output)))
		}
	}

	cmd := exec.CommandContext(ctx,
		"hc", "--json", "run", "--bg",
		"--name", herdrNotificationBridgeName,
		"--label", herdrNotificationBridgeLabel,
		"--no-notify",
		environmentValue(env, "CCPLANT_BINARY_PATH", "ccplant"),
		"agent-provisioner", "hc-notification-bridge",
		"--agent-type", agentType,
		"--agent-socket", agentSocket,
	)
	cmd.Env = env
	output, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("run hc notification bridge: %w: %s", err, strings.TrimSpace(string(output)))
	}
	var terminal herdrTerminal
	if err := json.Unmarshal(output, &terminal); err != nil {
		return "", fmt.Errorf("decode hc notification bridge: %w", err)
	}
	if strings.TrimSpace(terminal.Address) == "" {
		return "", fmt.Errorf("hc notification bridge returned no terminal address")
	}
	return terminal.Address, nil
}

func environmentValue(env []string, key, fallback string) string {
	prefix := key + "="
	for i := len(env) - 1; i >= 0; i-- {
		if strings.HasPrefix(env[i], prefix) {
			if value := strings.TrimSpace(strings.TrimPrefix(env[i], prefix)); value != "" {
				return value
			}
			return fallback
		}
	}
	return fallback
}

// RunHerdrNotificationBridge forwards hc parent notes directly to the local
// agent API, using ACP session/prompt for ACP-backed agents.
func RunHerdrNotificationBridge(ctx context.Context, input io.Reader, output io.Writer, agentType, agentSocket string) error {
	if _, err := fmt.Fprint(output, "\033]7501;state=working:app=ccplant-hc-bridge\033\\"); err != nil {
		return err
	}
	scanner := bufio.NewScanner(input)
	scanner.Buffer(make([]byte, 4096), 1<<20)
	for sequence := 1; scanner.Scan(); sequence++ {
		message := strings.TrimSpace(scanner.Text())
		if message == "" {
			continue
		}
		payload, _ := json.Marshal(map[string]string{"content": message})
		commandCtx, cancel := context.WithTimeout(ctx, localAgentRequestTimeout)
		err := executeControlCommandAtBase(commandCtx, unixSocketHTTPClient(agentSocket), localAgentControlType(agentType), controlCommand{
			ID:      fmt.Sprintf("hc-notification-%d", sequence),
			Type:    "prompt",
			Payload: payload,
		}, "http://unix")
		cancel()
		if err != nil {
			_, _ = fmt.Fprintf(output, "hc notification delivery failed: %v\n", err)
		}
	}
	return scanner.Err()
}

func localAgentControlType(agentType string) string {
	switch agentType {
	case "pi", "cursor":
		return agentType + "-acp"
	default:
		return agentType
	}
}

func getHerdrNotificationBridge(ctx context.Context, env []string) (herdrTerminal, error) {
	cmd := exec.CommandContext(ctx, "hc", "--json", "get", herdrNotificationBridgeName)
	cmd.Env = env
	output, err := cmd.CombinedOutput()
	if err != nil {
		return herdrTerminal{}, err
	}
	var terminal herdrTerminal
	if err := json.Unmarshal(output, &terminal); err != nil {
		return herdrTerminal{}, err
	}
	return terminal, nil
}
