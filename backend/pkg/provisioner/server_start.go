package provisioner

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"strconv"
	"time"

	"github.com/takutakahashi/agentapi-proxy/pkg/sessionsettings"
)

// Start starts the HTTP server and blocks until ctx is cancelled or a fatal error occurs.
func (s *Server) Start(ctx context.Context) (returnErr error) {
	s.serverCtx = ctx
	go s.runStartupScript(ctx)
	s.scheduleRestartAutoProvision(ctx)
	srv := &http.Server{Handler: s.provisionerMux()}
	listener, err := s.provisionerListener(srv)
	if err != nil {
		return fmt.Errorf("listen provisioner: %w", err)
	}
	defer func() {
		if closeErr := listener.Close(); closeErr != nil && !errors.Is(closeErr, net.ErrClosed) {
			returnErr = errors.Join(returnErr, closeErr)
		}
		if s.socketPath != "" {
			if removeErr := os.Remove(s.socketPath); removeErr != nil && !os.IsNotExist(removeErr) {
				returnErr = errors.Join(returnErr, removeErr)
			}
		}
	}()
	go s.shutdownProvisioner(ctx, srv)
	log.Printf("[PROVISIONER] Listening on %s", listener.Addr())
	if err := srv.Serve(listener); err != nil && err != http.ErrServerClosed {
		return fmt.Errorf("provisioner server error: %w", err)
	}
	return nil
}

func (s *Server) scheduleRestartAutoProvision(ctx context.Context) {
	if s.settingsFile == "" {
		return
	}
	if _, err := os.Stat(s.settingsFile); err != nil {
		return
	}
	log.Printf("[PROVISIONER] Settings file found at %s – scheduling restart auto-provisioning", s.settingsFile)
	settings, err := sessionsettings.LoadSettings(s.settingsFile)
	if err != nil {
		log.Printf("[PROVISIONER] Failed to load settings for auto-provisioning: %v", err)
		return
	}
	go s.runRestartAutoProvision(ctx, settings)
}

func (s *Server) runRestartAutoProvision(ctx context.Context, settings *sessionsettings.SessionSettings) {
	if !s.waitForRestartAutoProvision(ctx, restartAutoProvisionDelay) {
		return
	}
	if !s.claimProvisioning() {
		log.Printf("[PROVISIONER] Skipping restart auto-provisioning because status is %s", s.GetStatus())
		return
	}
	log.Printf("[PROVISIONER] No initial provision request claimed; auto-provisioning from %s", s.settingsFile)
	s.mu.RLock()
	handler := s.restartSettingsHandler
	s.mu.RUnlock()
	if handler != nil {
		handler(settings)
	}
	s.runProvision(ctx, settings)
}

func (s *Server) provisionerMux() *http.ServeMux {
	mux := http.NewServeMux()
	mux.HandleFunc("/livez", s.handleLivez)
	mux.HandleFunc("/healthz", s.handleHealthz)
	mux.HandleFunc("/status", s.handleStatus)
	mux.HandleFunc("/pause", s.handlePauseAgent)
	mux.HandleFunc("/restart", s.handleRestartAgent)
	mux.HandleFunc("/sandbox-domains", s.handleSandboxDomains)
	mux.HandleFunc("/sandbox-policy", s.handleSandboxPolicy)
	mux.HandleFunc("/one-time-secrets/", s.handleOneTimeSecret)
	mux.HandleFunc("/herdr/rpc", s.handleHerdrRPC)
	mux.HandleFunc("/notifications/send", s.handleCLIBroker)
	mux.HandleFunc("/internal/usage-events", s.handleCLIBroker)
	mux.HandleFunc("/sessions/", s.handleCLIBroker)
	mux.HandleFunc("/", s.handleCLIBroker)
	return mux
}

func (s *Server) provisionerListener(server *http.Server) (net.Listener, error) {
	if s.socketPath == "" {
		server.Addr = net.JoinHostPort(s.host, strconv.Itoa(s.port))
		return net.Listen("tcp", server.Addr)
	}
	if err := prepareLocalSocketPath(s.socketPath); err != nil {
		return nil, err
	}
	listener, err := net.Listen("unix", s.socketPath)
	if err != nil {
		return nil, err
	}
	if err := os.Chmod(s.socketPath, 0o600); err != nil {
		return nil, errors.Join(err, listener.Close())
	}
	return listener, nil
}

func (s *Server) shutdownProvisioner(ctx context.Context, server *http.Server) {
	<-ctx.Done()
	status, message, phase, elapsed := s.snapshot()
	log.Printf("[PROVISIONER] Context cancelled, shutting down HTTP server (status=%s, message=%q, phase=%q, phase_elapsed=%s, err=%v)", status, message, phase, elapsed.Round(time.Millisecond), ctx.Err())
	if err := server.Shutdown(context.Background()); err != nil {
		log.Printf("[PROVISIONER] HTTP server shutdown failed: %v", err)
	}
}
