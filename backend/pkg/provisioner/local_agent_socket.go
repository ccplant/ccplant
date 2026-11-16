package provisioner

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"path/filepath"
	"sync"
)

const defaultLocalAgentSocketPath = "/tmp/ccplant/agent.sock"

func localAgentSocketPath() string {
	if value := os.Getenv("AGENTAPI_LOCAL_SOCKET"); value != "" {
		return value
	}
	return nativeRuntimePath("agent", "agent.sock", defaultLocalAgentSocketPath)
}

// startLocalAgentSocketProxy exposes the existing agent HTTP API through a
// process-local Unix socket. The TCP listener remains available for existing
// session routing, while local integrations can avoid the Pod network.
func startLocalAgentSocketProxy(ctx context.Context, socketPath, agentURL string) (func(), error) {
	target, err := url.Parse(agentURL)
	if err != nil {
		return nil, fmt.Errorf("parse local agent URL: %w", err)
	}
	if target.Scheme != "http" || target.Host == "" {
		return nil, fmt.Errorf("local agent URL must be an absolute HTTP URL")
	}
	if err := prepareLocalSocketPath(socketPath); err != nil {
		return nil, err
	}

	listener, err := net.Listen("unix", socketPath)
	if err != nil {
		return nil, fmt.Errorf("listen on local agent socket: %w", err)
	}
	if err := os.Chmod(socketPath, 0o600); err != nil {
		_ = listener.Close()
		_ = os.Remove(socketPath)
		return nil, fmt.Errorf("restrict local agent socket permissions: %w", err)
	}
	socketInfo, err := os.Lstat(socketPath)
	if err != nil {
		_ = listener.Close()
		return nil, fmt.Errorf("inspect local agent socket: %w", err)
	}

	proxy := httputil.NewSingleHostReverseProxy(target)
	proxy.ErrorHandler = func(w http.ResponseWriter, _ *http.Request, proxyErr error) {
		log.Printf("[PROVISIONER] Local agent socket proxy failed: %v", proxyErr)
		http.Error(w, "local agent unavailable", http.StatusBadGateway)
	}
	server := &http.Server{Handler: proxy, ReadHeaderTimeout: localAgentRequestTimeout}
	done := make(chan struct{})
	go func() {
		defer close(done)
		if serveErr := server.Serve(listener); serveErr != nil && !errors.Is(serveErr, http.ErrServerClosed) {
			log.Printf("[PROVISIONER] Local agent socket server stopped: %v", serveErr)
		}
	}()

	var stopOnce sync.Once
	stop := func() {
		stopOnce.Do(func() {
			_ = server.Close()
			_ = listener.Close()
			<-done
			if current, statErr := os.Lstat(socketPath); statErr == nil && os.SameFile(socketInfo, current) {
				_ = os.Remove(socketPath)
			}
		})
	}
	go func() {
		<-ctx.Done()
		stop()
	}()
	return stop, nil
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
