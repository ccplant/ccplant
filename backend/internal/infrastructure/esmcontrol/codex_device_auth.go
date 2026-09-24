package esmcontrol

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"sort"
	"strings"
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

// PoolDirectory is the read-only inventory needed by the shared pool resolver
// and by the final pool-to-manager routing step.
type PoolDirectory interface {
	sessionrunnercore.ResolverStore
}

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
	routes   authorizedRouteResolver
	managers ManagerDirectory
	local    codexauth.WorkloadLauncher
	// active remembers which manager owns each running attempt so cancel is
	// routed to the right execution plane. Lost entries (parent restart) fall
	// back to a best-effort broadcast.
	active sync.Map
}

func NewCodexDeviceAuthLauncher(tunnel ControlTunnel, directory PoolDirectory, local ...codexauth.WorkloadLauncher) *CodexDeviceAuthLauncher {
	launcher := &CodexDeviceAuthLauncher{tunnel: tunnel, managers: directory}
	if len(local) > 0 {
		launcher.local = local[0]
	}
	if directory == nil {
		return launcher
	}
	resolver := sessionrunnercore.NewResolver(directory, 0).WithLocalFallback(launcher.local != nil)
	if tunnel != nil {
		resolver.WithManagerLiveness(tunnelLiveness{tunnel: tunnel})
	}
	launcher.routes = resolver
	return launcher
}

// authorizedRouteResolver deliberately exposes only complete, authorized
// routes. StartCodexDeviceAuth cannot obtain raw managers through this field.
type authorizedRouteResolver interface {
	ResolveRoute(context.Context, sessionrunnercore.Subject, string, map[string]string) (sessionrunnercore.AuthorizedRoute, error)
}

var _ codexauth.WorkloadLauncher = (*CodexDeviceAuthLauncher)(nil)

// Available reports whether at least one enrolled manager is connected right
// now. It backs GET /codex/device-auth/config so the frontend can tell "no
// execution plane" apart from "feature disabled".
func (l *CodexDeviceAuthLauncher) Available(ctx context.Context) bool {
	if l.local != nil {
		return true
	}
	if l.managers == nil || l.tunnel == nil {
		return false
	}
	managers, err := l.managers.ListManagers(ctx)
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
	if request.AttemptID == "" || request.CallbackURL == "" || request.Token == "" || request.ExpiresAt.IsZero() || request.SubjectID == "" {
		return errors.New("attempt_id, callback_url, token, expires_at, and subject_id are required")
	}
	subjectType := sessionrunnercore.SubjectType(request.SubjectType)
	if subjectType != sessionrunnercore.SubjectUser && subjectType != sessionrunnercore.SubjectTeam {
		return errors.New("subject_type must be user or team")
	}
	if l.routes == nil {
		return errors.New("authorized session route resolver is unavailable")
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
	resolved, err := l.routes.ResolveRoute(ctx, sessionrunnercore.Subject{Type: sessionrunnercore.SubjectType(request.SubjectType), ID: request.SubjectID}, "", nil)
	if err != nil {
		return "", fmt.Errorf("resolve authorized pool for Codex auth: %w", err)
	}
	if resolved == nil {
		return "", errors.New("no authorized and healthy session pool is available for Codex auth")
	}
	if resolved.Kind() == sessionrunnercore.RouteKindLocal {
		if l.local == nil {
			return "", errors.New("local Codex auth workload launcher is unavailable")
		}
		if err := l.local.StartCodexDeviceAuth(ctx, request); err != nil {
			return "", fmt.Errorf("start local Codex auth workload: %w", err)
		}
		l.active.Store(request.AttemptID, activeCodexAuthTarget{local: true})
		return "local", nil
	}
	if resolved.Kind() != sessionrunnercore.RouteKindPool {
		return "", fmt.Errorf("unsupported authorized route kind %q", resolved.Kind())
	}
	if l.tunnel == nil {
		return "", errors.New("external Codex auth workload tunnel is unavailable")
	}
	managers := orderManagers(resolved.Managers())
	log.Printf("[CODEX_AUTH_ESM] Attempt %s resolved pool=%s binding=%s with %d candidate manager(s)", request.AttemptID, resolved.PoolName(), resolved.BindingID(), len(managers))
	lastErr := errors.New("no connected session manager is available for codex device auth")
	for _, manager := range managers {
		if l.tunnel == nil {
			break
		}
		if !l.tunnel.IsConnected(ctx, manager.ID) {
			log.Printf("[CODEX_AUTH_ESM] Skipping disconnected manager %s for attempt %s", manager.ID, request.AttemptID)
			continue
		}
		resp, doErr := l.postWorkload(ctx, manager.ID, request)
		if doErr != nil {
			log.Printf("[CODEX_AUTH_ESM] Manager %s request failed for attempt %s: %v", manager.ID, request.AttemptID, doErr)
			lastErr = fmt.Errorf("manager %s: %w", manager.ID, doErr)
			continue
		}
		status := resp.StatusCode
		detail, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		_ = resp.Body.Close()
		switch status {
		case http.StatusAccepted:
			log.Printf("[CODEX_AUTH_ESM] Manager %s accepted attempt %s", manager.ID, request.AttemptID)
			l.active.Store(request.AttemptID, activeCodexAuthTarget{managerID: manager.ID})
			return manager.ID, nil
		case http.StatusNotFound, http.StatusNotImplemented:
			log.Printf("[CODEX_AUTH_ESM] Manager %s does not support attempt %s (status=%d)", manager.ID, request.AttemptID, status)
			// The manager cannot run auth workloads (unknown route on an older
			// revision, or an execution plane without the capability). Try the
			// next connected manager instead of failing the attempt.
			lastErr = fmt.Errorf("manager %s does not support codex device auth workloads", manager.ID)
		default:
			log.Printf("[CODEX_AUTH_ESM] Manager %s rejected attempt %s (status=%d detail=%q)", manager.ID, request.AttemptID, status, strings.TrimSpace(string(detail)))
			lastErr = fmt.Errorf("manager %s returned HTTP %d: %s", manager.ID, status, strings.TrimSpace(string(detail)))
		}
	}
	log.Printf("[CODEX_AUTH_ESM] No manager accepted attempt %s: %v", request.AttemptID, lastErr)
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
		target, _ := owner.(activeCodexAuthTarget)
		if target.local {
			return l.local.CancelCodexDeviceAuth(ctx, attemptID)
		}
		return l.deleteWorkload(ctx, target.managerID, attemptID)
	}
	if l.managers == nil {
		return errors.New("session pool directory is unavailable")
	}
	// Unknown attempt (for example after a parent restart). Broadcast the
	// cancel to every connected manager; deletion is idempotent there.
	managers, err := l.managers.ListManagers(ctx)
	if err != nil {
		return err
	}
	var lastErr error
	if l.local != nil {
		if err := l.local.CancelCodexDeviceAuth(ctx, attemptID); err != nil {
			lastErr = err
		}
	}
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

type activeCodexAuthTarget struct {
	managerID string
	local     bool
}

// orderManagers applies workload-specific preference only after the core
// resolver has produced an authorized route. It cannot add a manager that was
// not an enabled supplier of the selected pool.
func orderManagers(authorized []*sessionrunnercore.Manager) []*sessionrunnercore.Manager {
	result := append([]*sessionrunnercore.Manager(nil), authorized...)
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
	return result
}

type tunnelLiveness struct{ tunnel ControlTunnel }

func (l tunnelLiveness) IsManagerConnected(ctx context.Context, managerID string) (bool, error) {
	return l.tunnel.IsConnected(ctx, managerID), nil
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
	detail, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
	switch resp.StatusCode {
	case http.StatusOK, http.StatusNoContent, http.StatusAccepted, http.StatusNotFound, http.StatusNotImplemented:
		return nil
	default:
		return fmt.Errorf("manager %s returned HTTP %d: %s", managerID, resp.StatusCode, strings.TrimSpace(string(detail)))
	}
}
