package app

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"log"
	"os"
	"strings"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/takutakahashi/agentapi-proxy/internal/core/esmcontrol"
	"github.com/takutakahashi/agentapi-proxy/internal/core/sessioncontrol"
	sessionrunnercore "github.com/takutakahashi/agentapi-proxy/internal/core/sessionrunner"
	infraesmcontrol "github.com/takutakahashi/agentapi-proxy/internal/infrastructure/esmcontrol"
	"github.com/takutakahashi/agentapi-proxy/internal/infrastructure/kvstore"
	infrasessioncontrol "github.com/takutakahashi/agentapi-proxy/internal/infrastructure/sessioncontrol"
	infrasessionrunner "github.com/takutakahashi/agentapi-proxy/internal/infrastructure/sessionrunner"
	portrepos "github.com/takutakahashi/agentapi-proxy/internal/usecases/ports/repositories"
	"github.com/takutakahashi/agentapi-proxy/pkg/config"
	"k8s.io/client-go/kubernetes"
)

const (
	defaultSessionControlPoolSize         = 50
	defaultSessionControlBlockingPoolSize = 400
)

func buildSessionControlStore(cfg *config.Config) sessioncontrol.Store {
	commandPoolSize := cfg.Redis.SessionControlPoolSize
	if commandPoolSize <= 0 {
		commandPoolSize = defaultSessionControlPoolSize
	}
	blockingPoolSize := cfg.Redis.SessionControlBlockingPoolSize
	if blockingPoolSize <= 0 {
		blockingPoolSize = defaultSessionControlBlockingPoolSize
	}
	opts := &redis.Options{
		Addr:           cfg.Redis.Addr,
		Password:       cfg.Redis.Password,
		DB:             cfg.Redis.DB,
		PoolSize:       commandPoolSize,
		MaxActiveConns: commandPoolSize,
	}
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
		log.Printf("[SESSION_CONTROL] Disabled: Redis ping failed: %v", err)
		closeRedisClient("session control", client)
		return nil
	}
	log.Printf("[SESSION_CONTROL] Redis Streams control channel enabled (command_pool_size=%d, blocking_pool_size=%d)",
		commandPoolSize, blockingPoolSize)
	blockingOpts := *opts
	blockingOpts.ReadTimeout = 35 * time.Second
	blockingOpts.PoolSize = blockingPoolSize
	blockingOpts.MaxActiveConns = blockingPoolSize
	blockingClient := redis.NewClient(&blockingOpts)
	return infrasessioncontrol.NewRedisStoreWithBlockingClient(client, blockingClient)
}

func buildSessionAllocationNotifier(cfg *config.Config) (sessionrunnercore.AllocationNotifier, *redis.Client) {
	local := infrasessionrunner.NewLocalAllocationNotifier()
	if cfg.Redis.Addr == "" {
		log.Printf("[SESSION_POOL] Redis not configured; runner allocation notifications are process-local")
		return local, nil
	}
	opts := &redis.Options{Addr: cfg.Redis.Addr, Password: cfg.Redis.Password, DB: cfg.Redis.DB, ReadTimeout: 35 * time.Second}
	if d, err := time.ParseDuration(cfg.Redis.DialTimeout); err == nil && d > 0 {
		opts.DialTimeout = d
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
		log.Printf("[SESSION_POOL] Redis unavailable; runner allocation notifications are process-local: %v", err)
		closeRedisClient("session allocation notifier", client)
		return local, nil
	}
	log.Printf("[SESSION_POOL] Redis runner allocation notifications enabled")
	return infrasessionrunner.NewRedisAllocationNotifier(client), client
}

func buildESMControlStore(cfg *config.Config) esmcontrol.Store {
	opts := &redis.Options{Addr: cfg.Redis.Addr, Password: cfg.Redis.Password, DB: cfg.Redis.DB, ReadTimeout: 35 * time.Second}
	if d, err := time.ParseDuration(cfg.Redis.DialTimeout); err == nil && d > 0 {
		opts.DialTimeout = d
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
		log.Printf("[ESM_CONTROL] Disabled: Redis ping failed: %v", err)
		closeRedisClient("ESM control", client)
		return nil
	}
	log.Printf("[ESM_CONTROL] Outbound manager tunnel enabled")
	return infraesmcontrol.NewRedisStore(client)
}

func closeRedisClient(subject string, client *redis.Client) {
	if err := client.Close(); err != nil {
		log.Printf("[%s] Redis client close failed: %v", subject, err)
	}
}

func buildApplicationKVStore(cfg config.KVStoreConfig, kubeClient kubernetes.Interface) (kvstore.Store, bool, error) {
	if cfg.Primary == nil && cfg.Secondary == nil {
		return buildLegacyApplicationKVStore(cfg, kubeClient)
	}
	if cfg.Primary == nil {
		return nil, false, errors.New("kv_store.primary is required when secondary is configured")
	}
	if cfg.Backend != "" || cfg.DatabaseURL != "" || cfg.AuthToken != "" {
		return nil, false, errors.New("legacy kv_store fields cannot be combined with primary/secondary")
	}
	primary, err := buildKVBackend(*cfg.Primary, kubeClient)
	if err != nil {
		return nil, false, fmt.Errorf("primary: %w", err)
	}
	if cfg.Secondary == nil {
		if cfg.Primary.Backend == "kubernetes" || cfg.Primary.Backend == "" {
			return nil, false, nil
		}
		return primary, true, nil
	}
	secondary, err := buildKVBackend(*cfg.Secondary, kubeClient)
	if err != nil {
		return nil, false, errors.Join(fmt.Errorf("secondary: %w", err), primary.Close())
	}
	mode := cfg.Replication.Mode
	if mode == "" {
		mode = string(kvstore.ReplicationModeRollback)
	}
	replicated, err := kvstore.NewReplicatedStore(primary, secondary, kvstore.ReplicationMode(mode))
	if err != nil {
		return nil, false, errors.Join(err, primary.Close(), secondary.Close())
	}
	return replicated, true, nil
}

func buildLegacyApplicationKVStore(cfg config.KVStoreConfig, kubeClient kubernetes.Interface) (kvstore.Store, bool, error) {
	if cfg.Backend == "" || cfg.Backend == "kubernetes" {
		return nil, false, nil
	}
	store, err := buildKVBackend(config.KVStoreBackendConfig{Backend: cfg.Backend, DatabaseURL: cfg.DatabaseURL, AuthToken: cfg.AuthToken, Encryption: cfg.Encryption}, kubeClient)
	return store, err == nil, err
}

func resolveApplicationNamespace(configured string) string {
	// Explicit runtime configuration must win over stale config snapshots.
	// Non-Kubernetes API runtimes have no service-account namespace fallback,
	// so honoring this override prevents application data from silently landing
	// in the "default" logical namespace.
	if namespace := strings.TrimSpace(os.Getenv("AGENTAPI_KV_STORE_NAMESPACE")); namespace != "" {
		return namespace
	}
	if namespace := strings.TrimSpace(configured); namespace != "" {
		return namespace
	}
	if namespace := strings.TrimSpace(os.Getenv("POD_NAMESPACE")); namespace != "" {
		return namespace
	}
	return "default"
}

func validateAPIKVStore(cfg config.KVStoreConfig) error {
	backend := configuredKVBackend(cfg)
	if backend != "libsql" && backend != "libsql-encrypted" && backend != "kubernetes" {
		return fmt.Errorf("primary backend must be libsql, libsql-encrypted, or kubernetes, got %q", backend)
	}
	if cfg.Secondary != nil && cfg.Secondary.Backend != "libsql" && cfg.Secondary.Backend != "libsql-encrypted" && cfg.Secondary.Backend != "kubernetes" {
		return fmt.Errorf("secondary backend must be libsql, libsql-encrypted, or kubernetes in the API role, got %q", cfg.Secondary.Backend)
	}
	return nil
}

func configuredKVBackend(cfg config.KVStoreConfig) string {
	backend := cfg.Backend
	if cfg.Primary != nil {
		backend = cfg.Primary.Backend
	}
	if backend == "" {
		return "kubernetes"
	}
	return backend
}

func buildKVBackend(cfg config.KVStoreBackendConfig, kubeClient kubernetes.Interface) (kvstore.Store, error) {
	switch cfg.Backend {
	case "", "kubernetes":
		return kvstore.NewKubernetesStore(kubeClient), nil
	case "libsql", "libsql-encrypted":
		if strings.TrimSpace(cfg.DatabaseURL) == "" {
			return nil, errors.New("database_url is required for libSQL")
		}
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		store, err := kvstore.NewLibSQLStore(ctx, cfg.DatabaseURL, cfg.AuthToken)
		if err != nil {
			return nil, err
		}
		if cfg.Backend == "libsql" {
			return store, nil
		}
		cfg.Encryption.Keys = cfg.Encryption.EffectiveKeys()
		if cfg.Encryption.ActiveKeyID == "" || len(cfg.Encryption.Keys) == 0 {
			return nil, errors.Join(errors.New("KV encryption keys are required for libsql-encrypted"), store.Close())
		}
		keyring, err := buildKVEncryptionKeyring(ctx, cfg.Encryption, store)
		if err != nil {
			return nil, errors.Join(fmt.Errorf("configure KV encryption: %w", err), store.Close())
		}
		encrypted, err := kvstore.NewEncryptedStore(store, keyring)
		if err != nil {
			return nil, errors.Join(err, store.Close())
		}
		return encrypted, nil
	default:
		return nil, fmt.Errorf("unsupported backend %q", cfg.Backend)
	}
}

func buildKVEncryptionKeyring(ctx context.Context, encryption config.KVStoreEncryptionConfig, registry kvstore.BranchKeyRegistry) (kvstore.EnvelopeKeyring, error) {
	switch encryption.Provider {
	case "", "local":
		return kvstore.NewLocalKeyring(encryption.ActiveKeyID, encryption.Keys)
	case "aws-kms":
		return kvstore.NewKMSKeyring(ctx, encryption.ActiveKeyID, encryption.KMSRegion, encryption.Keys)
	case "aws-kms-branch":
		return kvstore.NewBranchKMSKeyring(ctx, encryption.ActiveKeyID, encryption.KMSRegion, encryption.Keys, registry,
			time.Duration(encryption.BranchCacheTTLSeconds)*time.Second, encryption.BranchCacheMaxEntries)
	case "aws-kms-branch-scoped":
		return kvstore.NewScopedBranchKMSKeyring(ctx, encryption.ActiveKeyID, encryption.KMSRegion, encryption.Keys, registry,
			time.Duration(encryption.BranchCacheTTLSeconds)*time.Second, encryption.BranchCacheMaxEntries)
	case "cloud-kms-branch":
		return kvstore.NewCloudBranchKMSKeyring(ctx, encryption.ActiveKeyID, encryption.Keys, registry,
			time.Duration(encryption.BranchCacheTTLSeconds)*time.Second, encryption.BranchCacheMaxEntries)
	case "cloud-kms-branch-scoped":
		return kvstore.NewScopedCloudBranchKMSKeyring(ctx, encryption.ActiveKeyID, encryption.Keys, registry,
			time.Duration(encryption.BranchCacheTTLSeconds)*time.Second, encryption.BranchCacheMaxEntries)
	default:
		return nil, fmt.Errorf("unsupported KV encryption provider %q", encryption.Provider)
	}
}

func cleanupLocalSessionRoutes(ctx context.Context, repo portrepos.SessionRouteRepository, runtimeSessionID string) {
	routes, err := repo.List(ctx, "")
	if err != nil {
		log.Printf("[SESSION_ROUTE] Warning: failed to list routes while deleting runtime session %s: %v", runtimeSessionID, err)
		return
	}
	for _, route := range routes {
		if route.ManagerID != "" || route.RemoteSessionID != runtimeSessionID {
			continue
		}
		if err := repo.Delete(ctx, route.SessionID); err != nil {
			log.Printf("[SESSION_ROUTE] Warning: failed to delete local alias %s for runtime session %s: %v", route.SessionID, runtimeSessionID, err)
		}
	}
}

// StartMonitoring starts the session monitoring (called after server is fully initialized)
