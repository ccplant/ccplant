package services

import (
	"context"
	"crypto/subtle"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/takutakahashi/agentapi-proxy/internal/domain/entities"
)

// NativeSessionManager runs one agent-provisioner process per session on the host.
// Provision settings are kept in memory and pulled by the unmodified provisioner
// through the existing internal provisioner API.
type NativeSessionManager struct {
	mu                sync.RWMutex
	sessions          map[string]*NativeSession
	provisionRequests map[string]*ProvisionRequest
	stateDir          string
	proxyURL          string
	provisionerToken  string
	upstreamAuthToken string
	binaryPath        string
	filesystemSandbox bool
	httpClient        *http.Client
}

type NativeSession struct {
	mu              sync.RWMutex
	id              string
	request         *entities.RunServerRequest
	rootDir         string
	agentPort       int
	provisionerPort int
	cmd             *exec.Cmd
	pid             int
	startedAt       time.Time
	updatedAt       time.Time
	lastMessageAt   time.Time
	status          string
	statusMessage   string
	description     string
	cancel          context.CancelFunc
	deleteDone      chan struct{}
	deleteErr       error
}

type nativeSessionState struct {
	ID                string                     `json:"id"`
	Request           *entities.RunServerRequest `json:"request"`
	RootDir           string                     `json:"root_dir"`
	AgentPort         int                        `json:"agent_port"`
	ProvisionerPort   int                        `json:"provisioner_port"`
	PID               int                        `json:"pid"`
	StartedAt         time.Time                  `json:"started_at"`
	UpdatedAt         time.Time                  `json:"updated_at"`
	LastMessageAt     time.Time                  `json:"last_message_at"`
	Status            string                     `json:"status"`
	StatusMessage     string                     `json:"status_message,omitempty"`
	Description       string                     `json:"description,omitempty"`
	FilesystemSandbox bool                       `json:"filesystem_sandbox,omitempty"`
}

func NewNativeSessionManager(stateDir, proxyURL, provisionerToken, upstreamAuthToken, binaryPath string, filesystemSandbox bool) (*NativeSessionManager, error) {
	if stateDir == "" {
		return nil, errors.New("native state directory is required")
	}
	if proxyURL == "" {
		return nil, errors.New("native provisioner proxy URL is required")
	}
	if provisionerToken == "" {
		return nil, errors.New("native provisioner token is required")
	}
	if binaryPath == "" {
		var err error
		binaryPath, err = os.Executable()
		if err != nil {
			return nil, fmt.Errorf("resolve executable: %w", err)
		}
	}
	if filesystemSandbox && runtime.GOOS != "darwin" {
		return nil, fmt.Errorf("native filesystem sandbox is only supported on macOS")
	}
	if filesystemSandbox {
		if _, err := os.Stat(nativeSandboxExecPath); err != nil {
			return nil, fmt.Errorf("native filesystem sandbox requires %s: %w", nativeSandboxExecPath, err)
		}
	}
	if err := os.MkdirAll(filepath.Join(stateDir, "sessions"), 0o700); err != nil {
		return nil, fmt.Errorf("create native state directory: %w", err)
	}
	m := &NativeSessionManager{
		sessions:          make(map[string]*NativeSession),
		provisionRequests: make(map[string]*ProvisionRequest),
		stateDir:          stateDir,
		proxyURL:          strings.TrimRight(proxyURL, "/"),
		provisionerToken:  provisionerToken,
		upstreamAuthToken: upstreamAuthToken,
		binaryPath:        binaryPath,
		filesystemSandbox: filesystemSandbox,
		httpClient:        &http.Client{Timeout: 30 * time.Second},
	}
	if err := m.restoreSessions(); err != nil {
		return nil, err
	}
	return m, nil
}

func (m *NativeSessionManager) CreateSession(ctx context.Context, id string, req *entities.RunServerRequest, webhookPayload []byte) (entities.Session, error) {
	return m.CreateSessionDirect(ctx, id, req, webhookPayload)
}

func (m *NativeSessionManager) CreateSessionDirect(_ context.Context, id string, req *entities.RunServerRequest, _ []byte) (returnSession entities.Session, returnErr error) {
	if req == nil {
		return nil, errors.New("native session requires allocation metadata")
	}
	if !validNativeSessionID(id) {
		return nil, fmt.Errorf("invalid native session ID %q", id)
	}
	m.mu.Lock()
	if _, exists := m.sessions[id]; exists {
		m.mu.Unlock()
		return nil, fmt.Errorf("session %s already exists", id)
	}
	m.mu.Unlock()

	paths, err := prepareNativeSessionDirectories(m.stateDir, id)
	if err != nil {
		return nil, err
	}
	created := false
	defer func() {
		if !created {
			returnErr = errors.Join(returnErr, paths.cleanup())
		}
	}()
	ports, err := reserveNativeSessionPorts()
	if err != nil {
		return nil, err
	}
	session, err := m.launchNativeSession(id, req, ports, paths)
	if err != nil {
		return nil, err
	}
	created = true
	return session, nil
}

func validNativeSessionID(id string) bool {
	return id != "" && id != "." && id != ".." && filepath.Base(id) == id
}

func nativeHerdrRuntimeDir(id string) string {
	return filepath.Join(os.TempDir(), "ccplant-hc-"+id)
}

func nativeProvisionerEnvironment(base []string, values ...string) []string {
	overrides := make(map[string]struct{}, len(values)+3)
	for _, value := range values {
		if key, ok := nativeEnvironmentKey(value); ok {
			overrides[key] = struct{}{}
		}
	}
	for _, key := range []string{"AGENTAPI_WORKDIR", "AGENTAPI_REPO_DIR", "CCPLANT_BINARY_PATH"} {
		overrides[key] = struct{}{}
	}
	env := make([]string, 0, len(base)+len(values))
	for _, value := range base {
		key, ok := nativeEnvironmentKey(value)
		if ok {
			if _, overridden := overrides[key]; overridden {
				continue
			}
		}
		env = append(env, value)
	}
	return append(env, values...)
}

func nativeEnvironmentKey(value string) (string, bool) {
	separator := strings.IndexByte(value, '=')
	if separator < 0 {
		return "", false
	}
	return value[:separator], true
}

func reserveTCPPort() (int, error) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return 0, fmt.Errorf("reserve TCP port: %w", err)
	}
	port := ln.Addr().(*net.TCPAddr).Port
	if err := ln.Close(); err != nil {
		return 0, fmt.Errorf("close reserved TCP port: %w", err)
	}
	return port, nil
}

func (m *NativeSessionManager) ValidateProvisionerToken(token string) bool {
	return token != "" && len(token) == len(m.provisionerToken) && subtle.ConstantTimeCompare([]byte(token), []byte(m.provisionerToken)) == 1
}

func (m *NativeSessionManager) ValidateSessionControlToken(sessionID, token string) bool {
	return validateSessionControlToken(m.provisionerToken, sessionID, token)
}

func (m *NativeSessionManager) UsesRemoteProvisioner() bool { return true }

func (m *NativeSessionManager) ConnectProvisioner(_ context.Context, req ProvisionerConnectRequest) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	p := m.provisionRequests[req.SessionID]
	if p == nil {
		return fmt.Errorf("provision request for session %s not found", req.SessionID)
	}
	p.ClaimedBy = req.PodName
	p.UpdatedAt = time.Now().UTC()
	return nil
}

func (m *NativeSessionManager) ClaimProvisionRequest(_ context.Context, sessionID, podName string) (*ProvisionRequest, bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	p := m.provisionRequests[sessionID]
	if p == nil {
		return nil, false, nil
	}
	if p.Status == "ready" || p.Status == "claimed" || p.Status == "provisioning" {
		return nil, false, nil
	}
	p.Status = "claimed"
	p.ClaimedBy = podName
	p.UpdatedAt = time.Now().UTC()
	copyReq := *p
	return &copyReq, true, nil
}

func (m *NativeSessionManager) UpdateProvisionRequestStatus(_ context.Context, sessionID, requestID string, update ProvisionRequestStatusUpdate) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	p := m.provisionRequests[sessionID]
	if p == nil {
		return fmt.Errorf("provision request for session %s not found", sessionID)
	}
	if p.RequestID != requestID {
		return fmt.Errorf("provision request id mismatch")
	}
	p.Status, p.Message, p.UpdatedAt = update.Status, update.Message, time.Now().UTC()
	if update.PodName != "" {
		p.ClaimedBy = update.PodName
	}
	if s := m.sessions[sessionID]; s != nil {
		s.mu.Lock()
		s.status = update.Status
		s.statusMessage = update.Message
		if update.Status == "ready" {
			s.status = "running"
			s.statusMessage = ""
		}
		s.updatedAt = time.Now().UTC()
		s.mu.Unlock()
		if err := m.persistSession(s); err != nil {
			return err
		}
	}
	return nil
}
