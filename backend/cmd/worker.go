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

	"github.com/spf13/cobra"
	"github.com/takutakahashi/agentapi-proxy/internal/infrastructure/controlapi"
	"github.com/takutakahashi/agentapi-proxy/internal/infrastructure/kvstore"
	"github.com/takutakahashi/agentapi-proxy/internal/infrastructure/repositories"
	"github.com/takutakahashi/agentapi-proxy/internal/infrastructure/services"
	"github.com/takutakahashi/agentapi-proxy/internal/modules/schedule"
	"github.com/takutakahashi/agentapi-proxy/internal/modules/slackbot"
	"github.com/takutakahashi/agentapi-proxy/pkg/config"
	slackbotcleanup "github.com/takutakahashi/agentapi-proxy/pkg/slackbot_cleanup"
	stockinventory "github.com/takutakahashi/agentapi-proxy/pkg/stock_inventory"
	"github.com/takutakahashi/agentapi-proxy/pkg/telemetry"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/fake"
	ctrlconfig "sigs.k8s.io/controller-runtime/pkg/client/config"
)

var workerConfigPath string
var workerVerbose bool

var WorkerCmd = &cobra.Command{Use: "worker", Short: "Start AgentAPI background workers", Args: cobra.NoArgs, RunE: runWorkers}

func init() {
	WorkerCmd.Flags().StringVarP(&workerConfigPath, "config", "c", "config.json", "Configuration file path")
	WorkerCmd.Flags().BoolVarP(&workerVerbose, "verbose", "v", false, "Enable verbose logging")
}

func runWorkers(_ *cobra.Command, _ []string) error {
	shutdownTelemetry, err := telemetry.Setup(context.Background())
	if err != nil {
		return err
	}
	defer func() {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = shutdownTelemetry(shutdownCtx)
	}()

	cfg, err := loadRuntimeConfig(workerConfigPath)
	if err != nil {
		return err
	}
	if workerVerbose {
		log.SetFlags(log.LstdFlags | log.Lshortfile)
	}
	controlURL := cfg.Worker.ControlAPIURL
	if controlURL == "" {
		// Keep config-file compatibility while charts migrate to worker.controlApi.
		// The credential is intentionally not inherited: a provisioner credential
		// must never grant worker-control access.
		controlURL = cfg.KubernetesSession.ProvisionerProxyURL
	}
	if controlURL == "" || cfg.Worker.ControlAPIToken == "" {
		return errors.New("worker control API URL and worker control token are required")
	}
	if cfg.Worker.SessionAPIURL == "" {
		cfg.Worker.SessionAPIURL = controlURL
	}
	store, err := newWorkerKVStore(cfg.KVStore)
	if err != nil {
		return err
	}
	defer func() { _ = store.Close() }()
	persistence := kvstore.NewKubernetesAdapter(fake.NewSimpleClientset(), store)
	runtimeNamespace := resolveKubernetesNamespace(cfg.ScheduleWorker.Namespace, cfg.KubernetesSession.Namespace)
	persistenceNamespace := cfg.KVStore.Namespace
	if persistenceNamespace == "" {
		persistenceNamespace = "default"
	}
	if err := configureWorkerSlackCredential(context.Background(), cfg, persistence, persistenceNamespace); err != nil {
		return err
	}
	remote := controlapi.NewSessionManager(controlURL, cfg.Worker.ControlAPIToken)
	memoryRepo := repositories.NewKubernetesMemoryRepository(persistence, persistenceNamespace)
	encryption, err := services.NewEncryptionServiceFactory("AGENTAPI_ENCRYPTION").Create()
	if err != nil {
		return err
	}
	encryptionRegistry := services.NewEncryptionServiceRegistry(encryption)
	if decryption, err := services.NewEncryptionServiceFactory("AGENTAPI_DECRYPTION").Create(); err == nil {
		encryptionRegistry.Register(decryption)
	}
	profileRepo := repositories.NewKubernetesSessionProfileRepository(persistence, persistenceNamespace, encryptionRegistry)
	leaseClient := remote

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	var mu sync.Mutex
	var scheduleWorker *remoteScheduleWorker
	var cleanupWorker *slackbotcleanup.LeaderCleanupWorker
	var stockWorker *stockinventory.LeaderWorker
	if cfg.ScheduleWorker.Enabled {
		scheduleWorker = newRemoteScheduleWorker(cfg, remote, leaseClient, runtimeNamespace)
		go scheduleWorker.Run(ctx)
	}
	if cfg.SlackbotCleanupWorker.Enabled {
		cleanupWorker = newRemoteCleanupWorker(cfg, remote, leaseClient, runtimeNamespace)
		go cleanupWorker.Run(ctx)
	}
	if cfg.StockInventoryWorker.Enabled {
		stockWorker = newRemoteStockWorker(cfg, remote, leaseClient, runtimeNamespace)
		go stockWorker.Run(ctx)
	}
	startRemoteSlackSocketManager(ctx, cfg, persistence, persistenceNamespace, runtimeNamespace, remote, memoryRepo, profileRepo, leaseClient)
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

const workerDefaultSlackSecretName = "agentapi-worker-slack-default"

func configureWorkerSlackCredential(ctx context.Context, cfg *config.Config, persistence kubernetes.Interface, namespace string) error {
	if cfg.Slack.AppToken == "" && cfg.Slack.BotToken == "" {
		return nil
	}
	if cfg.Slack.AppToken == "" || cfg.Slack.BotToken == "" {
		return errors.New("worker Slack Socket Mode requires both app and bot tokens")
	}

	secrets := persistence.CoreV1().Secrets(namespace)
	secret, err := secrets.Get(ctx, workerDefaultSlackSecretName, metav1.GetOptions{})
	if apierrors.IsNotFound(err) {
		secret = &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: workerDefaultSlackSecretName}, Data: map[string][]byte{}}
		secret.Data["app-token"] = []byte(cfg.Slack.AppToken)
		secret.Data["bot-token"] = []byte(cfg.Slack.BotToken)
		if _, err = secrets.Create(ctx, secret, metav1.CreateOptions{}); err != nil {
			return fmt.Errorf("create worker Slack credential: %w", err)
		}
	} else if err != nil {
		return fmt.Errorf("get worker Slack credential: %w", err)
	} else {
		if secret.Data == nil {
			secret.Data = map[string][]byte{}
		}
		secret.Data["app-token"] = []byte(cfg.Slack.AppToken)
		secret.Data["bot-token"] = []byte(cfg.Slack.BotToken)
		if _, err = secrets.Update(ctx, secret, metav1.UpdateOptions{}); err != nil {
			return fmt.Errorf("update worker Slack credential: %w", err)
		}
	}

	cfg.Slack.AppTokenSecretName = workerDefaultSlackSecretName
	cfg.Slack.AppTokenSecretKey = "app-token"
	cfg.KubernetesSession.SlackBotTokenSecretName = workerDefaultSlackSecretName
	cfg.KubernetesSession.SlackBotTokenSecretKey = "bot-token"
	return nil
}

func startRemoteSlackSocketManager(ctx context.Context, cfg *config.Config, persistence kubernetes.Interface, persistenceNamespace, runtimeNamespace string, remote *controlapi.SessionManager, memory *repositories.KubernetesMemoryRepository, profiles *repositories.KubernetesSessionProfileRepository, leaseClient schedule.LeaseClient) {
	repo := repositories.NewKubernetesSlackBotRepository(persistence, persistenceNamespace)
	resolver := slackbot.NewSlackChannelResolver(persistence, persistenceNamespace).WithSecretClient(persistence)
	handler := slackbot.NewSlackBotEventHandler(repo, remote, cfg.KubernetesSession.SlackBotTokenSecretName, cfg.KubernetesSession.SlackBotTokenSecretKey, resolver, cfg.Webhook.BaseURL, cfg.Slack.DryRun, memory, profiles)
	appSecret := cfg.Slack.AppTokenSecretName
	if appSecret == "" {
		appSecret = cfg.KubernetesSession.SlackBotTokenSecretName
	}
	appKey := cfg.Slack.AppTokenSecretKey
	if appKey == "" {
		appKey = "app-token"
	}
	election := workerElection(cfg.ScheduleWorker.LeaseDuration, cfg.ScheduleWorker.RenewDeadline, cfg.ScheduleWorker.RetryPeriod, "", runtimeNamespace)
	manager := slackbot.NewSlackSocketManager(persistence, persistenceNamespace, repo, handler, resolver, slackbot.SlackSocketManagerConfig{DefaultAppTokenSecretName: appSecret, DefaultAppTokenSecretKey: appKey, DefaultBotTokenSecretName: cfg.KubernetesSession.SlackBotTokenSecretName, DefaultBotTokenSecretKey: cfg.KubernetesSession.SlackBotTokenSecretKey, LeaderElectionConfig: election, LeaderElectionClient: leaseClient})
	go manager.Run(ctx)
}

func newWorkerKVStore(cfg config.KVStoreConfig) (kvstore.Store, error) {
	backend := cfg.Backend
	databaseURL := cfg.DatabaseURL
	authToken := cfg.AuthToken
	encryption := cfg.Encryption
	if cfg.Primary != nil {
		backend = cfg.Primary.Backend
		databaseURL = cfg.Primary.DatabaseURL
		authToken = cfg.Primary.AuthToken
		encryption = cfg.Primary.Encryption
	}
	if backend == "" || backend == "kubernetes" {
		restConfig, err := ctrlconfig.GetConfig()
		if err != nil {
			return nil, fmt.Errorf("get Kubernetes config for worker KV store: %w", err)
		}
		client, err := kubernetes.NewForConfig(restConfig)
		if err != nil {
			return nil, fmt.Errorf("create Kubernetes client for worker KV store: %w", err)
		}
		return kvstore.NewKubernetesStore(client), nil
	}
	if backend != "libsql" && backend != "libsql-encrypted" {
		return nil, fmt.Errorf("unsupported worker kv_store backend %q", backend)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	store, err := kvstore.NewLibSQLStore(ctx, databaseURL, authToken)
	if err != nil {
		return nil, err
	}
	if backend == "libsql" {
		return store, nil
	}
	if encryption.ActiveKeyID == "" || len(encryption.Keys) == 0 {
		_ = store.Close()
		return nil, errors.New("KV encryption keys are required for libsql-encrypted")
	}
	var keyring kvstore.EnvelopeKeyring
	switch encryption.Provider {
	case "", "local":
		keyring, err = kvstore.NewLocalKeyring(encryption.ActiveKeyID, encryption.Keys)
	case "aws-kms":
		keyring, err = kvstore.NewKMSKeyring(ctx, encryption.ActiveKeyID, encryption.KMSRegion, encryption.Keys)
	case "aws-kms-branch":
		keyring, err = kvstore.NewBranchKMSKeyring(ctx, encryption.ActiveKeyID, encryption.KMSRegion, encryption.Keys, store,
			time.Duration(encryption.BranchCacheTTLSeconds)*time.Second, encryption.BranchCacheMaxEntries)
	case "cloud-kms-branch":
		keyring, err = kvstore.NewCloudBranchKMSKeyring(ctx, encryption.ActiveKeyID, encryption.Keys, store,
			time.Duration(encryption.BranchCacheTTLSeconds)*time.Second, encryption.BranchCacheMaxEntries)
	default:
		err = fmt.Errorf("unsupported KV encryption provider %q", encryption.Provider)
	}
	if err != nil {
		_ = store.Close()
		return nil, fmt.Errorf("configure worker KV encryption: %w", err)
	}
	encrypted, err := kvstore.NewEncryptedStore(store, keyring)
	if err != nil {
		_ = store.Close()
		return nil, err
	}
	return encrypted, nil
}

type remoteScheduleWorker struct {
	client        *controlapi.SessionManager
	sessionAPIURL string
	elector       *schedule.LeaderElector
	interval      time.Duration
}

func newRemoteScheduleWorker(cfg *config.Config, remote *controlapi.SessionManager, leaseClient schedule.LeaseClient, namespace string) *remoteScheduleWorker {
	interval, _ := time.ParseDuration(cfg.ScheduleWorker.CheckInterval)
	if interval <= 0 {
		interval = 30 * time.Second
	}
	election := workerElection(cfg.ScheduleWorker.LeaseDuration, cfg.ScheduleWorker.RenewDeadline, cfg.ScheduleWorker.RetryPeriod, schedule.ScheduleWorkerLeaseName, namespace)
	return &remoteScheduleWorker{client: remote, sessionAPIURL: cfg.Worker.SessionAPIURL, elector: schedule.NewLeaderElector(leaseClient, election), interval: interval}
}

func (w *remoteScheduleWorker) Run(ctx context.Context) {
	w.elector.Run(ctx, func(leaderCtx context.Context) {
		log.Printf("[SCHEDULE_WORKER] Started API polling with check interval %v", w.interval)
		w.process(leaderCtx)
		ticker := time.NewTicker(w.interval)
		defer ticker.Stop()
		for {
			select {
			case <-leaderCtx.Done():
				return
			case <-ticker.C:
				w.process(leaderCtx)
			}
		}
	}, nil)
}

func (w *remoteScheduleWorker) process(ctx context.Context) {
	telemetry.OperationVoid(ctx, "cmd.remoteScheduleWorker.process", w.processOnce)
}

func (w *remoteScheduleWorker) processOnce(ctx context.Context) {
	jobs, err := w.client.ClaimDueSchedules(ctx)
	if err != nil {
		log.Printf("[SCHEDULE_WORKER] Claim API failed: %v", err)
		return
	}
	for _, job := range jobs {
		var sessionID string
		var createErr error
		for attempt := 1; attempt <= 3; attempt++ {
			sessionID, createErr = w.client.StartScheduledSession(ctx, w.sessionAPIURL, job)
			if createErr == nil {
				break
			}
			if attempt < 3 {
				select {
				case <-ctx.Done():
					return
				case <-time.After(time.Duration(attempt) * time.Second):
				}
			}
		}
		status, message := "success", ""
		if createErr != nil {
			status, message = "failed", createErr.Error()
		}
		if err := w.client.FinalizeSchedule(ctx, job, status, sessionID, message); err != nil {
			log.Printf("[SCHEDULE_WORKER] Finalize failed for %s: %v", job.ScheduleID, err)
			continue
		}
		log.Printf("[SCHEDULE_WORKER] schedule=%s execution=%s status=%s session=%s", job.ScheduleID, job.ExecutionID, status, sessionID)
	}
}

func (w *remoteScheduleWorker) Stop() {}

func newRemoteCleanupWorker(cfg *config.Config, remote *controlapi.SessionManager, leaseClient schedule.LeaseClient, namespace string) *slackbotcleanup.LeaderCleanupWorker {
	check, _ := time.ParseDuration(cfg.SlackbotCleanupWorker.CheckInterval)
	ttl, _ := time.ParseDuration(cfg.SlackbotCleanupWorker.SessionTTL)
	ttlCheck, _ := time.ParseDuration(cfg.SlackbotCleanupWorker.SessionTTLCheckInterval)
	election := workerElection(cfg.SlackbotCleanupWorker.LeaseDuration, cfg.SlackbotCleanupWorker.RenewDeadline, cfg.SlackbotCleanupWorker.RetryPeriod, schedule.SlackbotCleanupWorkerLeaseName, namespace)
	return slackbotcleanup.NewLeaderCleanupWorker(remote, leaseClient, slackbotcleanup.CleanupWorkerConfig{CheckInterval: check, SessionTTL: ttl, SessionTTLCheckInterval: ttlCheck, Enabled: true, DryRun: cfg.SlackbotCleanupWorker.DryRun}, election)
}

func newRemoteStockWorker(cfg *config.Config, remote *controlapi.SessionManager, leaseClient schedule.LeaseClient, namespace string) *stockinventory.LeaderWorker {
	interval, _ := time.ParseDuration(cfg.StockInventoryWorker.CheckInterval)
	if interval <= 0 {
		interval = 30 * time.Second
	}
	election := workerElection(cfg.StockInventoryWorker.LeaseDuration, cfg.StockInventoryWorker.RenewDeadline, cfg.StockInventoryWorker.RetryPeriod, schedule.StockInventoryWorkerLeaseName, namespace)
	return stockinventory.NewLeaderWorker(remote, leaseClient, stockinventory.WorkerConfig{CheckInterval: interval, TargetCount: cfg.StockInventoryWorker.TargetCount, Requirements: stockinventory.StockRequirements{DinD: cfg.StockInventoryWorker.DockerEnabled}, Pools: buildStockInventoryPools(cfg.StockInventoryWorker, cfg.StockInventoryWorker.TargetCount), Enabled: true}, election)
}

func workerElection(lease, renew, retry, name, namespace string) schedule.LeaderElectionConfig {
	l, _ := time.ParseDuration(lease)
	r, _ := time.ParseDuration(renew)
	p, _ := time.ParseDuration(retry)
	if l <= 0 {
		l = 9 * time.Minute
	}
	if r <= 0 {
		r = 6 * time.Minute
	}
	if p <= 0 {
		p = time.Minute
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
