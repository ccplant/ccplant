package services

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/takutakahashi/agentapi-proxy/internal/domain/entities"
	portrepos "github.com/takutakahashi/agentapi-proxy/internal/usecases/ports/repositories"
	closeutils "github.com/takutakahashi/agentapi-proxy/pkg/utils"
)

func (m *NativeSessionManager) GetSession(id string) entities.Session {
	m.mu.RLock()
	defer m.mu.RUnlock()
	session, ok := m.sessions[id]
	if !ok || session == nil {
		return nil
	}
	return session
}

func (m *NativeSessionManager) ListSessions(filter entities.SessionFilter) []entities.Session {
	m.mu.RLock()
	defer m.mu.RUnlock()
	result := make([]entities.Session, 0, len(m.sessions))
	for _, s := range m.sessions {
		if filter.UserID != "" && s.UserID() != filter.UserID {
			continue
		}
		if filter.Scope != "" && s.Scope() != filter.Scope {
			continue
		}
		if filter.TeamID != "" && s.TeamID() != filter.TeamID {
			continue
		}
		if filter.Status != "" && s.Status() != filter.Status {
			continue
		}
		result = append(result, s)
	}
	return result
}

// ActiveSessionCount returns the number of native workloads that have not
// reached a terminal state. Finished sessions may remain available for
// inspection until they are explicitly deleted, but must not be reported as
// active in manager heartbeats.
func (m *NativeSessionManager) ActiveSessionCount() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	count := 0
	for _, s := range m.sessions {
		if s != nil && nativeSessionStatusActive(s.Status()) {
			count++
		}
	}
	return count
}

func nativeSessionStatusActive(status string) bool {
	switch strings.ToLower(status) {
	case "error", "failed", "exited", "stopped", "terminated":
		return false
	default:
		return true
	}
}

func (m *NativeSessionManager) DeleteSession(id string) error {
	m.mu.Lock()
	s := m.sessions[id]
	if s == nil {
		m.mu.Unlock()
		return nil
	}
	rel, pathErr := filepath.Rel(filepath.Join(m.stateDir, "sessions"), s.rootDir)
	if pathErr != nil || rel == "." || rel == "" || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		m.mu.Unlock()
		return fmt.Errorf("unsafe session state path for %s", id)
	}
	s.mu.Lock()
	if s.deleteDone != nil {
		done := s.deleteDone
		s.mu.Unlock()
		m.mu.Unlock()
		<-done
		s.mu.RLock()
		err := s.deleteErr
		s.mu.RUnlock()
		return err
	}
	s.deleteDone, s.deleteErr = make(chan struct{}), nil
	done := s.deleteDone
	s.status = "terminating"
	s.updatedAt = time.Now().UTC()
	pid := s.pid
	cancel := s.cancel
	s.mu.Unlock()
	delete(m.provisionRequests, id)
	m.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	err := terminateNativeProcessGroup(pid, s.rootDir)
	if err == nil {
		m.mu.Lock()
		if m.sessions[id] == s {
			delete(m.sessions, id)
		}
		m.mu.Unlock()
	}
	s.mu.Lock()
	s.deleteErr = err
	if err != nil {
		s.status = "stopped"
		s.statusMessage = "session cleanup failed: " + err.Error()
		s.updatedAt = time.Now().UTC()
	}
	close(done)
	if err != nil {
		s.deleteDone = nil
	}
	s.mu.Unlock()
	if err != nil {
		err = errors.Join(err, m.persistSession(s))
	}
	return err
}

func terminateNativeProcessGroup(pid int, root string) error {
	if strings.TrimSpace(root) == "" || filepath.Clean(root) == string(filepath.Separator) {
		return errors.New("unsafe session state path")
	}
	if pid > 0 {
		if err := stopNativeProcessGroup(pid); err != nil {
			return err
		}
	}
	return cleanupNativeSessionDirs(root)
}

func stopNativeProcessGroup(pid int) error {
	if err := syscall.Kill(-pid, syscall.SIGTERM); err != nil && !errors.Is(err, syscall.ESRCH) {
		return fmt.Errorf("terminate process group: %w", err)
	}
	if waitForNativeProcessGroupExit(pid, 10*time.Second, 100*time.Millisecond) {
		return nil
	}
	if err := syscall.Kill(-pid, syscall.SIGKILL); err != nil && !errors.Is(err, syscall.ESRCH) {
		return fmt.Errorf("kill process group: %w", err)
	}
	if !waitForNativeProcessGroupExit(pid, 2*time.Second, 25*time.Millisecond) {
		return errors.New("process group still exists after SIGKILL")
	}
	return nil
}

func waitForNativeProcessGroupExit(pid int, timeout, interval time.Duration) bool {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if syscall.Kill(-pid, 0) != nil {
			return true
		}
		time.Sleep(interval)
	}
	return syscall.Kill(-pid, 0) != nil
}

func cleanupNativeSessionDirs(root string) error {
	stateErr := os.RemoveAll(root)
	herdrErr := os.RemoveAll(nativeHerdrRuntimeDir(filepath.Base(root)))
	if stateErr != nil {
		stateErr = fmt.Errorf("remove session state: %w", stateErr)
	}
	if herdrErr != nil {
		herdrErr = fmt.Errorf("remove session herdr runtime: %w", herdrErr)
	}
	return errors.Join(stateErr, herdrErr)
}

func (m *NativeSessionManager) SendMessage(ctx context.Context, id, message string) error {
	s := m.nativeSession(id)
	if s == nil {
		return fmt.Errorf("session %s not found", id)
	}
	path := "/message"
	payload, err := json.Marshal(map[string]string{"content": message, "type": "user"})
	if err != nil {
		return fmt.Errorf("encode native message: %w", err)
	}
	if s.request != nil && strings.HasSuffix(s.request.AgentType, "-acp") {
		acpSessionID, err := m.getACPSessionID(ctx, s)
		if err != nil {
			return err
		}
		path = "/rpc"
		payload, err = json.Marshal(map[string]interface{}{
			"jsonrpc": "2.0",
			"id":      time.Now().UnixNano(),
			"method":  "session/prompt",
			"params": map[string]interface{}{
				"sessionId": acpSessionID,
				"prompt":    []map[string]string{{"type": "text", "text": message}},
			},
		})
		if err != nil {
			return fmt.Errorf("encode native ACP message: %w", err)
		}
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "http://"+s.Addr()+path, bytes.NewReader(payload))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := m.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer func() { closeutils.SafeCloseResponseBody(resp.Body) }()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, readErr := io.ReadAll(resp.Body)
		if readErr != nil {
			return fmt.Errorf("read message error response: %w", readErr)
		}
		return fmt.Errorf("message returned %d: %s", resp.StatusCode, body)
	}
	s.mu.Lock()
	s.lastMessageAt, s.updatedAt = time.Now().UTC(), time.Now().UTC()
	s.mu.Unlock()
	return m.persistSession(s)
}

func (m *NativeSessionManager) getACPSessionID(ctx context.Context, s *NativeSession) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://"+s.Addr()+"/session", nil)
	if err != nil {
		return "", err
	}
	resp, err := m.httpClient.Do(req)
	if err != nil {
		return "", err
	}
	defer func() { closeutils.SafeCloseResponseBody(resp.Body) }()
	if resp.StatusCode >= 300 {
		return "", fmt.Errorf("ACP session returned HTTP %d", resp.StatusCode)
	}
	var result struct {
		SessionID string `json:"sessionId"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", err
	}
	if result.SessionID == "" {
		return "", errors.New("ACP session ID is empty")
	}
	return result.SessionID, nil
}

func (m *NativeSessionManager) StopAgent(ctx context.Context, id string) error {
	s := m.nativeSession(id)
	if s == nil {
		return fmt.Errorf("session %s not found", id)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "http://"+s.Addr()+"/stop", nil)
	if err != nil {
		return err
	}
	resp, err := m.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer func() { closeutils.SafeCloseResponseBody(resp.Body) }()
	if resp.StatusCode >= 300 {
		return fmt.Errorf("stop returned HTTP %d", resp.StatusCode)
	}
	return nil
}

func (m *NativeSessionManager) GetMessages(ctx context.Context, id string) ([]portrepos.Message, error) {
	s := m.nativeSession(id)
	if s == nil {
		return nil, fmt.Errorf("session %s not found", id)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://"+s.Addr()+"/messages", nil)
	if err != nil {
		return nil, err
	}
	resp, err := m.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { closeutils.SafeCloseResponseBody(resp.Body) }()
	if resp.StatusCode >= 300 {
		return nil, fmt.Errorf("messages returned HTTP %d", resp.StatusCode)
	}
	var result struct {
		Messages []portrepos.Message `json:"messages"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, err
	}
	return result.Messages, nil
}

func (m *NativeSessionManager) Shutdown(_ time.Duration) error {
	m.mu.RLock()
	ids := make([]string, 0, len(m.sessions))
	for id := range m.sessions {
		ids = append(ids, id)
	}
	m.mu.RUnlock()
	var shutdownErr error
	for _, id := range ids {
		shutdownErr = errors.Join(shutdownErr, m.DeleteSession(id))
	}
	return shutdownErr
}

func (m *NativeSessionManager) nativeSession(id string) *NativeSession {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.sessions[id]
}
