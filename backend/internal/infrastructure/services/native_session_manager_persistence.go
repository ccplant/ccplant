package services

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"syscall"
)

func (m *NativeSessionManager) persistSession(s *NativeSession) (returnErr error) {
	s.mu.RLock()
	state := nativeSessionState{ID: s.id, Request: s.request, RootDir: s.rootDir, AgentPort: s.agentPort,
		ProvisionerPort: s.provisionerPort, PID: s.pid, StartedAt: s.startedAt, UpdatedAt: s.updatedAt,
		LastMessageAt: s.lastMessageAt, Status: s.status, StatusMessage: s.statusMessage, Description: s.description,
		FilesystemSandbox: m.filesystemSandbox}
	s.mu.RUnlock()
	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal native session state: %w", err)
	}
	path := filepath.Join(s.rootDir, "runtime", "state.json")
	temporary, err := os.CreateTemp(filepath.Dir(path), ".state-*.tmp")
	if err != nil {
		return err
	}
	temporaryPath := temporary.Name()
	defer func() {
		if err := os.Remove(temporaryPath); err != nil && !errors.Is(err, os.ErrNotExist) {
			returnErr = errors.Join(returnErr, fmt.Errorf("remove temporary native session state: %w", err))
		}
	}()
	if err := temporary.Chmod(0o600); err != nil {
		return errors.Join(err, temporary.Close())
	}
	written, err := temporary.Write(data)
	if err != nil || written != len(data) {
		if err == nil {
			err = io.ErrShortWrite
		}
		return errors.Join(err, temporary.Close())
	}
	if err := temporary.Sync(); err != nil {
		return errors.Join(err, temporary.Close())
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	return os.Rename(temporaryPath, path)
}

func (m *NativeSessionManager) restoreSessions() error {
	entries, err := os.ReadDir(filepath.Join(m.stateDir, "sessions"))
	if err != nil {
		return err
	}
	var restoreErr error
	for _, entry := range entries {
		if entry.IsDir() {
			restoreErr = errors.Join(restoreErr, m.restoreSession(entry.Name()))
		}
	}
	return restoreErr
}

func (m *NativeSessionManager) restoreSession(name string) error {
	sessionDir := filepath.Join(m.stateDir, "sessions", name)
	data, err := os.ReadFile(filepath.Join(sessionDir, "runtime", "state.json"))
	if err != nil {
		return os.RemoveAll(sessionDir)
	}
	var state nativeSessionState
	if err := json.Unmarshal(data, &state); err != nil || state.ID == "" || state.PID <= 0 {
		return os.RemoveAll(sessionDir)
	}
	if filepath.Clean(state.RootDir) != filepath.Clean(sessionDir) || state.FilesystemSandbox != m.filesystemSandbox {
		return nil
	}
	if state.Status == "terminating" || state.Status == "stopped" {
		return cleanupRestoredNativeSession(state, sessionDir)
	}
	if err := syscall.Kill(state.PID, 0); err != nil || !nativeProcessMatchesSession(state.PID, state.RootDir) {
		return os.RemoveAll(sessionDir)
	}
	if state.Status == "running" && !nativeAgentHealthy(state.AgentPort) {
		return nil
	}
	m.sessions[state.ID] = restoredNativeSession(state)
	return nil
}

func cleanupRestoredNativeSession(state nativeSessionState, sessionDir string) error {
	if syscall.Kill(state.PID, 0) == nil && nativeProcessMatchesSession(state.PID, state.RootDir) {
		return terminateNativeProcessGroup(state.PID, sessionDir)
	}
	return os.RemoveAll(sessionDir)
}

func restoredNativeSession(state nativeSessionState) *NativeSession {
	return &NativeSession{id: state.ID, request: state.Request, rootDir: state.RootDir,
		agentPort: state.AgentPort, provisionerPort: state.ProvisionerPort, pid: state.PID,
		startedAt: state.StartedAt, updatedAt: state.UpdatedAt, lastMessageAt: state.LastMessageAt,
		status: state.Status, statusMessage: state.StatusMessage, description: state.Description, cancel: func() {}}
}
