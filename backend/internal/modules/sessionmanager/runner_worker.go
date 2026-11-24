package sessionmanager

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/takutakahashi/agentapi-proxy/internal/domain/entities"
	"github.com/takutakahashi/agentapi-proxy/internal/usecases/ports/repositories"
	"github.com/takutakahashi/agentapi-proxy/pkg/sessionsettings"
	"github.com/takutakahashi/agentapi-proxy/pkg/telemetry"
	"github.com/takutakahashi/agentapi-proxy/pkg/utils"
)

// RunnerWorker exposes native process capacity through the same runner_claim_v1
// protocol used by pre-warmed Kubernetes Pods. Each goroutine represents one
// idle slot, so the parent store's compare-and-swap claim remains the sole
// allocator when multiple managers supply the same pool.
type RunnerWorker struct {
	manager                    repositories.SessionManager
	upstream, managerID, token string
	client                     *http.Client
	mu                         sync.Mutex
	idle                       map[string]int
}

type directSessionManager interface {
	CreateSessionDirect(context.Context, string, *entities.RunServerRequest, []byte) (entities.Session, error)
}

type runnerPool struct {
	Pool       string `json:"pool"`
	MinIdle    int    `json:"min_idle"`
	MaxRunners int    `json:"max_runners"`
	Enabled    bool   `json:"enabled"`
	Draining   bool   `json:"draining"`
}
type nativeClaim struct {
	Allocation struct {
		SessionID  string `json:"session_id"`
		ManagerID  string `json:"manager_id"`
		Generation int64  `json:"generation"`
	} `json:"allocation"`
	LeaseID      string                           `json:"lease_id"`
	RuntimeToken string                           `json:"runtime_token"`
	Settings     *sessionsettings.SessionSettings `json:"settings"`
}

type completionRequest struct {
	runnerID, token, sessionID, leaseID, action string
	remoteSessionID                             string
}

type managerCall struct {
	method, path  string
	input, output any
}

func NewRunnerWorker(manager repositories.SessionManager, upstream, managerID, token string) *RunnerWorker {
	return &RunnerWorker{manager: manager, upstream: strings.TrimRight(upstream, "/"), managerID: managerID, token: token, client: utils.NewHTTPClient(utils.HTTPClientConfig{Timeout: 35 * time.Second}), idle: map[string]int{}}
}

func (w *RunnerWorker) Start(ctx context.Context) {
	if w.managerID == "" {
		log.Printf("[SESSION_RUNNER] manager ID is required")
		return
	}
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()
	for {
		w.reconcile(ctx)
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func (w *RunnerWorker) reconcile(ctx context.Context) {
	telemetry.OperationVoid(ctx, "sessionmanager.RunnerWorker.reconcile", w.reconcileOnce)
}

func (w *RunnerWorker) reconcileOnce(ctx context.Context) {
	// Native managers communicate only with the backend. The backend owns the
	// shared Redis connection used for liveness, pool state, and control traffic.
	sessions := w.manager.ListSessions(entities.SessionFilter{})
	statuses := make(map[string]string, len(sessions))
	for _, session := range sessions {
		statuses[session.ID()] = session.Status()
	}
	result, err := w.sendHeartbeat(ctx, statuses)
	if err != nil {
		log.Printf("[SESSION_RUNNER] heartbeat failed: %v", err)
		return
	}
	cleanupOrphanedSessions(w.manager, result.OrphanedSessionIDs)
	// Re-read after orphan cleanup so stale local workloads do not consume pool
	// capacity for another heartbeat interval.
	active := activeSessionCount(w.manager.ListSessions(entities.SessionFilter{}))
	for _, pool := range result.Pools {
		w.ensureIdleCapacity(ctx, pool, active)
	}
}

type heartbeatResult struct {
	Pools              []runnerPool `json:"pools"`
	OrphanedSessionIDs []string     `json:"orphaned_session_ids"`
}

func (w *RunnerWorker) sendHeartbeat(ctx context.Context, statuses map[string]string) (*heartbeatResult, error) {
	body, err := json.Marshal(map[string]any{"session_statuses": statuses})
	if err != nil {
		return nil, fmt.Errorf("encode heartbeat: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, w.upstream+"/internal/session-managers/"+url.PathEscape(w.managerID)+"/heartbeat", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("create heartbeat request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+w.token)
	req.Header.Set("Content-Type", "application/json")
	resp, err := w.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { utils.SafeCloseResponseBody(resp.Body) }()
	if resp.StatusCode/100 != 2 {
		return nil, fmt.Errorf("heartbeat returned HTTP %d", resp.StatusCode)
	}
	var result heartbeatResult
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("decode heartbeat: %w", err)
	}
	return &result, nil
}

func (w *RunnerWorker) ensureIdleCapacity(ctx context.Context, pool runnerPool, active int) {
	if !pool.Enabled || pool.Draining {
		return
	}
	desired := max(1, pool.MinIdle)
	if pool.MaxRunners > 0 {
		desired = min(desired, pool.MaxRunners-active)
	}
	desired = max(0, desired)
	w.mu.Lock()
	missing := max(0, desired-w.idle[pool.Pool])
	w.idle[pool.Pool] += missing
	w.mu.Unlock()
	for range missing {
		go w.runSlot(ctx, pool.Pool)
	}
}

func activeSessionCount(sessions []entities.Session) int {
	active := 0
	for _, session := range sessions {
		if session == nil {
			continue
		}
		switch strings.ToLower(session.Status()) {
		case "error", "failed", "exited", "stopped", "terminated":
			continue
		default:
			active++
		}
	}
	return active
}

type orphanSessionDeleter interface {
	DeleteSession(string) error
}

func cleanupOrphanedSessions(manager orphanSessionDeleter, sessionIDs []string) {
	for _, sessionID := range sessionIDs {
		if sessionID == "" {
			continue
		}
		if err := manager.DeleteSession(sessionID); err != nil {
			log.Printf("[SESSION_RUNNER] cleanup orphaned session %s: %v", sessionID, err)
			continue
		}
		log.Printf("[SESSION_RUNNER] cleaned up orphaned session %s absent from parent routes", sessionID)
	}
}

func (w *RunnerWorker) runSlot(ctx context.Context, pool string) {
	telemetry.OperationVoid(ctx, "sessionmanager.RunnerWorker.runSlot", func(ctx context.Context) {
		w.runSlotOnce(ctx, pool)
	}, telemetry.String("runner.pool", pool))
}

func (w *RunnerWorker) runSlotOnce(ctx context.Context, pool string) {
	defer func() { w.mu.Lock(); w.idle[pool]--; w.mu.Unlock() }()
	runnerID := uuid.NewString()
	runnerToken, err := w.register(ctx, runnerID, pool)
	if err != nil {
		log.Printf("[SESSION_RUNNER] register %s: %v", runnerID, err)
		return
	}
	for ctx.Err() == nil {
		claim, ok, err := w.claim(ctx, runnerID, runnerToken, pool)
		if err != nil {
			log.Printf("[SESSION_RUNNER] claim pool %s: %v", pool, err)
			sleepOrDone(ctx, 3*time.Second)
			continue
		}
		if !ok {
			continue
		}
		if claim.Settings == nil || claim.RuntimeToken == "" || claim.Allocation.SessionID == "" {
			w.completeAndLog(ctx, completionRequest{runnerID, runnerToken, claim.Allocation.SessionID, claim.LeaseID, "fail", ""})
			continue
		}
		creator, ok := w.manager.(directSessionManager)
		if !ok {
			w.completeAndLog(ctx, completionRequest{runnerID, runnerToken, claim.Allocation.SessionID, claim.LeaseID, "fail", ""})
			return
		}
		// The parent route addresses the allocated workload by runner ID. Keep the
		// manager's local session key aligned with that route so lifecycle commands
		// (especially DELETE) target the process that was actually created.
		createdSession, createErr := w.createClaimSession(ctx, creator, runnerID, claim)
		if createErr != nil {
			log.Printf("[SESSION_RUNNER] create %s for session %s: %v", runnerID, claim.Allocation.SessionID, createErr)
			w.completeAndLog(ctx, completionRequest{runnerID, runnerToken, claim.Allocation.SessionID, claim.LeaseID, "fail", ""})
			continue
		}
		if createdSession == nil || createdSession.ID() == "" {
			log.Printf("[SESSION_RUNNER] create %s for session %s returned no session ID", runnerID, claim.Allocation.SessionID)
			w.completeAndLog(ctx, completionRequest{runnerID, runnerToken, claim.Allocation.SessionID, claim.LeaseID, "fail", ""})
			continue
		}
		if err = w.complete(ctx, completionRequest{runnerID, runnerToken, claim.Allocation.SessionID, claim.LeaseID, "ack", createdSession.ID()}); err != nil {
			log.Printf("[SESSION_RUNNER] ack %s: %v", claim.Allocation.SessionID, err)
		}
		return
	}
}

func (w *RunnerWorker) createClaimSession(ctx context.Context, creator directSessionManager, runnerID string, claim *nativeClaim) (entities.Session, error) {
	claim.Settings.ParentRuntime = &sessionsettings.ParentRuntimeConfig{Enabled: true, Endpoint: w.upstream, SessionID: claim.Allocation.SessionID, ManagerID: claim.Allocation.ManagerID, Token: claim.RuntimeToken, Generation: claim.Allocation.Generation}
	createdSession, err := creator.CreateSessionDirect(ctx, runnerID, runRequestFromSettings(claim.Settings), nil)
	if err != nil {
		return nil, err
	}
	if createdSession == nil {
		return nil, fmt.Errorf("session manager returned no session")
	}
	return createdSession, nil
}

func (w *RunnerWorker) register(ctx context.Context, id, pool string) (string, error) {
	var out struct {
		RunnerToken string `json:"runner_token"`
	}
	err := w.managerRequest(ctx, managerCall{http.MethodPost, "/internal/session-runners/register", map[string]string{"runner_id": id, "pool": pool}, &out})
	return out.RunnerToken, err
}
func (w *RunnerWorker) claim(ctx context.Context, id, token, pool string) (*nativeClaim, bool, error) {
	u := w.upstream + "/internal/session-runners/allocations/next?wait=30s&pool=" + url.QueryEscape(pool)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, false, fmt.Errorf("create claim request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("X-Session-Runner-ID", id)
	resp, err := w.client.Do(req)
	if err != nil {
		return nil, false, err
	}
	defer func() { utils.SafeCloseResponseBody(resp.Body) }()
	if resp.StatusCode == http.StatusNoContent {
		return nil, false, nil
	}
	if resp.StatusCode != http.StatusOK {
		return nil, false, fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	var out nativeClaim
	err = json.NewDecoder(resp.Body).Decode(&out)
	return &out, true, err
}
func (w *RunnerWorker) complete(ctx context.Context, completion completionRequest) error {
	body := map[string]string{"lease_id": completion.leaseID}
	if completion.action == "ack" && completion.remoteSessionID != "" {
		body["remote_session_id"] = completion.remoteSessionID
	}
	raw, err := json.Marshal(body)
	if err != nil {
		return fmt.Errorf("encode completion: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, w.upstream+"/internal/session-runners/allocations/"+url.PathEscape(completion.sessionID)+"/"+completion.action, bytes.NewReader(raw))
	if err != nil {
		return fmt.Errorf("create completion request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+completion.token)
	req.Header.Set("X-Session-Runner-ID", completion.runnerID)
	req.Header.Set("Content-Type", "application/json")
	resp, err := w.client.Do(req)
	if err != nil {
		return err
	}
	defer func() { utils.SafeCloseResponseBody(resp.Body) }()
	if resp.StatusCode/100 != 2 {
		return fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	return nil
}

func (w *RunnerWorker) completeAndLog(ctx context.Context, completion completionRequest) {
	if err := w.complete(ctx, completion); err != nil {
		log.Printf("[SESSION_RUNNER] %s allocation %s: %v", completion.action, completion.sessionID, err)
	}
}

func (w *RunnerWorker) managerRequest(ctx context.Context, call managerCall) error {
	raw, err := json.Marshal(call.input)
	if err != nil {
		return fmt.Errorf("encode manager request: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, call.method, w.upstream+call.path, bytes.NewReader(raw))
	if err != nil {
		return fmt.Errorf("create manager request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+w.token)
	req.Header.Set("X-Session-Manager-ID", w.managerID)
	req.Header.Set("Content-Type", "application/json")
	resp, err := w.client.Do(req)
	if err != nil {
		return err
	}
	defer func() { utils.SafeCloseResponseBody(resp.Body) }()
	if resp.StatusCode/100 != 2 {
		return fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	return json.NewDecoder(resp.Body).Decode(call.output)
}

func runRequestFromSettings(settings *sessionsettings.SessionSettings) *entities.RunServerRequest {
	req := &entities.RunServerRequest{UserID: settings.Session.UserID, Scope: entities.ResourceScope(settings.Session.Scope), TeamID: settings.Session.TeamID, AgentType: settings.Session.AgentType, Teams: settings.Session.Teams, InitialMessage: settings.InitialMessage, ProvisionSettings: settings}
	for _, storage := range settings.Storages {
		resolved := entities.ResolvedStorageAttachment{StorageAttachment: entities.StorageAttachment{StorageID: storage.StorageID, StoragePath: storage.StoragePath, MountPath: storage.MountPath, ReadOnly: storage.ReadOnly, Managed: storage.Managed}, Kind: entities.StorageKind(storage.Kind), Revision: storage.Revision}
		for _, file := range storage.Files {
			resolved.Files = append(resolved.Files, entities.StorageFile{Path: file.Path, Digest: file.Digest, Size: file.Size, Mode: file.Mode, Revision: file.Revision})
		}
		req.Storages = append(req.Storages, resolved)
	}
	if settings.Repository != nil {
		req.RepoInfo = &entities.RepositoryInfo{FullName: settings.Repository.FullName, CloneDir: settings.Repository.CloneDir, Branch: settings.Repository.Branch, PR: settings.Repository.PR}
	}
	return req
}

func sleepOrDone(ctx context.Context, duration time.Duration) {
	timer := time.NewTimer(duration)
	defer timer.Stop()
	select {
	case <-ctx.Done():
	case <-timer.C:
	}
}
