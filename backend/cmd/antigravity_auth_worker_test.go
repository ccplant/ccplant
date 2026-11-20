package cmd

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCollectAntigravityAuthBundleExcludesRuntimeData(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	root := filepath.Join(home, ".gemini", "antigravity-cli")
	if err := os.MkdirAll(filepath.Join(root, "log"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "auth-token.json"), []byte(`{"refresh_token":"secret"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "log", "cli.log"), []byte("noise"), 0o600); err != nil {
		t.Fatal(err)
	}
	raw, err := collectAntigravityAuthBundle()
	if err != nil {
		t.Fatal(err)
	}
	var bundle antigravityAuthBundle
	if err := json.Unmarshal(raw, &bundle); err != nil {
		t.Fatal(err)
	}
	if len(bundle.Files) != 1 || bundle.Files[0].Path != "auth-token.json" {
		t.Fatalf("unexpected bundle: %+v", bundle)
	}
}

func TestBoundedAntigravityError(t *testing.T) {
	message := boundedAntigravityError(errors.New(strings.Repeat("x", 4096)))
	if len(message) != 2048 {
		t.Fatalf("bounded error length = %d", len(message))
	}
}

func TestSendHCTextUsesKeysWithoutDraftWait(t *testing.T) {
	dir := t.TempDir()
	argsPath := filepath.Join(dir, "args")
	script := "#!/bin/sh\nprintf '%s\\n' \"$@\" > \"$HC_TEST_ARGS\"\nprintf '%s\\n' '{}'\n"
	if err := os.WriteFile(filepath.Join(dir, "hc"), []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("HC_TEST_ARGS", argsPath)
	if err := sendHCText(t.Context(), "t-secret", "Ab:/?x=1&y_z"); err != nil {
		t.Fatal(err)
	}
	args, err := os.ReadFile(argsPath)
	if err != nil {
		t.Fatal(err)
	}
	want := "keys\nt-secret\nshift+a\nb\nshift+;\n/\nshift+/\nx\n=\n1\nshift+7\ny\nshift+-\nz\nenter\n--json\n"
	if string(args) != want {
		t.Fatalf("hc arguments = %q", args)
	}
}

func TestSendAntigravitySubscriptionSelectsAndConfirms(t *testing.T) {
	dir := t.TempDir()
	argsPath := filepath.Join(dir, "args")
	script := "#!/bin/sh\nprintf '%s\\n' \"$@\" > \"$HC_TEST_ARGS\"\nprintf '%s\\n' '{}'\n"
	if err := os.WriteFile(filepath.Join(dir, "hc"), []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("HC_TEST_ARGS", argsPath)
	if err := sendAntigravityTerminalInput(t.Context(), "t-auth", antigravitySubscriptionPrefix+"2"); err != nil {
		t.Fatal(err)
	}
	args, err := os.ReadFile(argsPath)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := string(args), "keys\nt-auth\ndown\ndown\nenter\n--json\n"; got != want {
		t.Fatalf("hc arguments = %q, want %q", got, want)
	}
}

func TestReadAntigravityTerminalIncludesScrollbackAfterExit(t *testing.T) {
	dir := t.TempDir()
	argsPath := filepath.Join(dir, "args")
	script := `#!/bin/sh
printf '%s\n' "$*" >> "$HC_TEST_ARGS"
case "$1" in
  get) printf '%s\n' '{"state":"finished","exit_code":0}' ;;
  read) printf '%s\n' '{"text":"Select available subscription:\n1. Gemini Enterprise"}' ;;
esac
`
	if err := os.WriteFile(filepath.Join(dir, "hc"), []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("HC_TEST_ARGS", argsPath)

	transcript, finished, exitCode, err := readAntigravityTerminal(t.Context(), "t-auth")
	if err != nil {
		t.Fatal(err)
	}
	if !finished || exitCode != 0 || !strings.Contains(transcript, "Gemini Enterprise") {
		t.Fatalf("unexpected terminal result: transcript=%q finished=%v exit=%d", transcript, finished, exitCode)
	}
	args, err := os.ReadFile(argsPath)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := string(args), "get t-auth --json\nread t-auth --json --lines 200\n"; got != want {
		t.Fatalf("hc arguments = %q, want %q", got, want)
	}
}
