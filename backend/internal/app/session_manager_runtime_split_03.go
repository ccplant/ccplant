package app

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"time"

	"github.com/labstack/echo/v4"
	"github.com/redis/go-redis/v9"
	coreallocation "github.com/takutakahashi/agentapi-proxy/internal/core/sessionallocation"
	"github.com/takutakahashi/agentapi-proxy/internal/infrastructure/services"
	"github.com/takutakahashi/agentapi-proxy/internal/interfaces/controllers"
	portrepos "github.com/takutakahashi/agentapi-proxy/internal/usecases/ports/repositories"
	"github.com/takutakahashi/agentapi-proxy/pkg/config"
	"github.com/takutakahashi/agentapi-proxy/pkg/sessionsettings"
	closeutils "github.com/takutakahashi/agentapi-proxy/pkg/utils"
)

func (p *upstreamManagedFilesProxy) SaveManagedFiles(ctx context.Context, sessionID, owner, syncID string, files []sessionsettings.ManagedFile, expected map[string]string) error {
	body, err := json.Marshal(struct {
		Owner    string                        `json:"owner"`
		SyncID   string                        `json:"sync_id,omitempty"`
		Files    []sessionsettings.ManagedFile `json:"files"`
		Expected map[string]string             `json:"expected,omitempty"`
	}{Owner: owner, SyncID: syncID, Files: files, Expected: expected})
	if err != nil {
		return err
	}
	endpoint := p.baseURL + "/internal/session-managers/" + url.PathEscape(p.managerID) + "/sessions/" + url.PathEscape(sessionID) + "/managed-files"
	req, err := http.NewRequestWithContext(ctx, http.MethodPut, endpoint, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+p.token)
	req.Header.Set("Content-Type", "application/json")
	resp, err := p.client.Do(req)
	if err != nil {
		return err
	}
	defer func() { closeutils.SafeCloseResponseBody(resp.Body) }()
	if resp.StatusCode == http.StatusConflict {
		return controllers.ErrManagedFileConflict
	}
	if resp.StatusCode != http.StatusNoContent {
		return fmt.Errorf("parent managed-file persistence returned HTTP %d", resp.StatusCode)
	}
	return nil
}

func newSessionManagerRedis(cfg *config.Config) (*redis.Client, error) {
	if cfg.Redis.Addr == "" {
		return nil, errors.New("session-manager Redis is required for allocation leader election")
	}
	opts := &redis.Options{Addr: cfg.Redis.Addr, Password: cfg.Redis.Password, DB: cfg.Redis.DB}
	if d, err := time.ParseDuration(cfg.Redis.DialTimeout); err == nil && d > 0 {
		opts.DialTimeout = d
	}
	if d, err := time.ParseDuration(cfg.Redis.ReadTimeout); err == nil && d > 0 {
		opts.ReadTimeout = d
	}
	if d, err := time.ParseDuration(cfg.Redis.WriteTimeout); err == nil && d > 0 {
		opts.WriteTimeout = d
	}
	if cfg.Redis.TLSEnabled {
		opts.TLSConfig = &tls.Config{MinVersion: tls.VersionTLS12}
	}
	client := redis.NewClient(opts)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := client.Ping(ctx).Err(); err != nil {
		_ = client.Close()
		return nil, fmt.Errorf("connect session-manager Redis: %w", err)
	}
	return client, nil
}

func durationOr(raw string, fallback time.Duration) time.Duration {
	parsed, err := time.ParseDuration(raw)
	if err != nil || parsed <= 0 {
		return fallback
	}
	return parsed
}

type localAllocationClient struct {
	queue  coreallocation.Queue
	routes portrepos.SessionRouteRepository
}

func newLocalAllocationClient(queue coreallocation.Queue, routes portrepos.SessionRouteRepository) *localAllocationClient {
	return &localAllocationClient{queue: queue, routes: routes}
}

func (c *localAllocationClient) Next(ctx context.Context, wait time.Duration) (*coreallocation.AllocationRequest, bool, error) {
	return c.queue.NextSessionAllocation(ctx, wait)
}

func (c *localAllocationClient) Complete(ctx context.Context, sessionID string, result coreallocation.AllocationResult) error {
	// Persist the public-to-runtime stock alias before acknowledging the queue.
	// Queue completion deletes the allocation, so doing this in the opposite
	// order can permanently lose the only mapping after a crash or KV failure.
	if result.Status == coreallocation.StatusAssigned && result.AllocatedSessionID != "" && result.AllocatedSessionID != sessionID {
		if c.routes == nil {
			return fmt.Errorf("save session route for allocation %s: route repository is not configured", sessionID)
		}
		reader, ok := c.queue.(coreallocation.AllocationReader)
		if !ok {
			return fmt.Errorf("load allocation %s before saving session route: queue does not support durable allocation reads", sessionID)
		}
		allocation, err := reader.GetSessionAllocation(ctx, sessionID)
		if err != nil {
			return fmt.Errorf("load allocation %s before saving session route: %w", sessionID, err)
		}
		if allocation == nil {
			return fmt.Errorf("load allocation %s before saving session route: allocation not found", sessionID)
		}
		if allocation.SessionID != sessionID {
			return fmt.Errorf("load allocation %s before saving session route: durable allocation has session ID %s", sessionID, allocation.SessionID)
		}
		route := &portrepos.SessionRoute{SessionID: allocation.SessionID, RemoteSessionID: result.AllocatedSessionID, StartedAt: time.Now(), Status: "starting"}
		if allocation.Request != nil {
			route.UserID = allocation.Request.UserID
			route.Scope = string(allocation.Request.Scope)
			route.TeamID = allocation.Request.TeamID
			route.Tags = allocation.Request.Tags
			route.InitialMessage = allocation.Request.InitialMessage
		}
		if err := c.routes.Save(ctx, route); err != nil {
			return err
		}
	}
	if _, err := c.queue.CompleteSessionAllocation(ctx, sessionID, result); err != nil {
		return err
	}
	return nil
}

func (r *SessionManagerRuntime) Echo() *echo.Echo { return r.echo }

func (r *SessionManagerRuntime) Manager() *services.KubernetesSessionManager { return r.manager }

func (r *SessionManagerRuntime) Config() *config.Config { return r.config }

func (r *SessionManagerRuntime) Shutdown(timeout time.Duration) error {
	if r.runtimeCancel != nil {
		r.runtimeCancel()
	}
	if r.allocator != nil {
		r.allocator.Stop()
	}
	var managerErr, kvErr, redisErr error
	if r.manager != nil {
		managerErr = r.manager.Shutdown(timeout)
	}
	if r.kvStore != nil {
		kvErr = r.kvStore.Close()
	}
	if r.redis != nil {
		redisErr = r.redis.Close()
	}
	return errors.Join(managerErr, kvErr, redisErr)
}
