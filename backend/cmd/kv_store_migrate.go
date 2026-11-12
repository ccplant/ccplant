package cmd

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"github.com/takutakahashi/agentapi-proxy/internal/infrastructure/kvstore"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/client-go/kubernetes"
	ctrl "sigs.k8s.io/controller-runtime"
)

type kvStoreMigrateOptions struct {
	namespace             string
	destinationNamespace  string
	primaryBackend        string
	primaryDatabaseURL    string
	primaryAuthToken      string
	secondaryBackend      string
	secondaryDatabaseURL  string
	secondaryAuthToken    string
	legacyDatabaseURL     string
	legacyAuthToken       string
	encryptionActiveKeyID string
	encryptionKeysJSON    string
	encryptionProvider    string
	encryptionKMSRegion   string
	dryRun                bool
	overwrite             bool
	output                string
}

type kvStoreMigrationEntry struct {
	Kind      kvstore.Kind `json:"kind"`
	Namespace string       `json:"namespace"`
	Key       string       `json:"key"`
	Status    string       `json:"status"`
}

type kvStoreMigrationResult struct {
	DryRun    bool                    `json:"dry_run"`
	Selected  int                     `json:"selected"`
	Copied    int                     `json:"copied"`
	Updated   int                     `json:"updated"`
	Skipped   int                     `json:"skipped"`
	Conflicts int                     `json:"conflicts"`
	Entries   []kvStoreMigrationEntry `json:"entries"`
}

func newKVStoreMigrateCommand() *cobra.Command {
	o := &kvStoreMigrateOptions{}
	command := &cobra.Command{
		Use:   "migrate",
		Short: "Migrate application KV data from the primary store to the secondary store",
		Long: `Copy application-owned Secret and ConfigMap documents from the configured primary store to the secondary store.

The command selects only resource families used by the application KV boundary;
operational resources such as Pod-mounted Secrets and Helm ConfigMaps are not
copied. Source objects are never modified or deleted.

Run the application with primary/secondary replication enabled before starting
an online migration. Runtime mutations and this command coordinate through
per-record destination locks, and the source is re-read while each lock is held.
Re-running is safe: identical destination records are skipped. A different
existing destination record is reported as a conflict unless --overwrite is
specified. Use --dry-run to inspect destination conflicts without writing
anything.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if o.output != "text" && o.output != "json" {
				return fmt.Errorf("unsupported output %q (must be text or json)", o.output)
			}
			primaryConfig, secondaryConfig, err := o.storeConfigs()
			if err != nil {
				return err
			}
			primary, secondary, err := buildMigrationStores(cmd.Context(), primaryConfig, secondaryConfig)
			if err != nil {
				return err
			}
			wrappedSecondary, err := encryptedMigrationDestination(cmd.Context(), secondary, o.secondaryBackend == "libsql-encrypted", o.encryptionProvider, o.encryptionActiveKeyID, o.encryptionKMSRegion, o.encryptionKeysJSON)
			if err != nil {
				_ = errors.Join(primary.Close(), secondary.Close())
				return fmt.Errorf("configure secondary encryption: %w", err)
			}
			secondary = wrappedSecondary
			defer func() { _ = errors.Join(primary.Close(), secondary.Close()) }()

			result, migrateErr := migrateKVStores(cmd.Context(), primary, secondary, *o)
			if err := writeKVStoreMigrationResult(cmd.OutOrStdout(), result, o.output); err != nil {
				return err
			}
			return migrateErr
		},
	}
	flags := command.Flags()
	flags.StringVarP(&o.namespace, "namespace", "n", resolveKubernetesNamespace(), "Kubernetes namespace containing application KV data")
	flags.StringVar(&o.destinationNamespace, "destination-namespace", "", "rewrite copied records into this destination namespace")
	flags.StringVar(&o.primaryBackend, "primary-backend", os.Getenv("AGENTAPI_KV_STORE_PRIMARY_BACKEND"), "primary backend (kubernetes or libsql)")
	flags.StringVar(&o.primaryDatabaseURL, "primary-database-url", os.Getenv("AGENTAPI_KV_STORE_PRIMARY_DATABASE_URL"), "primary libSQL database URL")
	flags.StringVar(&o.primaryAuthToken, "primary-auth-token", os.Getenv("AGENTAPI_KV_STORE_PRIMARY_AUTH_TOKEN"), "primary libSQL authentication token")
	flags.StringVar(&o.secondaryBackend, "secondary-backend", os.Getenv("AGENTAPI_KV_STORE_SECONDARY_BACKEND"), "secondary backend (kubernetes or libsql)")
	flags.StringVar(&o.secondaryDatabaseURL, "secondary-database-url", os.Getenv("AGENTAPI_KV_STORE_SECONDARY_DATABASE_URL"), "secondary libSQL database URL")
	flags.StringVar(&o.secondaryAuthToken, "secondary-auth-token", os.Getenv("AGENTAPI_KV_STORE_SECONDARY_AUTH_TOKEN"), "secondary libSQL authentication token")
	flags.StringVar(&o.legacyDatabaseURL, "database-url", os.Getenv("AGENTAPI_KV_STORE_DATABASE_URL"), "deprecated: destination libSQL database URL")
	flags.StringVar(&o.legacyAuthToken, "auth-token", os.Getenv("AGENTAPI_KV_STORE_AUTH_TOKEN"), "deprecated: destination libSQL authentication token")
	flags.StringVar(&o.encryptionActiveKeyID, "encryption-active-key-id", os.Getenv("AGENTAPI_KV_STORE_SECONDARY_ENCRYPTION_ACTIVE_KEY_ID"), "active key ID used to encrypt destination values")
	flags.StringVar(&o.encryptionKeysJSON, "encryption-keys-json", os.Getenv("AGENTAPI_KV_STORE_SECONDARY_ENCRYPTION_KEYS"), "JSON object mapping destination key IDs to local keys or KMS key references")
	flags.StringVar(&o.encryptionProvider, "encryption-provider", os.Getenv("AGENTAPI_KV_STORE_SECONDARY_ENCRYPTION_PROVIDER"), "destination encryption provider (local, aws-kms, aws-kms-branch, aws-kms-branch-scoped, cloud-kms-branch, or cloud-kms-branch-scoped)")
	flags.StringVar(&o.encryptionKMSRegion, "encryption-kms-region", os.Getenv("AGENTAPI_KV_STORE_SECONDARY_ENCRYPTION_KMS_REGION"), "AWS region for a destination AWS KMS provider")
	flags.BoolVar(&o.dryRun, "dry-run", false, "inspect records and conflicts without writing to the secondary store")
	flags.BoolVar(&o.overwrite, "overwrite", false, "replace different records that already exist in the secondary store")
	flags.StringVarP(&o.output, "output", "o", "text", "output format: text or json")
	return command
}

func encryptedMigrationDestination(ctx context.Context, store kvstore.Store, encryptionRequired bool, provider, activeKeyID, kmsRegion, keysJSON string) (kvstore.Store, error) {
	if activeKeyID == "" && strings.TrimSpace(keysJSON) == "" {
		if encryptionRequired {
			return nil, errors.New("libsql-encrypted destination requires an encryption active key ID and encryption keys JSON")
		}
		return store, nil
	}
	if activeKeyID == "" || strings.TrimSpace(keysJSON) == "" {
		return nil, errors.New("both encryption active key ID and encryption keys JSON are required")
	}
	var keys map[string]string
	if err := json.Unmarshal([]byte(keysJSON), &keys); err != nil {
		return nil, fmt.Errorf("decode encryption keys JSON: %w", err)
	}
	var keyring kvstore.EnvelopeKeyring
	var err error
	switch provider {
	case "", "local":
		keyring, err = kvstore.NewLocalKeyring(activeKeyID, keys)
	case "aws-kms":
		keyring, err = kvstore.NewKMSKeyring(ctx, activeKeyID, kmsRegion, keys)
	case "aws-kms-branch", "aws-kms-branch-scoped", "cloud-kms-branch", "cloud-kms-branch-scoped":
		registry, ok := store.(kvstore.BranchKeyRegistry)
		if !ok {
			return nil, fmt.Errorf("encryption provider %q requires a branch key registry", provider)
		}
		if provider == "aws-kms-branch" {
			keyring, err = kvstore.NewBranchKMSKeyring(ctx, activeKeyID, kmsRegion, keys, registry, 15*time.Minute, 128)
		} else if provider == "aws-kms-branch-scoped" {
			keyring, err = kvstore.NewScopedBranchKMSKeyring(ctx, activeKeyID, kmsRegion, keys, registry, 15*time.Minute, 128)
		} else if provider == "cloud-kms-branch" {
			keyring, err = kvstore.NewCloudBranchKMSKeyring(ctx, activeKeyID, keys, registry, 15*time.Minute, 128)
		} else {
			keyring, err = kvstore.NewScopedCloudBranchKMSKeyring(ctx, activeKeyID, keys, registry, 15*time.Minute, 128)
		}
	default:
		return nil, fmt.Errorf("unsupported KV encryption provider %q", provider)
	}
	if err != nil {
		return nil, err
	}
	return kvstore.NewEncryptedStore(store, keyring)
}

type migrationStoreConfig struct {
	backend, databaseURL, authToken string
}

func (o kvStoreMigrateOptions) storeConfigs() (migrationStoreConfig, migrationStoreConfig, error) {
	if o.primaryBackend == "" && o.secondaryBackend == "" && o.legacyDatabaseURL != "" {
		return migrationStoreConfig{backend: "kubernetes"}, migrationStoreConfig{backend: "libsql", databaseURL: o.legacyDatabaseURL, authToken: o.legacyAuthToken}, nil
	}
	if o.primaryBackend == "" || o.secondaryBackend == "" {
		return migrationStoreConfig{}, migrationStoreConfig{}, errors.New("primary and secondary backends are required (configure AGENTAPI_KV_STORE_PRIMARY_BACKEND and AGENTAPI_KV_STORE_SECONDARY_BACKEND)")
	}
	return migrationStoreConfig{backend: o.primaryBackend, databaseURL: o.primaryDatabaseURL, authToken: o.primaryAuthToken}, migrationStoreConfig{backend: o.secondaryBackend, databaseURL: o.secondaryDatabaseURL, authToken: o.secondaryAuthToken}, nil
}

func buildMigrationStores(ctx context.Context, primaryConfig, secondaryConfig migrationStoreConfig) (kvstore.Store, kvstore.Store, error) {
	var client kubernetes.Interface
	if primaryConfig.backend == "kubernetes" || secondaryConfig.backend == "kubernetes" {
		config, err := ctrl.GetConfig()
		if err != nil {
			return nil, nil, fmt.Errorf("get Kubernetes config: %w", err)
		}
		client, err = kubernetes.NewForConfig(config)
		if err != nil {
			return nil, nil, fmt.Errorf("create Kubernetes client: %w", err)
		}
	}
	primary, err := buildMigrationStore(ctx, primaryConfig, client)
	if err != nil {
		return nil, nil, fmt.Errorf("initialize primary store: %w", err)
	}
	secondary, err := buildMigrationStore(ctx, secondaryConfig, client)
	if err != nil {
		_ = primary.Close()
		return nil, nil, fmt.Errorf("initialize secondary store: %w", err)
	}
	return primary, secondary, nil
}

func buildMigrationStore(ctx context.Context, config migrationStoreConfig, client kubernetes.Interface) (kvstore.Store, error) {
	switch config.backend {
	case "kubernetes":
		return kvstore.NewKubernetesStore(client), nil
	case "libsql", "libsql-encrypted":
		if strings.TrimSpace(config.databaseURL) == "" {
			return nil, errors.New("database URL is required for libsql")
		}
		return kvstore.NewLibSQLStore(ctx, config.databaseURL, config.authToken)
	default:
		return nil, fmt.Errorf("unsupported backend %q", config.backend)
	}
}

func migrateKVStores(ctx context.Context, source, destination kvstore.Store, options kvStoreMigrateOptions) (kvStoreMigrationResult, error) {
	records, err := collectApplicationKVStoreRecords(ctx, source, options.namespace)
	if err != nil {
		return kvStoreMigrationResult{DryRun: options.dryRun, Entries: []kvStoreMigrationEntry{}}, err
	}
	return migrateSelectedKVStoreRecords(ctx, source, destination, records, options)
}

func migrateSelectedKVStoreRecords(ctx context.Context, source, destination kvstore.Store, records []kvstore.Record, options kvStoreMigrateOptions) (kvStoreMigrationResult, error) {
	result := kvStoreMigrationResult{DryRun: options.dryRun, Entries: []kvStoreMigrationEntry{}}
	result.Selected = len(records)

	for _, selected := range records {
		destinationNamespace := selected.Namespace
		if options.destinationNamespace != "" {
			destinationNamespace = options.destinationNamespace
		}
		entry := kvStoreMigrationEntry{Kind: selected.Kind, Namespace: destinationNamespace, Key: selected.Key}
		migrate := func(lockCtx context.Context) error {
			// The initial scan only selects work. Re-read the source while holding
			// the same destination lock used by replicated request writes so a
			// concurrent delete cannot be undone by a stale backfill.
			source, sourceErr := source.Get(lockCtx, selected.Kind, selected.Namespace, selected.Key)
			if errors.Is(sourceErr, kvstore.ErrNotFound) {
				entry.Status = "source-deleted"
				result.Skipped++
				return nil
			}
			if sourceErr != nil {
				return fmt.Errorf("re-read source %s/%s: %w", selected.Kind, selected.Key, sourceErr)
			}
			source.Namespace = destinationNamespace
			// Labels are denormalized metadata and older stores can contain stale
			// values. Destinations derive the canonical labels from the document.
			source.Labels = nil
			existing, getErr := destination.Get(lockCtx, source.Kind, source.Namespace, source.Key)
			switch {
			case errors.Is(getErr, kvstore.ErrNotFound):
				if options.dryRun {
					entry.Status = "would-copy"
					result.Copied++
					return nil
				}
				if _, createErr := destination.Create(lockCtx, source); createErr != nil {
					return fmt.Errorf("copy %s/%s: %w", source.Kind, source.Key, createErr)
				}
				entry.Status = "copied"
				result.Copied++
			case getErr != nil:
				return fmt.Errorf("read destination %s/%s: %w", source.Kind, source.Key, getErr)
			case bytes.Equal(existing.Value, source.Value):
				entry.Status = "skipped"
				result.Skipped++
			case options.overwrite:
				if options.dryRun {
					entry.Status = "would-update"
					result.Updated++
					return nil
				}
				source.Version = existing.Version
				if _, updateErr := destination.Update(lockCtx, source); updateErr != nil {
					return fmt.Errorf("update %s/%s: %w", source.Kind, source.Key, updateErr)
				}
				entry.Status = "updated"
				result.Updated++
			default:
				entry.Status = "conflict"
				result.Conflicts++
			}
			return nil
		}
		var err error
		if options.dryRun {
			err = migrate(ctx)
		} else {
			target := kvstore.Record{Kind: selected.Kind, Namespace: destinationNamespace, Key: selected.Key}
			err = kvstore.WithRecordLock(ctx, destination, target, migrate)
		}
		if err != nil {
			return result, err
		}
		result.Entries = append(result.Entries, entry)
	}
	if result.Conflicts > 0 {
		return result, fmt.Errorf("migration found %d conflicting destination record(s); rerun with --overwrite after review", result.Conflicts)
	}
	return result, nil
}

func migrateKubernetesKV(ctx context.Context, client kubernetes.Interface, destination kvstore.Store, options kvStoreMigrateOptions) (kvStoreMigrationResult, error) {
	return migrateKVStores(ctx, kvstore.NewKubernetesStore(client), destination, options)
}

func collectApplicationKVRecords(ctx context.Context, client kubernetes.Interface, namespace string) ([]kvstore.Record, error) {
	return collectApplicationKVStoreRecords(ctx, kvstore.NewKubernetesStore(client), namespace)
}

func collectApplicationKVStoreRecords(ctx context.Context, source kvstore.Store, namespace string) ([]kvstore.Record, error) {
	scanner, ok := source.(kvstore.Scanner)
	if !ok {
		return nil, errors.New("source KV store does not support administrative scanning")
	}
	secrets, err := scanner.Scan(ctx, kvstore.ScanQuery{Kind: kvstore.KindSecret, Namespace: namespace})
	if err != nil {
		return nil, fmt.Errorf("list source Secrets: %w", err)
	}
	configMaps, err := scanner.Scan(ctx, kvstore.ScanQuery{Kind: kvstore.KindConfigMap, Namespace: namespace})
	if err != nil {
		return nil, fmt.Errorf("list source ConfigMaps: %w", err)
	}

	records := make([]kvstore.Record, 0, len(secrets)+len(configMaps))
	for _, record := range secrets {
		var object corev1.Secret
		if err := json.Unmarshal(record.Value, &object); err != nil {
			return nil, fmt.Errorf("decode source Secret/%s: %w", record.Key, err)
		}
		if !isApplicationKVSecret(&object) {
			continue
		}
		record.Version = 0
		records = append(records, record)
	}
	for _, record := range configMaps {
		var object corev1.ConfigMap
		if err := json.Unmarshal(record.Value, &object); err != nil {
			return nil, fmt.Errorf("decode source ConfigMap/%s: %w", record.Key, err)
		}
		if !isApplicationKVConfigMap(&object) {
			continue
		}
		record.Version = 0
		records = append(records, record)
	}
	sort.Slice(records, func(i, j int) bool {
		if records[i].Kind == records[j].Kind {
			return records[i].Key < records[j].Key
		}
		return records[i].Kind < records[j].Kind
	})
	return records, nil
}

func isApplicationKVSecret(secret *corev1.Secret) bool {
	if hasAgentAPILabel(secret.Labels) {
		return true
	}
	// Provider connection and identity data predates the agentapi.proxy label
	// namespace. Keep these persistent records in the migration allowlist
	// without also copying short-lived OAuth states or broker leases.
	for _, label := range []string{
		"agentapi.ccplant.io/github-connection",
		"agentapi.ccplant.io/github-identity",
		"agentapi.ccplant.io/github-principal",
		"agentapi.ccplant.io/google-connection",
		"agentapi.ccplant.io/google-identity",
	} {
		if secret.Labels[label] == "true" {
			return true
		}
	}
	// The pre-v2 schedule store was a fixed-name Secret without labels.
	return secret.Name == "agentapi-schedules"
}

func hasAgentAPILabel(labels map[string]string) bool {
	for key := range labels {
		if strings.HasPrefix(key, "agentapi.proxy/") {
			return true
		}
	}
	return false
}

func isApplicationKVConfigMap(configMap *corev1.ConfigMap) bool {
	switch configMap.Name {
	case "agentapi-session-shares", "agentapi-user-team-mapping", "agentapi-slack-channel-cache":
		// Include fixed-name stores created before their identifying labels were
		// introduced or repaired by a subsequent write.
		return true
	}
	return hasAgentAPILabel(configMap.Labels)
}

func writeKVStoreMigrationResult(w io.Writer, result kvStoreMigrationResult, output string) error {
	if output == "json" {
		encoder := json.NewEncoder(w)
		encoder.SetIndent("", "  ")
		return encoder.Encode(result)
	}
	mode := "migration"
	if result.DryRun {
		mode = "dry run"
	}
	for _, entry := range result.Entries {
		if _, err := fmt.Fprintf(w, "%-12s %s/%s\n", entry.Status, entry.Kind, entry.Key); err != nil {
			return err
		}
	}
	_, err := fmt.Fprintf(w, "%s: selected=%d copied=%d updated=%d skipped=%d conflicts=%d\n",
		mode, result.Selected, result.Copied, result.Updated, result.Skipped, result.Conflicts)
	return err
}
