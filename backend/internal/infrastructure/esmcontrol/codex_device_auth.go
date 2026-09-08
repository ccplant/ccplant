package esmcontrol

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"sort"
	"sync"
	"time"

	sessionrunnercore "github.com/takutakahashi/agentapi-proxy/internal/core/sessionrunner"
	"github.com/takutakahashi/agentapi-proxy/pkg/codexauth"
)

const (
	// codexDeviceAuthStartTimeout bounds the tunnel round trip for launching an
	// authentication workload. Pod creation on the manager is normally fast, but
	// the tunnel waits for the manager's control poller to pick up the command.
	codexDeviceAuthStartTimeout = 30 * time.Second
	codexDeviceAuthCancelBudget = 15 * time.Second
)

// ControlTunnel is the subset of Tunnel used to reach connected external
// session managers.
type ControlTunnel interface {
	IsConnected(ctx context.Context, managerID string) bool
	Do(ctx context.Context, managerID, sessionID, remoteSessionID string, req *http.Request) (*http.Response, error)
}

// ManagerDirectory lists registered external session managers.
// sessionrunnercore.Store satisfies it.
type ManagerDirectory interface {
	ListManagers(context.Context) ([]*sessionrunnercore.Manager, error)
}

// CodexDeviceAuthLauncher delegates Codex device authentication attempts to a
// connected external session manager. The manager — not the parent API —
// creates the short-lived authentication Pod, so a control plane without
// Kubernetes access (for example the Fly.io API) still executes the workload
// on the real execution plane.
type CodexDeviceAuthLauncher struct {
	tunnel   ControlTunnel
	managers ManagerDirectory
	// active remembers which manager owns each running attempt so cancel is
	// routed to the right execution plane. Lost entries (parent restart) fall
	// back to a best-effort broadcast.
	active sync.Map
}

func NewCodexDeviceAuthLauncher(tunnel ControlTunnel, managers ManagerDirectory) *CodexDeviceAuthLauncher {
	return &CodexDeviceAuthLauncher{tunnel: tunnel, managers: managers}
}

var _ codexauth.WorkloadLauncher = (*CodexDeviceAuthLauncher)(nil)

// Available reports whether at least one enrolled manager is connected right
// now. It backs GET /codex/device-auth/config so the frontend can tell "no
// execution plane" apart from "feature disabled".
func (l *CodexDeviceAuthLauncher) Available(ctx context.Context) bool {
	managers, err := l.candidates(ctx)
	if err != nil {
		return false
	}
	for _, manager := range managers {
		if l.tunnel.IsConnected(ctx, manager.ID) {
			return true
		}
	}
	return false
}

func (l *CodexDeviceAuthLauncher) StartCodexDeviceAuth(ctx context.Context, request codexauth.WorkloadRequest) error {
	if request.AttemptID == "" || request.CallbackURL == "" || request.Token == "" || request.ExpiresAt.IsZero() {
		return errors.New("attempt_id, callback_url, token, and expires_at are required")
	}
	if _, err := l.startOnManager(ctx, request); err != nil {
		return err
	}
	return nil
}

// startOnManager walks the connected managers and returns the manager that
// accepted the workload.
func (l *CodexDeviceAuthLauncher) startOnManager(ctx context.Context, request codexauth.WorkloadRequest) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, codexDeviceAuthStartTimeout)
	defer cancel()
	managers, err := l.candidates(ctx)
	if err != nil {
		return "", err
	}
	lastErr := errors.New("no connected session manager is available for codex device auth")
	for _, manager := range managers {
		if !l.tunnel.IsConnected(ctx, manager.ID) {
			continue
		}
		resp, doErr := l.postWorkload(ctx, manager.ID, request)
		if doErr != nil {
			lastErr = fmt.Errorf("manager %s: %w", manager.ID, doErr)
			continue
		}
		status := resp.StatusCode
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4<<10))
		_ = resp.Body.Close()
		switch {
		case status == http.StatusAccepted:
			l.active.Store(request.AttemptID, manager.ID)
			return manager.ID, nil
		case status == http.StatusNotFound || status == http.StatusNotImplemented:
			// The manager cannot run auth workloads (unknown route on an older
			// revision, or an execution plane without the capability). Try the
			// next connected manager instead of failing the attempt.
			lastErr = fmt.Errorf("manager %s does not support codex device auth workloads", manager.ID)
		default:
			lastErr = fmt.Errorf("manager %s returned HTTP %d", manager.ID, status)
		}
	}
	return "", lastErr
}

func (l *CodexDeviceAuthLauncher) CancelCodexDeviceAuth(ctx context.Context, attemptID string) error {
	if attemptID == "" {
		return errors.New("attempt id is required")
	}
	ctx, cancel := context.WithTimeout(ctx, codexDeviceAuthCancelBudget)
	defer cancel()
	if owner, ok := l.active.Load(attemptID); ok {
		l.active.Delete(attemptID)
		managerID, _ := owner.(string)
		return l.deleteWorkload(ctx, managerID, attemptID)
	}
	// Unknown attempt (for example after a parent restart). Broadcast the
	// cancel to every connected manager; deletion is idempotent there.
	managers, err := l.candidates(ctx)
	if err != nil {
		return err
	}
	var lastErr error
	for _, manager := range managers {
		if !l.tunnel.IsConnected(ctx, manager.ID) {
			continue
		}
		if err := l.deleteWorkload(ctx, manager.ID, attemptID); err != nil && lastErr == nil {
			lastErr = err
		}
	}
	return lastErr
}

// candidates returns launch-capable managers in preference order: capability
// advertisement first, then the default manager, then the most recently seen.
func (l *CodexDeviceAuthLauncher) candidates(ctx context.Context) ([]*sessionrunnercore.Manager, error) {
	if l.managers == nil {
		return nil, errors.New("session manager directory is unavailable")
	}
	managers, err := l.managers.ListManagers(ctx)
	if err != nil {
		return nil, fmt.Errorf("list session managers: %w", err)
	}
	result := make([]*sessionrunnercore.Manager, 0, len(managers))
	for _, manager := range managers {
		if manager == nil || !manager.Enabled || manager.Draining {
			continue
		}
		result = append(result, manager)
	}
	sort.SliceStable(result, func(i, j int) bool {
		a, b := result[i], result[j]
		if ca, cb := hasCapability(a, sessionrunnercore.CapabilityCodexDeviceAuthV1), hasCapability(b, sessionrunnercore.CapabilityCodexDeviceAuthV1); ca != cb {
			return ca
		}
		if a.Default != b.Default {
			return a.Default
		}
		if !a.LastHeartbeatAt.Equal(b.LastHeartbeatAt) {
			return a.LastHeartbeatAt.After(b.LastHeartbeatAt)
		}
		return a.ID < b.ID
	})
	return result, nil
}

func hasCapability(manager *sessionrunnercore.Manager, capability string) bool {
	for _, item := range manager.Capabilities {
		if item == capability {
			return true
		}
	}
	return false
}

func (l *CodexDeviceAuthLauncher) postWorkload(ctx context.Context, managerID string, request codexauth.WorkloadRequest) (*http.Response, error) {
	body, err := json.Marshal(request)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "http://esm.local/api/v1/codex-device-auth", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	return l.tunnel.Do(ctx, managerID, request.AttemptID, "", req)
}

func (l *CodexDeviceAuthLauncher) deleteWorkload(ctx context.Context, managerID, attemptID string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, "http://esm.local/api/v1/codex-device-auth/"+attemptID, nil)
	if err != nil {
		return err
	}
	resp, err := l.tunnel.Do(ctx, managerID, attemptID, "", req)
	if err != nil {
		return fmt.Errorf("manager %s: %w", managerID, err)
	}
	defer func() { _ = resp.Body.Close() }()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4<<10))
	switch resp.StatusCode {
	case http.StatusOK, http.StatusNoContent, http.StatusAccepted, http.StatusNotFound, http.StatusNotImplemented:
		return nil
	default:
		return fmt.Errorf("manager %s returned HTTP %d", managerID, resp.StatusCode)
	}
}