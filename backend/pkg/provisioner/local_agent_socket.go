package provisioner

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
)

const defaultLocalAgentSocketPath = "/tmp/ccplant/agent.sock"

func localAgentSocketPath() string {
	if value := os.Getenv("AGENTAPI_LOCAL_SOCKET"); value != "" {
		return value
	}
	return nativeRuntimePath("agent", "agent.sock", defaultLocalAgentSocketPath)
}

func prepareLocalSocketPath(socketPath string) error {
	if !filepath.IsAbs(socketPath) {
		return fmt.Errorf("local agent socket path must be absolute")
	}
	dir := filepath.Dir(socketPath)
	info, err := os.Lstat(dir)
	switch {
	case errors.Is(err, os.ErrNotExist):
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return fmt.Errorf("create local agent socket directory: %w", err)
		}
	case err != nil:
		return fmt.Errorf("inspect local agent socket directory: %w", err)
	case !info.IsDir():
		return fmt.Errorf("local agent socket directory is not a directory")
	}
	if err := os.Chmod(dir, 0o700); err != nil {
		return fmt.Errorf("restrict local agent socket directory permissions: %w", err)
	}

	info, err = os.Lstat(socketPath)
	switch {
	case errors.Is(err, os.ErrNotExist):
		return nil
	case err != nil:
		return fmt.Errorf("inspect existing local agent socket: %w", err)
	case info.Mode()&os.ModeSocket == 0:
		return fmt.Errorf("refusing to replace non-socket local agent path")
	default:
		if err := os.Remove(socketPath); err != nil {
			return fmt.Errorf("remove stale local agent socket: %w", err)
		}
		return nil
	}
}

func unixSocketHTTPClient(socketPath string) *http.Client {
	transport := &http.Transport{
		DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
			return (&net.Dialer{}).DialContext(ctx, "unix", socketPath)
		},
	}
	return &http.Client{Transport: transport, Timeout: localAgentRequestTimeout}
}
