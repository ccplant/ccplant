package schedule

import (
	"context"
	"errors"
	"log"
	"os"
	"time"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
	portrepos "github.com/takutakahashi/agentapi-proxy/internal/usecases/ports/repositories"
)

const (
	ScheduleWorkerLeaseName        = "agentapi-schedule-worker"
	SlackbotCleanupWorkerLeaseName = "agentapi-slackbot-cleanup-worker"
	StockInventoryWorkerLeaseName  = "agentapi-stock-inventory-worker"
	SessionAllocatorLeaseName      = "agentapi-session-allocator"
)

type LeaderElectionConfig struct {
	LeaseDuration time.Duration
	RenewDeadline time.Duration
	RetryPeriod   time.Duration
	LeaseName     string
	Namespace     string
}

func DefaultLeaderElectionConfig(namespace string) LeaderElectionConfig {
	return LeaderElectionConfig{LeaseDuration: 9 * time.Minute, RenewDeadline: 6 * time.Minute, RetryPeriod: time.Minute, LeaseName: ScheduleWorkerLeaseName, Namespace: namespace}
}

// LeaderElector delegates atomic lease operations to its client. Workers use the
// control API client, while legacy in-process workers use the Redis adapter.
type LeaderElector struct {
	client   LeaseClient
	config   LeaderElectionConfig
	identity string
}

type LeaseClient interface {
	Acquire(context.Context, string, string, time.Duration) (bool, error)
	Renew(context.Context, string, string, time.Duration) (bool, error)
	Release(context.Context, string, string) (bool, error)
}

type redisLeaseClient struct{ client redis.UniversalClient }

func NewLeaderElector(client LeaseClient, config LeaderElectionConfig) *LeaderElector {
	hostname, _ := os.Hostname()
	return &LeaderElector{client: client, config: config, identity: hostname + "_" + uuid.NewString()[:8]}
}

func NewRedisLeaseClient(client redis.UniversalClient) LeaseClient {
	return &redisLeaseClient{client: client}
}

func NewRedisLeaderElector(client redis.UniversalClient, config LeaderElectionConfig) *LeaderElector {
	return NewLeaderElector(NewRedisLeaseClient(client), config)
}

func (c *redisLeaseClient) Acquire(ctx context.Context, key, identity string, duration time.Duration) (bool, error) {
	return c.client.SetNX(ctx, key, identity, duration).Result()
}

func (c *redisLeaseClient) Renew(ctx context.Context, key, identity string, duration time.Duration) (bool, error) {
	result, err := renewLease.Run(ctx, c.client, []string{key}, identity, duration.Milliseconds()).Int64()
	return result != 0, err
}

func (c *redisLeaseClient) Release(ctx context.Context, key, identity string) (bool, error) {
	result, err := releaseLease.Run(ctx, c.client, []string{key}, identity).Int64()
	return result != 0, err
}

var renewLease = redis.NewScript(`
if redis.call("get", KEYS[1]) == ARGV[1] then
  return redis.call("pexpire", KEYS[1], ARGV[2])
end
return 0`)

var releaseLease = redis.NewScript(`
if redis.call("get", KEYS[1]) == ARGV[1] then
  return redis.call("del", KEYS[1])
end
return 0`)

func (l *LeaderElector) Run(ctx context.Context, onStartedLeading func(context.Context), onStoppedLeading func()) {
	key := "agentapi:leader:" + l.config.Namespace + ":" + l.config.LeaseName
	for ctx.Err() == nil {
		acquired, err := l.client.Acquire(ctx, key, l.identity, l.config.LeaseDuration)
		if err != nil || !acquired {
			if err != nil {
				log.Printf("[LEADER_ELECTION] lease acquire failed: %v", err)
			}
			if !waitFor(ctx, l.config.RetryPeriod) {
				return
			}
			continue
		}
		leaderCtx, cancel := context.WithCancel(ctx)
		done := make(chan struct{})
		go func() { defer close(done); onStartedLeading(leaderCtx) }()
		lost := l.renew(ctx, key)
		cancel()
		<-done
		_, _ = l.client.Release(context.Background(), key, l.identity)
		if onStoppedLeading != nil {
			onStoppedLeading()
		}
		if !lost || ctx.Err() != nil {
			return
		}
	}
}

func (l *LeaderElector) renew(ctx context.Context, key string) bool {
	interval := l.config.RenewDeadline / 2
	if interval <= 0 {
		interval = time.Second
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return false
		case <-ticker.C:
			renewed, err := l.client.Renew(ctx, key, l.identity, l.config.LeaseDuration)
			if err != nil || !renewed {
				return true
			}
		}
	}
}

func waitFor(ctx context.Context, duration time.Duration) bool {
	timer := time.NewTimer(duration)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}

func (l *LeaderElector) Identity() string { return l.identity }

type LeaderWorker struct {
	worker  *Worker
	elector *LeaderElector
}

func NewLeaderWorker(manager Manager, sessionManager portrepos.SessionManager, client LeaseClient, workerConfig WorkerConfig, electionConfig LeaderElectionConfig, memoryRepo portrepos.MemoryRepository, sessionProfileRepo portrepos.SessionProfileRepository) *LeaderWorker {
	return &LeaderWorker{worker: NewWorker(manager, sessionManager, memoryRepo, workerConfig, sessionProfileRepo), elector: NewLeaderElector(client, electionConfig)}
}

var ErrRedisRequired = errors.New("worker leader election requires Redis")

func (lw *LeaderWorker) Run(ctx context.Context) {
	lw.elector.Run(ctx, func(leaderCtx context.Context) { _ = lw.worker.Start(leaderCtx); <-leaderCtx.Done() }, lw.worker.Stop)
}

func (lw *LeaderWorker) Stop() { lw.worker.Stop() }
