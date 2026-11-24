// Package provisioner provides the session Pod provisioner. The provisioner
// exposes local health/status endpoints and pulls provision requests from the
// proxy internal API.
package provisioner

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"

	"github.com/takutakahashi/agentapi-proxy/pkg/sessionsettings"
	closeutils "github.com/takutakahashi/agentapi-proxy/pkg/utils"
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
	RestartID string `json:"restart_id,omitempty"`
	Status    Status `json:"status"`
	Message   string `json:"message,omitempty"`
	ErrorCode string `json:"error_code,omitempty"`
}

// Server is the agent-provisioner HTTP server.
type Server struct {
	lifecycleMu    sync.Mutex
	agentCancel    context.CancelFunc
	provisionDone  chan struct{}
	agentExited    chan struct{}
	agentWorkers   sync.WaitGroup
	activeSettings *sessionsettings.SessionSettings
	restartID      string

	port             int
	host             string
	socketPath       string
	settingsFile     string // path to optional auto-provision settings file
	httpClient       *http.Client
	filterHTTPClient *http.Client
	filterURL        string
	brokerURL        string
	brokerKey        string
	brokerSessionID  string

	mu                     sync.RWMutex
	status                 Status
	message                string
	errorCode              string
	phase                  string
	phaseTime              time.Time
	serverCtx              context.Context // long-lived context for provisioning goroutines
	reporter               func(Status, string, string)
	restartSettingsHandler func(*sessionsettings.SessionSettings)
	startupDone            chan struct{}

	restartProvisionNow     chan struct{}
	restartProvisionNowOnce sync.Once
}

// New creates a new Server.
//
//   - port:         TCP port to listen on (e.g. 9001)
//   - settingsFile: path to /session-settings/settings.yaml; if this file
//     exists at startup the server auto-provisions from it (Pod restart case).
func New(port int, settingsFile string) *Server {
	return NewWithHost("0.0.0.0", port, settingsFile)
}

// NewWithHost creates a Server bound to a specific local address.
func NewWithHost(host string, port int, settingsFile string) *Server {
	if strings.TrimSpace(host) == "" {
		host = "0.0.0.0"
	}
	s := &Server{
		host:         host,
		port:         port,
		settingsFile: settingsFile,
		httpClient:   http.DefaultClient,
		filterURL:    "http://127.0.0.1:3129",
		status:       StatusPending,
		phase:        "starting",
		phaseTime:    time.Now(),
		startupDone:  make(chan struct{}),

		restartProvisionNow: make(chan struct{}),
	}
	if socketPath := strings.TrimSpace(os.Getenv("NETWORK_FILTER_CONTROL_SOCKET")); socketPath != "" {
		s.filterURL = "http://unix"
		s.filterHTTPClient = unixSocketHTTPClient(socketPath)
	}
	return s
}

// NewWithSocket creates a provisioner server that listens only on a Unix socket.
func NewWithSocket(socketPath, settingsFile string) *Server {
	s := NewWithHost("", 0, settingsFile)
	s.socketPath = socketPath
	return s
}

func (s *Server) StartRestartAutoProvisionNow() {
	if s.restartProvisionNow == nil {
		return
	}
	s.restartProvisionNowOnce.Do(func() { close(s.restartProvisionNow) })
}

func (s *Server) waitForRestartAutoProvision(ctx context.Context, delay time.Duration) bool {
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
	case <-s.restartProvisionNow:
	}
	return true
}

// ConfigureCLIBroker supplies the parent endpoint used for credentialed CLI
// requests. The credential itself is learned from the in-memory provision
// settings and is never accepted from the local caller.
func (s *Server) ConfigureCLIBroker(parentURL, sessionID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.brokerURL = strings.TrimRight(parentURL, "/")
	s.brokerSessionID = sessionID
}

func (s *Server) handleCLIBroker(w http.ResponseWriter, r *http.Request) {
	s.mu.RLock()
	parentURL, apiKey, sessionID := s.brokerURL, s.brokerKey, s.brokerSessionID
	s.mu.RUnlock()
	if parentURL == "" || apiKey == "" || sessionID == "" {
		http.Error(w, "CLI broker is not ready", http.StatusServiceUnavailable)
		return
	}
	if !allowedCLIBrokerRequest(r, sessionID) {
		http.Error(w, "CLI broker request is not allowed", http.StatusForbidden)
		return
	}
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 8<<20))
	if err != nil {
		http.Error(w, "request body is too large", http.StatusRequestEntityTooLarge)
		return
	}
	if r.URL.Path == "/notifications/send" {
		body, err = rewriteBrokerNotification(body, sessionID)
		if err != nil {
			http.Error(w, "invalid notification request", http.StatusBadRequest)
			return
		}
	}
	upstream, err := http.NewRequestWithContext(r.Context(), r.Method, parentURL+r.URL.RequestURI(), bytes.NewReader(body))
	if err != nil {
		http.Error(w, "invalid broker request", http.StatusBadRequest)
		return
	}
	upstream.Header.Set("Authorization", "Bearer "+apiKey)
	copySelectedHeaders(upstream.Header, r.Header, "Accept", "Content-Type", "Last-Event-ID")
	resp, err := http.DefaultClient.Do(upstream)
	if err != nil {
		http.Error(w, "CLI broker upstream request failed", http.StatusBadGateway)
		return
	}
	defer func() { closeutils.SafeCloseResponseBody(resp.Body) }()
	copySelectedHeaders(w.Header(), resp.Header, "Content-Type", "Cache-Control")
	w.WriteHeader(resp.StatusCode)
	if _, err := io.Copy(w, resp.Body); err != nil {
		log.Printf("[PROVISIONER] CLI broker response copy failed: %v", err)
	}
}

func rewriteBrokerNotification(body []byte, sessionID string) ([]byte, error) {
	var notification map[string]any
	if err := json.Unmarshal(body, &notification); err != nil {
		return nil, err
	}
	delete(notification, "user_id")
	notification["session_id"] = sessionID
	return json.Marshal(notification)
}

func copySelectedHeaders(destination, source http.Header, names ...string) {
	for _, name := range names {
		if value := source.Get(name); value != "" {
			destination.Set(name, value)
		}
	}
}

func allowedCLIBrokerRequest(r *http.Request, sessionID string) bool {
	path := r.URL.Path
	if path == "/notifications/send" {
		return r.Method == http.MethodPost
	}
	if path == "/internal/usage-events" {
		return r.Method == http.MethodPost
	}
	escapedID := url.PathEscape(sessionID)
	if path == "/sessions/"+escapedID || strings.HasPrefix(path, "/sessions/"+escapedID+"/") {
		return true
	}
	return path == "/"+escapedID+"/message" && r.Method == http.MethodPost
}

var allowedHerdrMethods = map[string]struct{}{
	"terminal.list":      {},
	"terminal.get":       {},
	"terminal.read":      {},
	"terminal.send":      {},
	"terminal.interrupt": {},
}

// handleHerdrRPC exposes the small, user-facing part of herdr-core's JSON-RPC
// API. herdr-core speaks JSON-RPC over stdin/stdout through `hc connect`; the
// provisioner is the only process in a session Pod that can reach that local
// transport. Keep the allowlist deliberately narrower than the full API: the
// web UI may inspect and talk to existing terminals, but cannot start or kill
// arbitrary processes.
func (s *Server) handleHerdrRPC(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if _, err := os.Stat(herdrParkMarkerPath()); err == nil {
		http.Error(w, "herdr-core is parked for session suspend", http.StatusServiceUnavailable)
		return
	}
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 1<<20))
	if err != nil {
		http.Error(w, "invalid JSON-RPC request", http.StatusBadRequest)
		return
	}
	var request struct {
		JSONRPC string          `json:"jsonrpc"`
		ID      json.RawMessage `json:"id"`
		Method  string          `json:"method"`
	}
	if err := json.Unmarshal(body, &request); err != nil || request.JSONRPC != "2.0" || len(request.ID) == 0 {
		http.Error(w, "invalid JSON-RPC request", http.StatusBadRequest)
		return
	}
	if _, ok := allowedHerdrMethods[request.Method]; !ok {
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(map[string]interface{}{
			"jsonrpc": "2.0",
			"id":      request.ID,
			"error": map[string]interface{}{
				"code": -32601, "message": "Method not found",
			},
		}); err != nil {
			log.Printf("[PROVISIONER] Write herdr method error response: %v", err)
		}
		return
	}

	cmd := exec.CommandContext(r.Context(), "hc", "connect")
	cmd.Stdin = bytes.NewReader(append(body, '\n'))
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &limitedWriter{writer: &stdout, remaining: 8 << 20}
	cmd.Stderr = &limitedWriter{writer: &stderr, remaining: 64 << 10}
	if err := cmd.Run(); err != nil {
		log.Printf("[PROVISIONER] herdr-core RPC %s failed: %v: %s", request.Method, err, strings.TrimSpace(stderr.String()))
		http.Error(w, "herdr-core is unavailable", http.StatusBadGateway)
		return
	}
	if !json.Valid(stdout.Bytes()) {
		log.Printf("[PROVISIONER] herdr-core RPC %s returned invalid JSON", request.Method)
		http.Error(w, "invalid response from herdr-core", http.StatusBadGateway)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	if _, err := w.Write(stdout.Bytes()); err != nil {
		log.Printf("[PROVISIONER] Write herdr response: %v", err)
	}
}

type limitedWriter struct {
	writer    io.Writer
	remaining int64
}

func (w *limitedWriter) Write(p []byte) (int, error) {
	if int64(len(p)) > w.remaining {
		return 0, fmt.Errorf("output limit exceeded")
	}
	n, err := w.writer.Write(p)
	w.remaining -= int64(n)
	return n, err
}

// handleOneTimeSecret lets the agent process consume a value without exposing
// the parent runtime bearer token to that process. This server is reachable
// through a Kubernetes Service, so the endpoint is explicitly loopback-only.
func (s *Server) handleOneTimeSecret(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	host, port, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil || port == "" || !net.ParseIP(host).IsLoopback() {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	secretID := strings.TrimPrefix(r.URL.Path, "/one-time-secrets/")
	if secretID == "" || strings.Contains(secretID, "/") {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}

	s.mu.RLock()
	settings := s.activeSettings
	var runtime *sessionsettings.ParentRuntimeConfig
	if settings != nil && settings.ParentRuntime != nil {
		copy := *settings.ParentRuntime
		runtime = &copy
	}
	s.mu.RUnlock()
	if runtime == nil || !runtime.Enabled || runtime.Endpoint == "" || runtime.SessionID == "" || runtime.Token == "" {
		http.Error(w, "parent runtime unavailable", http.StatusServiceUnavailable)
		return
	}

	endpoint := strings.TrimRight(runtime.Endpoint, "/") + "/internal/session-control/" + url.PathEscape(runtime.SessionID) + "/secrets/" + url.PathEscape(secretID)
	query := url.Values{}
	query.Set("generation", fmt.Sprintf("%d", runtime.Generation))
	req, err := http.NewRequestWithContext(r.Context(), http.MethodGet, endpoint+"?"+query.Encode(), nil)
	if err != nil {
		http.Error(w, "failed to create parent request", http.StatusInternalServerError)
		return
	}
	req.Header.Set("Authorization", "Bearer "+runtime.Token)
	resp, err := s.httpClient.Do(req)
	if err != nil {
		log.Printf("[PROVISIONER] one-time secret parent request failed: %v", err)
		http.Error(w, "parent runtime unavailable", http.StatusBadGateway)
		return
	}
	defer func() { closeutils.SafeCloseResponseBody(resp.Body) }()
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(resp.StatusCode)
	if _, err := io.Copy(w, resp.Body); err != nil {
		log.Printf("[PROVISIONER] Copy one-time secret response: %v", err)
	}
}
