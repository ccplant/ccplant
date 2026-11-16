package provisioner

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"
)

const herdrNotificationBridgeName = "ccplant-session-agent"

// The bridge is an hc parent terminal for the session agent. hc types child
// notifications into this terminal, and the loop forwards each one through
// the normal session message API so ACP and non-ACP agents receive them alike.
const herdrNotificationBridgeScript = `printf '\033]7501;state=working:app=ccplant-hc-bridge\033\\'
ccplant_binary="${CCPLANT_BINARY_PATH:-ccplant}"
proxy_endpoint="${AGENTAPI_PROXY_ENDPOINT:-}"
case "$proxy_endpoint" in
  */api/v1) proxy_endpoint="${proxy_endpoint%/api/v1}" ;;
esac
while IFS= read -r message; do
  [ -n "$message" ] || continue
  if [ -n "$proxy_endpoint" ]; then
    "$ccplant_binary" client send --endpoint "$proxy_endpoint" "$message"
  else
    "$ccplant_binary" client send "$message"
  fi
done`

type herdrTerminal struct {
	Address string `json:"address"`
	State   string `json:"state"`
}

func ensureHerdrNotificationBridge(ctx context.Context, env []string) (string, error) {
	if terminal, err := getHerdrNotificationBridge(ctx, env); err == nil {
		if terminal.State == "live" && strings.TrimSpace(terminal.Address) != "" {
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
		"--no-notify",
		"sh", "-c", herdrNotificationBridgeScript,
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
