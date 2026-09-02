// Package provisioner provides the session Pod provisioner. The provisioner
// exposes local health/status endpoints and pulls provision requests from the
// proxy internal API.
package provisioner

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"

	"github.com/takutakahashi/agentapi-proxy/pkg/sessionsettings"
)

const restartAutoProvisionDelay = 15 * time.Second

// Status represents the provisioning lifecycle state.
type Status string

const (
	// StatusPending means no provisioning has been triggered yet.
	StatusPending Status = "pending"
	// StatusProvisioning means provisioning is currently in progress.
	StatusProvisioning Status = "provisioning"
	// StatusReady means provisioning completed successfully and agentapi is running.
	StatusReady Status = "ready"
	// StatusError means provisioning failed.
	StatusError Status = "error"
)

// StatusResponse is the JSON body returned by GET /status.
type StatusResponse struct {
	Status  Status `json:"status"`
	Message string `json:"message,omitempty"`
}

// Server is the agent-provisioner HTTP server.
type Server struct {
	port         int
	settingsFile string // path to optional auto-provision settings file
	httpClient   *http.Client
	filterURL    string

	mu          sync.RWMutex
	status      Status
	message     string
	phase       string
	phaseTime   time.Time
	serverCtx   context.Context // long-lived context for provisioning goroutines
	reporter    func(Status, string)
	startupDone chan struct{}

	provisionMu     sync.Mutex
	provisionCancel context.CancelFunc
	provisionDone   chan struct{}
}

// New creates a new Server.
//
//   - port:         TCP port to listen on (e.g. 9001)
//   - settingsFile: path to /session-settings/settings.yaml; if this file
//     exists at startup the server auto-provisions from it (Pod restart case).
func New(port int, settingsFile string) *Server {
	return &Server{
		port:         port,
		settingsFile: settingsFile,
		httpClient:   http.DefaultClient,
		filterURL:    "http://127.0.0.1:3129",
		status:       StatusPending,
		phase:        "starting",
		phaseTime:    time.Now(),
		startupDone:  make(chan struct{}),
	}
}

// Start starts the HTTP server and blocks until ctx is cancelled or a fatal
// error occurs.
//
// If settingsFile exists at startup (Pod restart case), provisioning is
// started automatically in the background before the HTTP server begins
// accepting requests.
func (s *Server) Start(ctx context.Context) error {
	// Store the server-level context so that provisioning goroutines survive
	// beyond the HTTP request that triggered them.
	s.serverCtx = ctx

	// Run an explicitly configured startup pre-script in the background. Agent
	// packages are part of the image, so the default startup path stays offline.
	go s.runStartupScript(ctx)

	// Auto-provision from Secret volume if available (Pod restart case). Give
	// the pull client time to claim an initial provision request first: the
	// restart Secret intentionally exists before the first Pod is created so it
	// can survive an early eviction, but it must not start a second agent in the
	// normal initial-provisioning path.
	if s.settingsFile != "" {
		if _, err := os.Stat(s.settingsFile); err == nil {
			log.Printf("[PROVISIONER] Settings file found at %s – scheduling restart auto-provisioning", s.settingsFile)
			settings, err := sessionsettings.LoadSettings(s.settingsFile)
			if err != nil {
				log.Printf("[PROVISIONER] Failed to load settings for auto-provisioning: %v", err)
			} else {
				go func() {
					timer := time.NewTimer(restartAutoProvisionDelay)
					defer timer.Stop()
					select {
					case <-ctx.Done():
						return
					case <-timer.C:
					}
					if !s.claimProvisioning() {
						status := s.GetStatus()
						log.Printf("[PROVISIONER] Skipping restart auto-provisioning because status is %s", status)
						return
					}
					log.Printf("[PROVISIONER] No initial provision request claimed; auto-provisioning from %s", s.settingsFile)
					s.startProvision(settings)
				}()
			}
		}
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/livez", s.handleLivez)
	mux.HandleFunc("/healthz", s.handleHealthz)
	mux.HandleFunc("/status", s.handleStatus)
	mux.HandleFunc("/sandbox-domains", s.handleSandboxDomains)
	mux.HandleFunc("/sandbox-policy", s.handleSandboxPolicy)

	srv := &http.Server{
		Addr:    fmt.Sprintf(":%d", s.port),
		Handler: mux,
	}

	// Shutdown when context is cancelled.
	go func() {
		<-ctx.Done()
		status, msg, phase, elapsed := s.snapshot()
		log.Printf("[PROVISIONER] Context cancelled, shutting down HTTP server (status=%s, message=%q, phase=%q, phase_elapsed=%s, err=%v)", status, msg, phase, elapsed.Round(time.Millisecond), ctx.Err())
		_ = srv.Shutdown(context.Background())
	}()

	log.Printf("[PROVISIONER] Listening on :%d", s.port)
	if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		return fmt.Errorf("provisioner server error: %w", err)
	}
	return nil
}

// startProvision starts a new agent generation. Callers must stop the previous
// generation first when reloading an already provisioned session.
func (s *Server) startProvision(settings *sessionsettings.SessionSettings) {
	s.provisionMu.Lock()
	ctx, cancel := context.WithCancel(s.serverCtx)
	done := make(chan struct{})
	s.provisionCancel = cancel
	s.provisionDone = done
	s.provisionMu.Unlock()

	go func() {
		defer close(done)
		s.runProvision(ctx, settings)
	}()
}

// ReloadProvision stops the current agent subprocess generation and starts a
// new one from settings fetched through the authenticated session-control API.
// The provisioner, container, Pod and persistent workspace remain alive.
func (s *Server) ReloadProvision(settings *sessionsettings.SessionSettings) error {
	if settings == nil {
		return fmt.Errorf("reload settings are required")
	}
	s.provisionMu.Lock()
	cancel := s.provisionCancel
	done := s.provisionDone
	s.provisionMu.Unlock()
	if cancel == nil || done == nil {
		return fmt.Errorf("agent process has not been provisioned")
	}

	cancel()
	select {
	case <-done:
	case <-time.After(30 * time.Second):
		return fmt.Errorf("timed out stopping the current agent process")
	}
	s.setStatus(StatusProvisioning, "reloading session settings")
	s.startProvision(settings)
	return nil
}

func (s *Server) handleLivez(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(`{"ok":true}`))
}

// handleHealthz keeps stock runners out of the allocator until their startup
// prefetch has finished. Without this gate a runner can be claimed while the
// network-filter sidecar is still starting, causing ACP package installation
// to fail and the allocated session to time out.
func (s *Server) handleHealthz(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	select {
	case <-s.startupDone:
	default:
		w.WriteHeader(http.StatusServiceUnavailable)
		_, _ = w.Write([]byte(`{"ok":false,"reason":"startup prefetch in progress"}`))
		return
	}
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(`{"ok":true}`))
}

// handleStatus returns the current provisioning state as JSON.
// When the provisioner is in an error state, it returns HTTP 500 so that
// clients can distinguish a permanent failure from a transient startup delay.
func (s *Server) handleStatus(w http.ResponseWriter, r *http.Request) {
	s.mu.RLock()
	resp := StatusResponse{Status: s.status, Message: s.message}
	s.mu.RUnlock()

	w.Header().Set("Content-Type", "application/json")
	if resp.Status == StatusError {
		w.WriteHeader(http.StatusInternalServerError)
	}
	if err := json.NewEncoder(w).Encode(resp); err != nil {
		log.Printf("[PROVISIONER] Failed to encode status response: %v", err)
	}
}

// setStatus updates the provisioning state thread-safely.
func (s *Server) setStatus(st Status, msg string) {
	s.mu.Lock()
	s.status = st
	s.message = msg
	reporter := s.reporter
	s.mu.Unlock()
	log.Printf("[PROVISIONER] Status changed to %s%s", st, func() string {
		if msg != "" {
			return ": " + msg
		}
		return ""
	}())
	if reporter != nil {
		reporter(st, msg)
	}
}

func (s *Server) setPhase(phase string) {
	s.mu.Lock()
	s.phase = phase
	s.phaseTime = time.Now()
	s.mu.Unlock()
	log.Printf("[PROVISIONER] Phase changed to %s", phase)
}

func (s *Server) snapshot() (Status, string, string, time.Duration) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.status, s.message, s.phase, time.Since(s.phaseTime)
}

// GetStatus returns the current status (used by tests).
func (s *Server) GetStatus() Status {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.status
}

// claimProvisioning atomically reserves the single provisioning slot.
func (s *Server) claimProvisioning() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.status != StatusPending {
		return false
	}
	s.status = StatusProvisioning
	s.message = ""
	return true
}

// SetStatusReporter installs a callback invoked on every status transition.
func (s *Server) SetStatusReporter(fn func(Status, string)) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.reporter = fn
}

// handleSandboxDomains proxies GET /sandbox-domains to the network filter control
// server (127.0.0.1:3129/domains) and returns the accessed domain list.
// Returns 503 when the network filter is not running (no sandbox sidecar).
func (s *Server) handleSandboxDomains(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	resp, err := s.client().Get(s.controlURL() + "/domains") //nolint:noctx
	if err != nil {
		http.Error(w, "network filter not available", http.StatusServiceUnavailable)
		return
	}
	defer resp.Body.Close() //nolint:errcheck

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(resp.StatusCode)
	_, _ = io.Copy(w, resp.Body)
}

// handleSandboxPolicy replaces the running network filter policy. This is used
// when a pre-warmed stock Pod is adopted for a session with its own sandbox
// policy, allowing the policy to change without restarting the Pod.
func (s *Server) handleSandboxPolicy(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	req, err := http.NewRequestWithContext(r.Context(), http.MethodPost, s.controlURL()+"/policy", r.Body)
	if err != nil {
		http.Error(w, "invalid network filter request", http.StatusInternalServerError)
		return
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := s.client().Do(req)
	if err != nil {
		http.Error(w, "network filter not available", http.StatusServiceUnavailable)
		return
	}
	defer resp.Body.Close() //nolint:errcheck

	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(resp.StatusCode)
	_, _ = io.Copy(w, resp.Body)
}

func (s *Server) client() *http.Client {
	if s.httpClient != nil {
		return s.httpClient
	}
	return http.DefaultClient
}

func (s *Server) controlURL() string {
	if s.filterURL != "" {
		return s.filterURL
	}
	return "http://127.0.0.1:3129"
}

// runStartupScript executes PROVISIONER_PRE_SCRIPT, when explicitly configured,
// as soon as the Pod starts. Agent packages are baked into the image; an empty
// value therefore completes immediately without contacting a package registry.
// Failure is non-fatal: a warning is logged and the server continues normally.
func (s *Server) runStartupScript(ctx context.Context) {
	defer close(s.startupDone)
	script := strings.TrimSpace(os.Getenv("PROVISIONER_PRE_SCRIPT"))
	if script == "" {
		log.Printf("[PROVISIONER] Agent packages are preinstalled; skipping startup pre-script")
		return
	}
	if os.Getenv("AGENTAPI_SCIA_SESSION_SIDECAR_ENABLED") == "true" {
		waitForSciaProxy(ctx, "http://127.0.0.1:18081", 15*time.Second)
	}
	waitForLocalTCP(ctx, "127.0.0.1:3128", 30*time.Second)
	log.Printf("[PROVISIONER] Running startup pre-script")
	cmd := exec.CommandContext(ctx, "sh", "-c", script)
	cmd.Env = withoutProxyEnv(os.Environ())
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		log.Printf("[PROVISIONER] Warning: startup pre-script failed (continuing): %v", err)
	} else {
		log.Printf("[PROVISIONER] Startup pre-script complete")
	}
}

func waitForLocalTCP(ctx context.Context, address string, timeout time.Duration) {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		conn, err := (&net.Dialer{Timeout: 500 * time.Millisecond}).DialContext(ctx, "tcp", address)
		if err == nil {
			_ = conn.Close()
			log.Printf("[PROVISIONER] local network proxy is ready at %s", address)
			return
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(250 * time.Millisecond):
		}
	}
	log.Printf("[PROVISIONER] Warning: local network proxy did not become ready before timeout: %s", address)
}

func withoutProxyEnv(env []string) []string {
	filtered := make([]string, 0, len(env))
	for _, entry := range env {
		key, _, ok := strings.Cut(entry, "=")
		if !ok {
			filtered = append(filtered, entry)
			continue
		}
		switch key {
		case "HTTP_PROXY", "HTTPS_PROXY", "http_proxy", "https_proxy",
			"SSL_CERT_FILE", "REQUESTS_CA_BUNDLE", "CURL_CA_BUNDLE", "GIT_SSL_CAINFO", "NODE_EXTRA_CA_CERTS",
			"AGENTAPI_SCIA_PROXY_URL", "AGENTAPI_SCIA_GOOGLE_CREDENTIAL", "AGENTAPI_SCIA_USER_NAMESPACE":
			continue
		default:
			filtered = append(filtered, entry)
		}
	}
	return filtered
}
