package app

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/takutakahashi/agentapi-proxy/internal/core/sessioncontrol"
	sessionrunnercore "github.com/takutakahashi/agentapi-proxy/internal/core/sessionrunner"
	"github.com/takutakahashi/agentapi-proxy/internal/infrastructure/repositories"
	"github.com/takutakahashi/agentapi-proxy/internal/infrastructure/services"
	portrepos "github.com/takutakahashi/agentapi-proxy/internal/usecases/ports/repositories"
	"github.com/takutakahashi/agentapi-proxy/pkg/config"
	"github.com/takutakahashi/agentapi-proxy/pkg/sessionsettings"
	"github.com/takutakahashi/agentapi-proxy/pkg/telemetry"
	closeutils "github.com/takutakahashi/agentapi-proxy/pkg/utils"
	utilversion "k8s.io/apimachinery/pkg/util/version"
	"k8s.io/client-go/kubernetes"
)

type sessionManagerStockPurger interface {
	PurgeStockSessions(context.Context) error
}

func purgeSessionManagerStock(ctx context.Context, purger sessionManagerStockPurger) error {
	return purger.PurgeStockSessions(ctx)
}

func buildSessionManagerStatusEventRepository(cfg *config.Config, remoteMode bool) portrepos.StatusEventRepository {
	if remoteMode {
		return repositories.NewNoopStatusRepository()
	}
	return buildStatusEventRepository(cfg)
}

func buildSessionManagerControlStore(cfg *config.Config, remoteMode bool) sessioncontrol.Store {
	if remoteMode {
		return nil
	}
	return buildSessionControlStore(cfg)
}

func runSessionManagerVersionPoller(ctx context.Context, cfg *config.Config, client kubernetes.Interface, namespace string) {
	httpClient := &http.Client{Timeout: 10 * time.Second}
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()
	for {
		desired, err := fetchSessionManagerDesiredVersion(ctx, httpClient, cfg.SessionManager.UpgradeVersionURL, cfg.SessionManager.ConnectionToken)
		if err != nil {
			log.Printf("[SESSION_MANAGER] Version check failed: %v", err)
		} else if err := reconcileSessionManagerVersion(sessionManagerUpgrade{ctx: ctx, config: cfg, client: client, namespace: namespace, desired: desired}); err != nil {
			log.Printf("[SESSION_MANAGER] Auto-upgrade reconcile failed: %v", err)
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func fetchSessionManagerDesiredVersion(ctx context.Context, client *http.Client, endpoint, token string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return "", err
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer func() { closeutils.SafeCloseResponseBody(resp.Body) }()
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return "", fmt.Errorf("version endpoint returned HTTP %d", resp.StatusCode)
	}
	var result struct {
		Version string `json:"version"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", fmt.Errorf("decode version response: %w", err)
	}
	result.Version = strings.TrimSpace(result.Version)
	if result.Version == "" {
		return "", errors.New("version endpoint returned an empty version")
	}
	return result.Version, nil
}

type sessionManagerHeartbeat struct {
	upstream  string
	managerID string
	token     string
	config    *config.Config
	manager   *services.KubernetesSessionManager
	client    *http.Client
}

type sessionManagerHeartbeatResponse struct {
	ManagerID               string                                 `json:"manager_id"`
	Pools                   []*sessionrunnercore.PoolSupplier      `json:"pools"`
	RegisteredRunnerIDs     *[]string                              `json:"registered_runner_ids"`
	UpstreamVersion         string                                 `json:"upstream_version"`
	AllocatedRunnerPolicies map[string]sessionsettings.SessionMeta `json:"allocated_runner_policies"`
}

func runSessionRunnerManagerHeartbeat(ctx context.Context, heartbeat sessionManagerHeartbeat) {
	if heartbeat.client == nil {
		heartbeat.client = &http.Client{Timeout: 10 * time.Second}
	}
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()
	for {
		heartbeat.send(ctx)
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func (heartbeat *sessionManagerHeartbeat) send(ctx context.Context) {
	body := heartbeat.body(ctx)
	endpoint := strings.TrimRight(heartbeat.upstream, "/") + "/internal/session-managers/" + url.PathEscape(heartbeat.managerID) + "/heartbeat"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, body)
	if err != nil {
		log.Printf("[SESSION_MANAGER] Build runner pool heartbeat: %v", err)
		return
	}
	req.Header.Set("Authorization", "Bearer "+heartbeat.token)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := heartbeat.client.Do(req)
	if err != nil {
		log.Printf("[SESSION_MANAGER] Runner pool heartbeat failed: %v", err)
		return
	}
	defer closeutils.SafeCloseResponseBody(resp.Body)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		log.Printf("[SESSION_MANAGER] Runner pool heartbeat returned HTTP %d", resp.StatusCode)
		return
	}
	var result sessionManagerHeartbeatResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		log.Printf("[SESSION_MANAGER] Decode runner pool heartbeat: %v", err)
	}
	heartbeat.apply(ctx, result)
}

func (heartbeat *sessionManagerHeartbeat) body(ctx context.Context) io.Reader {
	runnerIDs, err := heartbeat.manager.ListRunnerSessionIDs(ctx)
	if err != nil {
		log.Printf("[SESSION_MANAGER] List local runner inventory: %v", err)
		return nil
	}
	statuses := make(map[string]string)
	for _, runnerID := range runnerIDs {
		if err := heartbeat.manager.RefreshRunnerSessionStatus(ctx, runnerID); err != nil {
			log.Printf("[SESSION_MANAGER] Refresh runner %s status: %v", runnerID, err)
		}
		if session := heartbeat.manager.GetSession(runnerID); session != nil {
			statuses[session.ID()] = session.Status()
		}
	}
	payload, err := json.Marshal(map[string]any{"local_runner_ids": runnerIDs, "session_statuses": statuses})
	if err != nil {
		log.Printf("[SESSION_MANAGER] Encode runner pool heartbeat: %v", err)
		return nil
	}
	return bytes.NewReader(payload)
}

func (heartbeat *sessionManagerHeartbeat) apply(ctx context.Context, result sessionManagerHeartbeatResponse) {
	if result.ManagerID != "" && result.ManagerID != heartbeat.managerID {
		log.Printf("[SESSION_MANAGER] Resolved registered manager ID %s (configured as %s)", result.ManagerID, heartbeat.managerID)
		heartbeat.managerID = result.ManagerID
		heartbeat.manager.ConfigureSessionRunnerPool(heartbeat.upstream, heartbeat.managerID, heartbeat.token, heartbeat.config.SessionManager.RunnerPool)
	}
	reconcileSessionRunnerHeartbeat(ctx, heartbeat.manager, result.Pools, result.RegisteredRunnerIDs)
	for runnerID, metadata := range result.AllocatedRunnerPolicies {
		if err := heartbeat.manager.ApplyRunnerAllocationMetadata(ctx, runnerID, metadata); err != nil {
			log.Printf("[SESSION_MANAGER] Apply runner %s allocation metadata: %v", runnerID, err)
		}
	}
	if err := reconcileSessionManagerVersion(sessionManagerUpgrade{ctx: ctx, config: heartbeat.config, client: heartbeat.manager.GetClient(), namespace: heartbeat.manager.GetNamespace(), desired: result.UpstreamVersion}); err != nil {
		log.Printf("[SESSION_MANAGER] Auto-upgrade reconcile failed: %v", err)
	}
}

func reconcileSessionRunnerHeartbeat(ctx context.Context, manager sessionRunnerInfrastructure, pools []*sessionrunnercore.PoolSupplier, registeredRunnerIDs *[]string) {
	telemetry.OperationVoid(ctx, "app.SessionManagerRuntime.reconcileHeartbeat", func(ctx context.Context) {
		// The parent inventory was captured before this heartbeat response. Remove
		// orphaned local runners before replenishing stock so a runner registered by
		// this reconciliation is not immediately deleted as absent from that stale
		// snapshot.
		if registeredRunnerIDs != nil {
			reconcileOrphanedSessionRunners(ctx, manager, *registeredRunnerIDs)
		}
		reconcileSessionRunnerPools(ctx, manager, pools)
	})
}

type orphanedSessionRunnerCleaner interface {
	DeleteRunnerSessionsNotRegistered(context.Context, map[string]struct{}) error
}

func reconcileOrphanedSessionRunners(ctx context.Context, manager sessionRunnerInfrastructure, registeredIDs []string) {
	telemetry.OperationVoid(ctx, "app.SessionManagerRuntime.reconcileOrphanedRunners", func(ctx context.Context) {
		cleaner, ok := manager.(orphanedSessionRunnerCleaner)
		if !ok {
			return
		}
		registered := make(map[string]struct{}, len(registeredIDs))
		for _, id := range registeredIDs {
			if id != "" {
				registered[id] = struct{}{}
			}
		}
		if err := cleaner.DeleteRunnerSessionsNotRegistered(ctx, registered); err != nil {
			log.Printf("[SESSION_MANAGER] Delete orphaned runner sessions: %v", err)
		}
	})
}

func sessionManagerUpgradeRequired(current, desired string) (bool, error) {
	// Development charts use immutable commit-addressed tags. Git commit order
	// is not encoded in the tag, so the authenticated parent is authoritative.
	if strings.HasPrefix(desired, "dev.ccplant.") {
		sha := strings.TrimPrefix(desired, "dev.ccplant.")
		if len(sha) != 40 {
			return false, fmt.Errorf("invalid upstream development version %q", desired)
		}
		if _, err := hex.DecodeString(sha); err != nil {
			return false, fmt.Errorf("invalid upstream development version %q", desired)
		}
		return current != desired, nil
	}
	desiredVersion, err := utilversion.ParseSemantic(desired)
	if err != nil {
		return false, fmt.Errorf("invalid upstream version %q: %w", desired, err)
	}
	currentVersion, err := utilversion.ParseSemantic(current)
	if err != nil {
		if strings.HasPrefix(current, "dev.ccplant.") {
			return true, nil
		}
		return false, fmt.Errorf("invalid current version %q: %w", current, err)
	}
	return currentVersion.LessThan(desiredVersion), nil
}

type sessionRunnerInfrastructure interface {
	CountStockSessionsForPool(context.Context, string, bool) (int, error)
	CountRunnerSessionsForPool(context.Context, string) (int, error)
	CreateStockSessionForPool(context.Context, string, bool) error
	PurgeStockSessionsForPool(context.Context, string) error
}

func reconcileSessionRunnerPools(ctx context.Context, manager sessionRunnerInfrastructure, pools []*sessionrunnercore.PoolSupplier) {
	telemetry.OperationVoid(ctx, "app.SessionManagerRuntime.reconcilePools", func(ctx context.Context) {
		for _, pool := range pools {
			reconcileSessionRunnerPool(ctx, manager, pool)
		}
	})
}

func reconcileSessionRunnerPool(ctx context.Context, manager sessionRunnerInfrastructure, pool *sessionrunnercore.PoolSupplier) {
	if pool == nil {
		return
	}
	if !pool.Enabled || pool.Draining {
		if err := manager.PurgeStockSessionsForPool(ctx, pool.Pool); err != nil {
			log.Printf("[SESSION_MANAGER] Purge disabled or draining pool %s stock: %v", pool.Pool, err)
		}
		return
	}
	if pool.MinIdle <= 0 {
		return
	}
	localTotal, err := manager.CountRunnerSessionsForPool(ctx, pool.Pool)
	if err != nil {
		log.Printf("[SESSION_MANAGER] Count pool %s runners: %v", pool.Pool, err)
		return
	}
	total := max(localTotal, pool.TotalRunners)
	stock := runnerPoolStock{ctx: ctx, manager: manager, pool: pool, total: total}
	stock.ensure(false)
	stock.ensure(true)
}

type runnerPoolStock struct {
	ctx     context.Context
	manager sessionRunnerInfrastructure
	pool    *sessionrunnercore.PoolSupplier
	total   int
}

func (stock *runnerPoolStock) ensure(dind bool) {
	idle, err := stock.manager.CountStockSessionsForPool(stock.ctx, stock.pool.Pool, dind)
	if err != nil {
		log.Printf("[SESSION_MANAGER] Count pool %s idle runners (dind=%t): %v", stock.pool.Pool, dind, err)
		return
	}
	for idle < stock.pool.MinIdle && (stock.pool.MaxRunners <= 0 || stock.total < stock.pool.MaxRunners) {
		if err := stock.manager.CreateStockSessionForPool(stock.ctx, stock.pool.Pool, dind); err != nil {
			log.Printf("[SESSION_MANAGER] Create pool %s runner (dind=%t): %v", stock.pool.Pool, dind, err)
			break
		}
		idle++
		stock.total++
	}
}

func newSessionManagerEncryptionRegistry() (*services.EncryptionServiceRegistry, error) {
	factory := services.NewEncryptionServiceFactory("AGENTAPI_ENCRYPTION")
	primary, err := factory.Create()
	if err != nil {
		return nil, fmt.Errorf("create session-manager encryption service: %w", err)
	}
	registry := services.NewEncryptionServiceRegistry(primary)
	registry.Register(services.NewNoopEncryptionService())
	decryptFactory := services.NewEncryptionServiceFactory("AGENTAPI_DECRYPTION")
	if decrypt, err := decryptFactory.Create(); err == nil && (decrypt.Algorithm() != primary.Algorithm() || decrypt.KeyID() != primary.KeyID()) {
		registry.Register(decrypt)
	}
	return registry, nil
}

type upstreamManagedFilesProxy struct {
	baseURL   string
	managerID string
	token     string
	client    *http.Client
}

func newUpstreamManagedFilesProxy(baseURL, managerID, token string) *upstreamManagedFilesProxy {
	return &upstreamManagedFilesProxy{
		baseURL: strings.TrimRight(baseURL, "/"), managerID: managerID, token: token,
		client: &http.Client{Timeout: 15 * time.Second},
	}
}
