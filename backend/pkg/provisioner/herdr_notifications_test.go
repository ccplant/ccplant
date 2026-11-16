package provisioner

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRunHerdrNotificationBridgeSendsACPPrompt(t *testing.T) {
	var rpcBody map[string]interface{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method + " " + r.URL.Path {
		case http.MethodGet + " /session":
			_ = json.NewEncoder(w).Encode(map[string]string{"sessionId": "acp-session"})
		case http.MethodPost + " /rpc":
			if err := json.NewDecoder(r.Body).Decode(&rpcBody); err != nil {
				t.Fatal(err)
			}
			w.WriteHeader(http.StatusAccepted)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	var output bytes.Buffer
	if err := RunHerdrNotificationBridge(context.Background(), bytes.NewBufferString("<hc>child exited</hc>\n"), &output, "codex-acp", server.URL); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(output.String(), "state=working:app=ccplant-hc-bridge") {
		t.Fatalf("bridge status missing from %q", output.String())
	}
	if rpcBody["method"] != "session/prompt" {
		t.Fatalf("RPC method = %v, want session/prompt", rpcBody["method"])
	}
	params := rpcBody["params"].(map[string]interface{})
	if params["sessionId"] != "acp-session" {
		t.Fatalf("session ID = %v, want acp-session", params["sessionId"])
	}
	prompt := params["prompt"].([]interface{})[0].(map[string]interface{})
	if prompt["text"] != "<hc>child exited</hc>" {
		t.Fatalf("prompt text = %v", prompt["text"])
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

	address, err := ensureHerdrNotificationBridge(context.Background(), os.Environ(), "codex-acp")
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

	address, err := ensureHerdrNotificationBridge(context.Background(), env, "codex-acp")
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
	if !strings.Contains(got, "run --bg --name "+herdrNotificationBridgeName+" --no-notify ccplant agent-provisioner hc-notification-bridge --agent-type codex-acp") {
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
