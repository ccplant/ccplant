package provisioner

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestServerListensOnUnixSocketWithoutTCP(t *testing.T) {
	socketPath := filepath.Join(t.TempDir(), "provisioner.sock")
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- NewWithSocket(socketPath, "").Start(ctx) }()

	deadline := time.Now().Add(3 * time.Second)
	for {
		if info, err := os.Stat(socketPath); err == nil && info.Mode()&os.ModeSocket != 0 {
			break
		}
		if time.Now().After(deadline) {
			cancel()
			t.Fatal("provisioner Unix socket was not created")
		}
		time.Sleep(10 * time.Millisecond)
	}

	resp, err := unixSocketHTTPClient(socketPath).Get("http://unix/livez")
	if err != nil {
		cancel()
		t.Fatalf("GET /livez through Unix socket: %v", err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != 200 {
		cancel()
		t.Fatalf("GET /livez status = %d, want 200", resp.StatusCode)
	}

	cancel()
	if err := <-done; err != nil {
		t.Fatalf("server shutdown: %v", err)
	}
	if _, err := os.Stat(socketPath); !os.IsNotExist(err) {
		t.Fatalf("socket remains after shutdown: %v", err)
	}
}
