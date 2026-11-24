package provisioner

import (
	"context"
	"encoding/json"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/takutakahashi/agentapi-proxy/pkg/sessionsettings"
	closeutils "github.com/takutakahashi/agentapi-proxy/pkg/utils"
)

func (s *Server) SetRestartSettingsHandler(handler func(*sessionsettings.SessionSettings)) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.restartSettingsHandler = handler
}

func (s *Server) handleLivez(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	writeProvisionerResponse(w, []byte(`{"ok":true}`))
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
		writeProvisionerResponse(w, []byte(`{"ok":false,"reason":"startup prefetch in progress"}`))
		return
	}
	w.WriteHeader(http.StatusOK)
	writeProvisionerResponse(w, []byte(`{"ok":true}`))
}

func writeProvisionerResponse(writer io.Writer, body []byte) {
	written, err := writer.Write(body)
	if err != nil {
		log.Printf("[PROVISIONER] Failed to write response: %v", err)
	} else if written != len(body) {
		log.Printf("[PROVISIONER] Failed to write complete response: %d of %d bytes", written, len(body))
	}
}

// handleStatus returns the current provisioning state as JSON.
// When the provisioner is in an error state, it returns HTTP 500 so that
// clients can distinguish a permanent failure from a transient startup delay.
func (s *Server) handleStatus(w http.ResponseWriter, r *http.Request) {
	s.mu.RLock()
	resp := StatusResponse{Status: s.status, Message: s.message, ErrorCode: s.errorCode, RestartID: s.restartID}
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
	s.errorCode = classifyProvisionError(msg)
	reporter := s.reporter
	errorCode := s.errorCode
	s.mu.Unlock()
	log.Printf("[PROVISIONER] Status changed to %s%s", st, func() string {
		if msg != "" {
			return ": " + msg
		}
		return ""
	}())
	if reporter != nil {
		reporter(st, msg, errorCode)
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
	s.errorCode = ""
	return true
}

// SetStatusReporter installs a callback invoked on every status transition.
func (s *Server) SetStatusReporter(fn func(Status, string, string)) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.reporter = fn
}

const codexAuthRequiredErrorCode = "codex_auth_required"

func classifyProvisionError(message string) string {
	if strings.Contains(strings.ToLower(message), "authentication required") {
		return codexAuthRequiredErrorCode
	}
	return ""
}

// handleSandboxDomains proxies GET /sandbox-domains to the network filter control
// server (127.0.0.1:3129/domains) and returns the accessed domain list.
// Returns 503 when the network filter is not running (no sandbox sidecar).
func (s *Server) handleSandboxDomains(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	resp, err := s.filterClient().Get(s.controlURL() + "/domains") //nolint:noctx
	if err != nil {
		http.Error(w, "network filter not available", http.StatusServiceUnavailable)
		return
	}
	defer func() { closeutils.SafeCloseResponseBody(resp.Body) }()

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(resp.StatusCode)
	copyProvisionerResponse(w, resp.Body)
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
	resp, err := s.filterClient().Do(req)
	if err != nil {
		http.Error(w, "network filter not available", http.StatusServiceUnavailable)
		return
	}
	defer func() { closeutils.SafeCloseResponseBody(resp.Body) }()

	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(resp.StatusCode)
	copyProvisionerResponse(w, resp.Body)
}

func copyProvisionerResponse(writer io.Writer, reader io.Reader) {
	written, err := io.Copy(writer, reader)
	if err != nil {
		log.Printf("[PROVISIONER] Failed to proxy response after %d bytes: %v", written, err)
	}
}

func (s *Server) filterClient() *http.Client {
	if s.filterHTTPClient != nil {
		return s.filterHTTPClient
	}
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
			if err := conn.Close(); err != nil {
				log.Printf("[PROVISIONER] Failed to close readiness connection: %v", err)
			}
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
	if data, err := os.ReadFile("/run/nfa/network-filter.log"); err == nil && len(data) > 0 {
		log.Printf("[PROVISIONER] network-filter startup log:\n%s", strings.TrimSpace(string(data)))
	}
}

func withoutProxyEnv(env []string) []string {
	filtered := make([]string, 0, len(env))
	for _, entry := range env {
		separator := strings.IndexByte(entry, '=')
		if separator < 0 {
			filtered = append(filtered, entry)
			continue
		}
		key := entry[:separator]
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
