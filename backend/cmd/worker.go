package cmd

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/spf13/cobra"
	"github.com/takutakahashi/agentapi-proxy/internal/infrastructure/controlapi"
	"github.com/takutakahashi/agentapi-proxy/internal/infrastructure/kvstore"
	"github.com/takutakahashi/agentapi-proxy/internal/infrastructure/repositories"
	"github.com/takutakahashi/agentapi-proxy/internal/modules/schedule"
	"github.com/takutakahashi/agentapi-proxy/pkg/config"
	slackbotcleanup "github.com/takutakahashi/agentapi-proxy/pkg/slackbot_cleanup"
	stockinventory "github.com/takutakahashi/agentapi-proxy/pkg/stock_inventory"
	"k8s.io/client-go/kubernetes/fake"
)

var workerConfigPath string
var workerVerbose bool

var WorkerCmd = &cobra.Command{Use: "worker", Short: "Start AgentAPI background workers", Args: cobra.NoArgs, RunE: runWorkers}

func init() {
	WorkerCmd.Flags().StringVarP(&workerConfigPath, "config", "c", "config.json", "Configuration file path")
	WorkerCmd.Flags().BoolVarP(&workerVerbose, "verbose", "v", false, "Enable verbose logging")
}

func runWorkers(_ *cobra.Command, _ []string) error {
	cfg, err := loadRuntimeConfig(workerConfigPath)
	if err != nil {
		return err
	}
	if workerVerbose {
		log.SetFlags(log.LstdFlags | log.Lshortfile)
	}
	if cfg.KubernetesSession.ProvisionerProxyURL == "" || cfg.KubernetesSession.ProvisionerToken == "" {
		return errors.New("worker control API URL and provisioner token are required")
	}
	store, err := newWorkerKVStore(cfg.KVStore)
	if err != nil {
		return err
	}
	defer func() { _ = store.Close() }()
	persistence := kvstore.NewKubernetesAdapter(fake.NewSimpleClientset(), store)
	namespace := resolveKubernetesNamespace(cfg.ScheduleWorker.Namespace, cfg.KubernetesSession.Namespace)
	remote := controlapi.NewSessionManager(cfg.KubernetesSession.ProvisionerProxyURL, cfg.KubernetesSession.ProvisionerToken)
	scheduleManager := schedule.NewKubernetesManager(persistence, namespace)
	memoryRepo := repositories.NewKubernetesMemoryRepository(persistence, namespace)
	profileRepo := repositories.NewKubernetesSessionProfileRepository(persistence, namespace)
	redisClient, err := newWorkerRedisClient(cfg)
	if err != nil {
		return err
	}
	defer func() { _ = redisClient.Close() }()

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	var mu sync.Mutex
	var scheduleWorker *schedule.LeaderWorker
	var cleanupWorker *slackbotcleanup.LeaderCleanupWorker
	var stockWorker *stockinventory.LeaderWorker
	if cfg.ScheduleWorker.Enabled {
		scheduleWorker = newRemoteScheduleWorker(cfg, scheduleManager, remote, memoryRepo, profileRepo, redisClient, namespace)
		go scheduleWorker.Run(ctx)
	}
	if cfg.SlackbotCleanupWorker.Enabled {
		cleanupWorker = newRemoteCleanupWorker(cfg, remote, redisClient, namespace)
		go cleanupWorker.Run(ctx)
	}
	if cfg.StockInventoryWorker.Enabled {
		stockWorker = newRemoteStockWorker(cfg, remote, redisClient, namespace)
		go stockWorker.Run(ctx)
	}
	<-ctx.Done()
	mu.Lock()
	defer mu.Unlock()
	if scheduleWorker != nil {
		scheduleWorker.Stop()
	}
	if cleanupWorker != nil {
		cleanupWorker.Stop()
	}
	if stockWorker != nil {
		stockWorker.Stop()
	}
	return nil
}

func newWorkerKVStore(cfg config.KVStoreConfig) (kvstore.Store, error) {
	backend := cfg.Backend
	databaseURL := cfg.DatabaseURL
	authToken := cfg.AuthToken
	if cfg.Primary != nil {
		backend = cfg.Primary.Backend
		databaseURL = cfg.Primary.DatabaseURL
		authToken = cfg.Primary.AuthToken
	}
	if backend != "libsql" {
		return nil, fmt.Errorf("worker kv_store backend must be libsql, got %q", backend)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	return kvstore.NewLibSQLStore(ctx, databaseURL, authToken)
}

func newRemoteScheduleWorker(cfg *config.Config, manager schedule.Manager, remote *controlapi.SessionManager, memory *repositories.KubernetesMemoryRepository, profiles *repositories.KubernetesSessionProfileRepository, redisClient redis.UniversalClient, namespace string) *schedule.LeaderWorker {
	interval, _ := time.ParseDuration(cfg.ScheduleWorker.CheckInterval)
	if interval <= 0 {
		interval = 30 * time.Second
	}
	election := workerElection(cfg.ScheduleWorker.LeaseDuration, cfg.ScheduleWorker.RenewDeadline, cfg.ScheduleWorker.RetryPeriod, schedule.ScheduleWorkerLeaseName, namespace)
	return schedule.NewLeaderWorker(manager, remote, redisClient, schedule.WorkerConfig{CheckInterval: interval, Enabled: true}, election, memory, profiles)
}

func newRemoteCleanupWorker(cfg *config.Config, remote *controlapi.SessionManager, redisClient redis.UniversalClient, namespace string) *slackbotcleanup.LeaderCleanupWorker {
	check, _ := time.ParseDuration(cfg.SlackbotCleanupWorker.CheckInterval)
	ttl, _ := time.ParseDuration(cfg.SlackbotCleanupWorker.SessionTTL)
	ttlCheck, _ := time.ParseDuration(cfg.SlackbotCleanupWorker.SessionTTLCheckInterval)
	election := workerElection(cfg.SlackbotCleanupWorker.LeaseDuration, cfg.SlackbotCleanupWorker.RenewDeadline, cfg.SlackbotCleanupWorker.RetryPeriod, schedule.SlackbotCleanupWorkerLeaseName, namespace)
	return slackbotcleanup.NewLeaderCleanupWorker(remote, redisClient, slackbotcleanup.CleanupWorkerConfig{CheckInterval: check, SessionTTL: ttl, SessionTTLCheckInterval: ttlCheck, Enabled: true, DryRun: cfg.SlackbotCleanupWorker.DryRun}, election)
}

func newRemoteStockWorker(cfg *config.Config, remote *controlapi.SessionManager, redisClient redis.UniversalClient, namespace string) *stockinventory.LeaderWorker {
	interval, _ := time.ParseDuration(cfg.StockInventoryWorker.CheckInterval)
	if interval <= 0 {
		interval = 30 * time.Second
	}
	election := workerElection(cfg.StockInventoryWorker.LeaseDuration, cfg.StockInventoryWorker.RenewDeadline, cfg.StockInventoryWorker.RetryPeriod, schedule.StockInventoryWorkerLeaseName, namespace)
	return stockinventory.NewLeaderWorker(remote, redisClient, stockinventory.WorkerConfig{CheckInterval: interval, TargetCount: cfg.StockInventoryWorker.TargetCount, Requirements: stockinventory.StockRequirements{DinD: cfg.StockInventoryWorker.DockerEnabled}, Pools: buildStockInventoryPools(cfg.StockInventoryWorker, cfg.StockInventoryWorker.TargetCount), Enabled: true}, election)
}

func workerElection(lease, renew, retry, name, namespace string) schedule.LeaderElectionConfig {
	l, _ := time.ParseDuration(lease)
	r, _ := time.ParseDuration(renew)
	p, _ := time.ParseDuration(retry)
	if l <= 0 {
		l = 15 * time.Second
	}
	if r <= 0 {
		r = 10 * time.Second
	}
	if p <= 0 {
		p = 2 * time.Second
	}
	return schedule.LeaderElectionConfig{LeaseDuration: l, RenewDeadline: r, RetryPeriod: p, LeaseName: name, Namespace: namespace}
}

func loadRuntimeConfig(path string) (*config.Config, error) {
	cfg, err := config.LoadConfig(path)
	if err == nil {
		return cfg, nil
	}
	log.Printf("Failed to load config from %s, trying environment: %v", path, err)
	return config.LoadConfig("")
}
