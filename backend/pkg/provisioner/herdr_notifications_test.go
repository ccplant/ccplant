package provisioner

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestHerdrNotificationBridgeNormalizesDirectRuntimeEndpoint(t *testing.T) {
	dir := t.TempDir()
	marker := filepath.Join(dir, "args")
	client := filepath.Join(dir, "ccplant")
	if err := os.WriteFile(client, []byte("#!/bin/sh\nprintf '%s\\n' \"$*\" > \"$HC_TEST_MARKER\"\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("sh", "-c", herdrNotificationBridgeScript)
	cmd.Env = append(os.Environ(),
		"CCPLANT_BINARY_PATH="+client,
		"AGENTAPI_PROXY_ENDPOINT=https://proxy.example/api/v1",
		"HC_TEST_MARKER="+marker,
	)
	cmd.Stdin = bytes.NewBufferString("<hc>child exited</hc>\n")
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("bridge failed: %v: %s", err, output)
	}
	args, err := os.ReadFile(marker)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := strings.TrimSpace(string(args)), "client send --endpoint https://proxy.example <hc>child exited</hc>"; got != want {
		t.Fatalf("arguments = %q, want %q", got, want)
	}
}

func TestEnsureHerdrNotificationBridgeReusesLiveTerminal(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("PATH", dir+":"+os.Getenv("PATH"))
	writeFakeHC(t, dir, `
if [ "$2" = "get" ]; then
  printf '%s\n' '{"address":"t9","state":"live"}'
  exit 0
fi
exit 1
`)

	address, err := ensureHerdrNotificationBridge(context.Background(), os.Environ())
	if err != nil {
		t.Fatal(err)
	}
	if address != "t9" {
		t.Fatalf("address = %q, want t9", address)
	}
}

func TestEnsureHerdrNotificationBridgeStartsTerminal(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("PATH", dir+":"+os.Getenv("PATH"))
	marker := filepath.Join(dir, "args")
	writeFakeHC(t, dir, `
printf '%s\n' "$*" >> "$HC_TEST_MARKER"
if [ "$2" = "get" ]; then
  exit 1
fi
printf '%s\n' '{"address":"t12","state":"live"}'
`)
	env := append(os.Environ(), "HC_TEST_MARKER="+marker)

	address, err := ensureHerdrNotificationBridge(context.Background(), env)
	if err != nil {
		t.Fatal(err)
	}
	if address != "t12" {
		t.Fatalf("address = %q, want t12", address)
	}
	args, err := os.ReadFile(marker)
	if err != nil {
		t.Fatal(err)
	}
	got := string(args)
	if !strings.Contains(got, "run --bg --name "+herdrNotificationBridgeName+" --no-notify sh -c") {
		t.Fatalf("hc run arguments missing from %q", got)
	}
}

func writeFakeHC(t *testing.T, dir, script string) {
	t.Helper()
	path := filepath.Join(dir, "hc")
	if err := os.WriteFile(path, []byte("#!/bin/sh\n"+script), 0o755); err != nil {
		t.Fatal(err)
	}
}
