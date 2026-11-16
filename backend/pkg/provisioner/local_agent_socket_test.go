package provisioner

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestLocalAgentSocketProxyForwardsAndRestrictsAccess(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/session" {
			t.Fatalf("path = %q, want /session", r.URL.Path)
		}
		_, _ = io.WriteString(w, `{"sessionId":"socket-session"}`)
	}))
	defer upstream.Close()

	socketPath := filepath.Join(t.TempDir(), "private", "agent.sock")
	ctx, cancel := context.WithCancel(context.Background())
	stop, err := startLocalAgentSocketProxy(ctx, socketPath, upstream.URL)
	if err != nil {
		t.Fatal(err)
	}
	defer stop()

	dirInfo, err := os.Stat(filepath.Dir(socketPath))
	if err != nil {
		t.Fatal(err)
	}
	if got := dirInfo.Mode().Perm(); got != 0o700 {
		t.Fatalf("socket directory permissions = %o, want 700", got)
	}
	socketInfo, err := os.Stat(socketPath)
	if err != nil {
		t.Fatal(err)
	}
	if got := socketInfo.Mode().Perm(); got != 0o600 {
		t.Fatalf("socket permissions = %o, want 600", got)
	}

	resp, err := unixSocketHTTPClient(socketPath).Get("http://unix/session")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}

	cancel()
	stop()
	if _, err := os.Lstat(socketPath); !os.IsNotExist(err) {
		t.Fatalf("socket still exists after shutdown: %v", err)
	}
}

func TestLocalAgentSocketProxyRefusesNonSocketPath(t *testing.T) {
	socketPath := filepath.Join(t.TempDir(), "agent.sock")
	if err := os.WriteFile(socketPath, []byte("keep"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := startLocalAgentSocketProxy(context.Background(), socketPath, "http://127.0.0.1:9000"); err == nil {
		t.Fatal("expected non-socket path to be rejected")
	}
	contents, err := os.ReadFile(socketPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(contents) != "keep" {
		t.Fatalf("non-socket path was modified: %q", contents)
	}
}
