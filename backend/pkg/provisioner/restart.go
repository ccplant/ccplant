package provisioner

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"fmt"
	"github.com/takutakahashi/agentapi-proxy/pkg/sessionsettings"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

// Only generated configuration is removed. Conversations, workspace and arbitrary
// user files stay intact. New complete settings are compiled immediately after this.
func cleanRestartFiles(settings *sessionsettings.SessionSettings, home string) error {
	for _, rel := range []string{".codex/hooks.json", ".codex/config.toml", ".claude/settings.json"} {
		if err := os.Remove(filepath.Join(home, rel)); err != nil && !os.IsNotExist(err) {
			return err
		}
	}
	claudePath := filepath.Join(home, ".claude.json")
	if raw, err := os.ReadFile(claudePath); err == nil {
		var config map[string]interface{}
		if err := json.Unmarshal(raw, &config); err != nil {
			return err
		}
		delete(config, "mcpServers")
		raw, err = json.Marshal(config)
		if err != nil {
			return err
		}
		if err := os.WriteFile(claudePath, raw, 0600); err != nil {
			return err
		}
	} else if !os.IsNotExist(err) {
		return err
	}
	active := map[string]bool{}
	for _, f := range settings.Files {
		active[f.Path] = true
	}
	for _, path := range sessionsettings.ManagedFileTypes {
		if !active[path] {
			if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
				return err
			}
		}
	}
	// A persisted cycle marker would automatically issue a new prompt after restart.
	for _, path := range []string{"/tmp/check/CYCLE_ENABLED"} {
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			return err
		}
	}
	files := settings.Files[:0]
	for _, f := range settings.Files {
		if f.Path != "/tmp/check/CYCLE_ENABLED" {
			files = append(files, f)
		}
	}
	settings.Files = files
	return nil
}

// The provisioner stays alive while its agent and companion processes stop.
// Lifecycle commands are authenticated with the manager's startup credential.
func (s *Server) authorizeLifecycle(w http.ResponseWriter, r *http.Request) bool {
	expected := os.Getenv("PROVISIONER_TOKEN")
	actual := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
	if expected == "" || subtle.ConstantTimeCompare([]byte(actual), []byte(expected)) != 1 {
		http.Error(w, "unauthorized", 401)
		return false
	}
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", 405)
		return false
	}
	return true
}
func (s *Server) pauseAgent(ctx context.Context) error {
	s.mu.RLock()
	cancel, exited, provisionDone := s.agentCancel, s.agentExited, s.provisionDone
	s.mu.RUnlock()
	if cancel == nil {
		return fmt.Errorf("no agent lifecycle exists")
	}
	cancel()
	if provisionDone != nil {
		select {
		case <-provisionDone:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	if exited != nil {
		select {
		case <-exited:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	workersDone := make(chan struct{})
	go func() { s.agentWorkers.Wait(); close(workersDone) }()
	select {
	case <-workersDone:
	case <-ctx.Done():
		return ctx.Err()
	}
	s.setStatus(Status("paused"), "")
	return nil
}
func (s *Server) handlePauseAgent(w http.ResponseWriter, r *http.Request) {
	if !s.authorizeLifecycle(w, r) {
		return
	}
	s.lifecycleMu.Lock()
	defer s.lifecycleMu.Unlock()
	if err := s.pauseAgent(r.Context()); err != nil {
		http.Error(w, "agent could not be stopped", 503)
		return
	}
	w.WriteHeader(204)
}
func (s *Server) handleRestartAgent(w http.ResponseWriter, r *http.Request) {
	if !s.authorizeLifecycle(w, r) {
		return
	}
	s.lifecycleMu.Lock()
	defer s.lifecycleMu.Unlock()
	var settings sessionsettings.SessionSettings
	if err := json.NewDecoder(io.LimitReader(r.Body, 16<<20)).Decode(&settings); err != nil || settings.RestartID == "" {
		http.Error(w, "invalid settings", 400)
		return
	}
	s.mu.RLock()
	old, id, status := s.activeSettings, s.restartID, s.status
	s.mu.RUnlock()
	if id == settings.RestartID && (status == StatusReady || status == StatusProvisioning) {
		w.WriteHeader(202)
		return
	}
	if err := sessionsettings.ValidateRestart(old, &settings); err != nil {
		http.Error(w, err.Error(), 422)
		return
	}
	if old.Session.ID != settings.Session.ID {
		http.Error(w, "session identity changed", 422)
		return
	}
	if err := s.pauseAgent(r.Context()); err != nil {
		http.Error(w, "agent could not be stopped", 503)
		return
	}
	settings.Restart = true
	settings.RestartInPlace = !old.Paused
	settings.Paused = false
	s.setStatus(StatusProvisioning, "")
	s.mu.Lock()
	s.restartID = settings.RestartID
	s.mu.Unlock()
	parent := s.serverCtx
	if parent == nil {
		parent = context.Background()
	}
	s.runProvision(parent, &settings)
	if s.GetStatus() != StatusReady {
		http.Error(w, "agent restart failed", 503)
		return
	}
	w.WriteHeader(202)
}
