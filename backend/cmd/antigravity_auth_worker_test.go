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

func TestSendHCTextUsesRPCStdin(t *testing.T) {
	dir := t.TempDir()
	argsPath := filepath.Join(dir, "args")
	requestPath := filepath.Join(dir, "request")
	script := "#!/bin/sh\nprintf '%s\\n' \"$@\" > \"$HC_TEST_ARGS\"\ncat > \"$HC_TEST_REQUEST\"\nprintf '%s\\n' '{\"jsonrpc\":\"2.0\",\"id\":1,\"result\":{}}'\n"
	if err := os.WriteFile(filepath.Join(dir, "hc"), []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("HC_TEST_ARGS", argsPath)
	t.Setenv("HC_TEST_REQUEST", requestPath)
	if err := sendHCText(t.Context(), "t-secret", "authorization-code"); err != nil {
		t.Fatal(err)
	}
	args, err := os.ReadFile(argsPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(args) != "connect\n" {
		t.Fatalf("hc arguments = %q", args)
	}
	request, err := os.ReadFile(requestPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(request), `"text":"authorization-code"`) {
		t.Fatalf("RPC request did not contain terminal input: %s", request)
	}
}
