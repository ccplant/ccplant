package cmd

import (
	"context"
	"crypto/tls"
	"log"
	"strings"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/takutakahashi/agentapi-proxy/internal/app"
	"github.com/takutakahashi/agentapi-proxy/internal/infrastructure/repositories"
	mcpiface "github.com/takutakahashi/agentapi-proxy/internal/interfaces/mcp"
	"github.com/takutakahashi/agentapi-proxy/internal/modules/schedule"
	"github.com/takutakahashi/agentapi-proxy/internal/modules/slackbot"
	"github.com/takutakahashi/agentapi-proxy/internal/modules/webhook"
	"github.com/takutakahashi/agentapi-proxy/pkg/config"
	stock_inventory "github.com/takutakahashi/agentapi-proxy/pkg/stock_inventory"
	"k8s.io/client-go/kubernetes"
	ctrl "sigs.k8s.io/controller-runtime"
)

func startStockInventoryWorker(configData *config.Config, proxyServer *app.Server) *stock_inventory.LeaderWorker {
	log.Printf("[STOCK_INVENTORY] Initializing stock inventory worker...")

	redisClient, err := newWorkerRedisClient(configData)
	if err != nil {
		log.Printf("[STOCK_INVENTORY] %v", err)
		return nil
	}

	namespace := resolveKubernetesNamespace(configData.StockInventoryWorker.Namespace, configData.KubernetesSession.Namespace)

	// KubernetesSessionManager implements StockRepository.
	stockRepo, ok := proxyServer.GetSessionManager().(stock_inventory.StockRepository)
	if !ok {
		log.Printf("[STOCK_INVENTORY] Session manager does not implement StockRepository, stock inventory worker disabled")
		return nil
	}

	checkInterval, err := time.ParseDuration(configData.StockInventoryWorker.CheckInterval)
	if err != nil {
		log.Printf("[STOCK_INVENTORY] Invalid check_interval, using default 30s: %v", err)
		checkInterval = 30 * time.Second
	}

	targetCount := configData.StockInventoryWorker.TargetCount
	if targetCount <= 0 {
		targetCount = 2
	}
	pools := buildStockInventoryPools(configData.StockInventoryWorker, targetCount)

	// Note: Sandbox (network filter) is always enabled - the SandboxEnabled config is ignored.
	workerConfig := stock_inventory.WorkerConfig{
		CheckInterval: checkInterval,
		TargetCount:   targetCount,
		Requirements: stock_inventory.StockRequirements{
			DinD: configData.StockInventoryWorker.DockerEnabled,
		},
		Pools:   pools,
		Enabled: true,
	}

	leaseDuration, err := time.ParseDuration(configData.StockInventoryWorker.LeaseDuration)
	if err != nil {
		leaseDuration = 9 * time.Minute
	}
	renewDeadline, err := time.ParseDuration(configData.StockInventoryWorker.RenewDeadline)
	if err != nil {
		renewDeadline = 6 * time.Minute
	}
	retryPeriod, err := time.ParseDuration(configData.StockInventoryWorker.RetryPeriod)
	if err != nil {
		retryPeriod = time.Minute
	}

	electionConfig := schedule.LeaderElectionConfig{
		LeaseDuration: leaseDuration,
		RenewDeadline: renewDeadline,
		RetryPeriod:   retryPeriod,
		LeaseName:     schedule.StockInventoryWorkerLeaseName,
		Namespace:     namespace,
	}

	leaderWorker := stock_inventory.NewLeaderWorker(stockRepo, schedule.NewRedisLeaseClient(redisClient), workerConfig, electionConfig)

	go leaderWorker.Run(context.Background())

	poolCount := len(pools)
	if poolCount == 0 {
		poolCount = 1
	}
	log.Printf("[STOCK_INVENTORY] Stock inventory worker started in namespace: %s (pools: %d)",
		namespace, poolCount)
	return leaderWorker
}

func buildStockInventoryPools(workerConfig config.StockInventoryWorkerConfig, defaultTargetCount int) []stock_inventory.StockPool {
	if len(workerConfig.Pools) == 0 {
		return nil
	}

	pools := make([]stock_inventory.StockPool, 0, len(workerConfig.Pools))
	for _, poolConfig := range workerConfig.Pools {
		targetCount := poolConfig.TargetCount
		if targetCount < 0 {
			targetCount = defaultTargetCount
		}
		// Note: Sandbox (network filter) is always enabled - SandboxEnabled is ignored.
		pools = append(pools, stock_inventory.StockPool{
			Name:        poolConfig.Name,
			TargetCount: targetCount,
			Requirements: stock_inventory.StockRequirements{
				DinD: poolConfig.DockerEnabled,
			},
		})
	}
	return pools
}

// registerWebhookHandlers registers webhook REST API handlers
func registerWebhookHandlers(configData *config.Config, proxyServer *app.Server) {
	log.Printf("[WEBHOOK_HANDLERS] Registering webhook handlers...")

	// Webhooks are persisted through the application KV store, so their logical
	// namespace must not depend on a mounted Kubernetes ServiceAccount token.
	namespace := resolvePersistenceNamespace(configData)

	// Create webhook repository (clean architecture)
	webhookRepo := repositories.NewKubernetesWebhookRepository(proxyServer.GetPersistenceClient(), namespace)

	// Set default GitHub Enterprise host if configured
	if configData.Webhook.GitHubEnterpriseHost != "" {
		webhookRepo.SetDefaultGitHubEnterpriseHost(configData.Webhook.GitHubEnterpriseHost)
		log.Printf("[WEBHOOK_HANDLERS] Default GitHub Enterprise host configured: %s", configData.Webhook.GitHubEnterpriseHost)
	}

	// Create and register webhook handlers with baseURL from config
	webhookHandlers := webhook.NewHandlers(webhookRepo, newTriggerSessionManager(configData), configData.Webhook.BaseURL, proxyServer.GetSessionProfileRepository()).
		WithSettingsRepository(proxyServer.GetSettingsRepository()).
		WithGitHubApplicationUserResolver(proxyServer)
	proxyServer.AddCustomHandler(webhookHandlers)

	if configData.Webhook.BaseURL != "" {
		log.Printf("[WEBHOOK_HANDLERS] Webhook base URL configured: %s", configData.Webhook.BaseURL)
	} else {
		log.Printf("[WEBHOOK_HANDLERS] Webhook base URL not configured, will auto-detect from request headers")
	}
	log.Printf("[WEBHOOK_HANDLERS] Webhook handlers registered successfully")
}

func newWorkerRedisClient(configData *config.Config) (redis.UniversalClient, error) {
	if strings.TrimSpace(configData.Redis.Addr) == "" {
		return nil, schedule.ErrRedisRequired
	}
	opts := &redis.Options{Addr: configData.Redis.Addr, Password: configData.Redis.Password, DB: configData.Redis.DB}
	if d, err := time.ParseDuration(configData.Redis.DialTimeout); err == nil && d > 0 {
		opts.DialTimeout = d
	}
	if d, err := time.ParseDuration(configData.Redis.ReadTimeout); err == nil && d > 0 {
		opts.ReadTimeout = d
	}
	if d, err := time.ParseDuration(configData.Redis.WriteTimeout); err == nil && d > 0 {
		opts.WriteTimeout = d
	}
	if configData.Redis.TLSEnabled {
		opts.TLSConfig = &tls.Config{MinVersion: tls.VersionTLS12}
	}
	client := redis.NewClient(opts)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := client.Ping(ctx).Err(); err != nil {
		_ = client.Close()
		return nil, err
	}
	return client, nil
}

// registerSlackBotHandlers registers SlackBot management REST API handlers
func registerSlackBotHandlers(configData *config.Config, proxyServer *app.Server) {
	log.Printf("[SLACKBOT_HANDLERS] Registering slackbot handlers...")

	// SlackBots are persisted through the application KV store, so their logical
	// namespace must not depend on a mounted Kubernetes ServiceAccount token.
	namespace := resolvePersistenceNamespace(configData)

	// Create SlackBot repository
	slackbotRepo := repositories.NewKubernetesSlackBotRepository(proxyServer.GetPersistenceClient(), namespace)

	// Create and register SlackBot management handlers (no event reception - handled by Socket Mode)
	slackbotHandlers := slackbot.NewHandlers(slackbotRepo, newTriggerSessionManager(configData), proxyServer.GetSessionProfileRepository(), proxyServer.GetSettingsRepository())
	proxyServer.AddCustomHandler(slackbotHandlers)

	log.Printf("[SLACKBOT_HANDLERS] SlackBot management handlers registered successfully")
}

// startSlackSocketManager starts the Slack Socket Mode manager with per-bot leader election
func startSlackSocketManager(configData *config.Config, proxyServer *app.Server) {
	log.Printf("[SOCKET_MANAGER] Initializing Slack Socket Mode manager...")

	// Create Kubernetes client
	restConfig, err := ctrl.GetConfig()
	if err != nil {
		log.Printf("[SOCKET_MANAGER] Kubernetes config not available, skipping Socket Mode: %v", err)
		return
	}

	client, err := kubernetes.NewForConfig(restConfig)
	if err != nil {
		log.Printf("[SOCKET_MANAGER] Failed to create Kubernetes client, skipping Socket Mode: %v", err)
		return
	}

	// Keep application records in the KV-store namespace. The runtime namespace
	// remains responsible for Kubernetes leases and operational Secrets.
	persistenceNamespace := resolvePersistenceNamespace(configData)
	runtimeNamespace := resolveKubernetesNamespace(configData.ScheduleWorker.Namespace, configData.KubernetesSession.Namespace)

	// Create dependencies
	slackbotRepo := repositories.NewKubernetesSlackBotRepository(proxyServer.GetPersistenceClient(), persistenceNamespace)
	channelResolver := slackbot.NewSlackChannelResolver(proxyServer.GetPersistenceClient(), persistenceNamespace).WithSecretClient(client).WithSettingsRepository(proxyServer.GetSettingsRepository())

	eventHandler := slackbot.NewSlackBotEventHandler(
		slackbotRepo,
		newTriggerSessionManager(configData),
		configData.KubernetesSession.SlackBotTokenSecretName,
		configData.KubernetesSession.SlackBotTokenSecretKey,
		channelResolver,
		configData.Webhook.BaseURL,
		configData.Slack.DryRun,
		proxyServer.GetSessionProfileRepository(),
	).WithSettingsRepository(proxyServer.GetSettingsRepository())
	if configData.Slack.DryRun {
		log.Printf("[SOCKET_MANAGER] Slack dry-run mode enabled: session creation and Slack posts will be logged only")
	}

	// Resolve App token secret (defaults to SlackBotTokenSecretName if not set)
	appTokenSecretName := configData.Slack.AppTokenSecretName
	if appTokenSecretName == "" {
		appTokenSecretName = configData.KubernetesSession.SlackBotTokenSecretName
	}
	appTokenSecretKey := configData.Slack.AppTokenSecretKey
	if appTokenSecretKey == "" {
		appTokenSecretKey = "app-token"
	}

	// Parse leader election config durations
	leaseDuration, err := time.ParseDuration(configData.ScheduleWorker.LeaseDuration)
	if err != nil {
		leaseDuration = 9 * time.Minute
	}
	renewDeadline, err := time.ParseDuration(configData.ScheduleWorker.RenewDeadline)
	if err != nil {
		renewDeadline = 6 * time.Minute
	}
	retryPeriod, err := time.ParseDuration(configData.ScheduleWorker.RetryPeriod)
	if err != nil {
		retryPeriod = time.Minute
	}

	electionConfig := schedule.LeaderElectionConfig{
		LeaseDuration: leaseDuration,
		RenewDeadline: renewDeadline,
		RetryPeriod:   retryPeriod,
		Namespace:     runtimeNamespace,
	}

	managerConfig := slackbot.SlackSocketManagerConfig{
		DefaultAppTokenSecretName: appTokenSecretName,
		DefaultAppTokenSecretKey:  appTokenSecretKey,
		DefaultBotTokenSecretName: configData.KubernetesSession.SlackBotTokenSecretName,
		DefaultBotTokenSecretKey:  configData.KubernetesSession.SlackBotTokenSecretKey,
		LeaderElectionConfig:      electionConfig,
	}
	redisClient, err := newWorkerRedisClient(configData)
	if err != nil {
		log.Printf("[SOCKET_MANAGER] %v", err)
		return
	}
	managerConfig.RedisClient = redisClient

	manager := slackbot.NewSlackSocketManager(
		client,
		persistenceNamespace,
		slackbotRepo,
		eventHandler,
		channelResolver,
		managerConfig,
	)

	go manager.Run(context.Background())

	log.Printf("[SOCKET_MANAGER] Slack Socket Mode manager started with persistence namespace %s and runtime namespace %s", persistenceNamespace, runtimeNamespace)
}

// registerMCPHandler registers MCP HTTP handler
func registerMCPHandler(proxyServer *app.Server, port string) {
	log.Printf("[MCP_HANDLER] Registering MCP handler...")

	// Create and register MCP handler with server dependencies
	mcpHandler := mcpiface.NewMCPHandler(proxyServer)
	proxyServer.AddCustomHandler(mcpHandler)

	log.Printf("[MCP_HANDLER] MCP handler registered successfully at /mcp")
}
