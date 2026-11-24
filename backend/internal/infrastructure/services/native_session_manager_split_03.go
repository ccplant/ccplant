package services

import (
	"bytes"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"time"

	"github.com/takutakahashi/agentapi-proxy/internal/domain/entities"
	closeutils "github.com/takutakahashi/agentapi-proxy/pkg/utils"
)

func nativeProcessMatchesSession(pid int, root string) bool {
	needle := "AGENTAPI_NATIVE_SESSION_ROOT=" + root
	if runtime.GOOS == "linux" {
		data, err := os.ReadFile(filepath.Join("/proc", strconv.Itoa(pid), "environ"))
		return err == nil && bytes.Contains(data, []byte(needle))
	}
	if runtime.GOOS == "darwin" {
		data, err := exec.Command("ps", "eww", "-p", strconv.Itoa(pid), "-o", "command=").Output()
		return err == nil && bytes.Contains(data, []byte(needle))
	}
	return false
}

func nativeAgentHealthy(port int) bool {
	client := &http.Client{Timeout: 2 * time.Second}
	resp, err := client.Get("http://" + net.JoinHostPort("127.0.0.1", strconv.Itoa(port)) + "/status")
	if err != nil {
		return false
	}
	defer func() { closeutils.SafeCloseResponseBody(resp.Body) }()
	return resp.StatusCode >= 200 && resp.StatusCode < 300
}

func (s *NativeSession) ID() string { return s.id }
func (s *NativeSession) Addr() string {
	return net.JoinHostPort("127.0.0.1", strconv.Itoa(s.agentPort))
}
func (s *NativeSession) UserID() string { return s.request.UserID }
func (s *NativeSession) Scope() entities.ResourceScope {
	if s.request.Scope == "" {
		return entities.ScopeUser
	}
	return s.request.Scope
}
func (s *NativeSession) TeamID() string          { return s.request.TeamID }
func (s *NativeSession) Tags() map[string]string { return s.request.Tags }
func (s *NativeSession) Status() string          { s.mu.RLock(); defer s.mu.RUnlock(); return s.status }
func (s *NativeSession) StartedAt() time.Time    { return s.startedAt }
func (s *NativeSession) UpdatedAt() time.Time    { s.mu.RLock(); defer s.mu.RUnlock(); return s.updatedAt }
func (s *NativeSession) LastMessageAt() time.Time {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.lastMessageAt
}
func (s *NativeSession) Description() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.description
}
func (s *NativeSession) StatusMessage() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.statusMessage
}
func (s *NativeSession) Cancel() {
	if s.cancel != nil {
		s.cancel()
	}
}
