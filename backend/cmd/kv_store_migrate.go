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

Stop application writes before running the migration. Re-running is safe when
the source has not changed: identical destination records are skipped. A
different existing destination record is reported as a conflict unless
--overwrite is specified. Use --dry-run to inspect destination conflicts
without writing anything.`,
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
			defer func() { _ = errors.Join(primary.Close(), secondary.Close()) }()
			secondary, err = encryptedMigrationDestination(cmd.Context(), secondary, o.encryptionProvider, o.encryptionActiveKeyID, o.encryptionKMSRegion, o.encryptionKeysJSON)
			if err != nil {
				return err
			}

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
	flags.StringVar(&o.encryptionActiveKeyID, "encryption-active-key-id", os.Getenv("AGENTAPI_KV_ENCRYPTION_ACTIVE_KEY_ID"), "active key ID used to encrypt destination values")
	flags.StringVar(&o.encryptionKeysJSON, "encryption-keys-json", os.Getenv("AGENTAPI_KV_ENCRYPTION_KEYS"), "JSON object mapping destination key IDs to base64-encoded 32-byte keys")
	flags.StringVar(&o.encryptionProvider, "encryption-provider", os.Getenv("AGENTAPI_KV_ENCRYPTION_PROVIDER"), "destination encryption provider (local, aws-kms, aws-kms-branch, or cloud-kms-branch)")
	flags.StringVar(&o.encryptionKMSRegion, "encryption-kms-region", os.Getenv("AGENTAPI_KV_ENCRYPTION_KMS_REGION"), "AWS region for a destination AWS KMS provider")
	flags.BoolVar(&o.dryRun, "dry-run", false, "inspect records and conflicts without writing to the secondary store")
	flags.BoolVar(&o.overwrite, "overwrite", false, "replace different records that already exist in the secondary store")
	flags.StringVarP(&o.output, "output", "o", "text", "output format: text or json")
	return command
}

func encryptedMigrationDestination(ctx context.Context, store kvstore.Store, provider, activeKeyID, kmsRegion, keysJSON string) (kvstore.Store, error) {
	if activeKeyID == "" && strings.TrimSpace(keysJSON) == "" {
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
	case "aws-kms-branch", "cloud-kms-branch":
		registry, ok := store.(kvstore.BranchKeyRegistry)
		if !ok {
			return nil, fmt.Errorf("encryption provider %q requires a branch key registry", provider)
		}
		if provider == "aws-kms-branch" {
			keyring, err = kvstore.NewBranchKMSKeyring(ctx, activeKeyID, kmsRegion, keys, registry, 15*time.Minute, 128)
		} else {
			keyring, err = kvstore.NewCloudBranchKMSKeyring(ctx, activeKeyID, keys, registry, 15*time.Minute, 128)
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
	result := kvStoreMigrationResult{DryRun: options.dryRun, Entries: []kvStoreMigrationEntry{}}
	records, err := collectApplicationKVStoreRecords(ctx, source, options.namespace)
	if err != nil {
		return result, err
	}
	result.Selected = len(records)

	for _, source := range records {
		if options.destinationNamespace != "" {
			source.Namespace = options.destinationNamespace
		}
		entry := kvStoreMigrationEntry{Kind: source.Kind, Namespace: source.Namespace, Key: source.Key}
		existing, getErr := destination.Get(ctx, source.Kind, source.Namespace, source.Key)
		switch {
		case errors.Is(getErr, kvstore.ErrNotFound):
			if options.dryRun {
				entry.Status = "would-copy"
				result.Copied++
			} else if _, createErr := destination.Create(ctx, source); createErr != nil {
				return result, fmt.Errorf("copy %s/%s: %w", source.Kind, source.Key, createErr)
			} else {
				entry.Status = "copied"
				result.Copied++
			}
		case getErr != nil:
			return result, fmt.Errorf("read destination %s/%s: %w", source.Kind, source.Key, getErr)
		case bytes.Equal(existing.Value, source.Value):
			entry.Status = "skipped"
			result.Skipped++
		case options.overwrite:
			if options.dryRun {
				entry.Status = "would-update"
				result.Updated++
			} else {
				source.Version = existing.Version
				if _, updateErr := destination.Update(ctx, source); updateErr != nil {
					return result, fmt.Errorf("update %s/%s: %w", source.Kind, source.Key, updateErr)
				}
				entry.Status = "updated"
				result.Updated++
			}
		default:
			entry.Status = "conflict"
			result.Conflicts++
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
	secrets, err := source.List(ctx, kvstore.Query{Kind: kvstore.KindSecret, Namespace: namespace})
	if err != nil {
		return nil, fmt.Errorf("list source Secrets: %w", err)
	}
	configMaps, err := source.List(ctx, kvstore.Query{Kind: kvstore.KindConfigMap, Namespace: namespace})
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
