//go:build e2e

package api_test

import (
	"context"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestCommandHelp(t *testing.T) {
	_, source, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("resolve E2E source path")
	}
	backendDir := filepath.Join(filepath.Dir(source), "..", "..", "..", "backend")
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	rootHelp := runCommandHelp(t, ctx, backendDir, ".", "--help")
	for _, command := range []string{"client", "admin", "native", "session-manager", "control-plane"} {
		if !strings.Contains(rootHelp, command) {
			t.Errorf("root help does not list %q", command)
		}
	}

	clientHelp := runCommandHelp(t, ctx, backendDir, ".", "client", "--help")
	for _, command := range []string{"schedule", "slackbot", "user", "webhook"} {
		if !strings.Contains(clientHelp, command) {
			t.Errorf("client help does not list %q", command)
		}
	}
}

func runCommandHelp(t *testing.T, ctx context.Context, dir string, args ...string) string {
	t.Helper()
	cmd := exec.CommandContext(ctx, "go", append([]string{"run"}, args...)...)
	cmd.Dir = dir
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("ccplant %s failed: %v\n%s", strings.Join(args[1:], " "), err, output)
	}
	return string(output)
}
