package cmd

import (
	"context"
	"errors"
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/spf13/cobra"
	"github.com/takutakahashi/agentapi-proxy/internal/infrastructure/kvstore"
)

type kvStoreBackfillSecretOptions struct {
	namespace             string
	destinationNamespace  string
	databaseURL           string
	authToken             string
	encryptionActiveKeyID string
	encryptionKeysJSON    string
	encryptionProvider    string
	encryptionKMSRegion   string
	dryRun                bool
	overwrite             bool
	output                string
}

func newKVStoreBackfillSecretCommand() *cobra.Command {
	o := &kvStoreBackfillSecretOptions{}
	command := &cobra.Command{
		Use:   "backfill-secret SECRET_NAME [SECRET_NAME...]",
		Short: "Backfill named Kubernetes Secrets into encrypted libSQL",
		Long: `Copy explicitly named Kubernetes Secrets into the encrypted libSQL application store.

This command is intended for operational Secrets that are deliberately read
through the application persistence client but are not selected by the normal
application-resource migration allowlist. Source Secrets are never modified or
deleted.

The destination is always treated as libsql-encrypted. Encryption configuration
is mandatory, and the command fails before copying if the active key or key
configuration is missing. Existing identical records are skipped; different
records are conflicts unless --overwrite is specified. Use --dry-run first to
inspect the result without writing anything.`,
		Args: cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, names []string) error {
			if o.output != "text" && o.output != "json" {
				return fmt.Errorf("unsupported output %q (must be text or json)", o.output)
			}
			return runKVStoreBackfillSecret(cmd.Context(), cmd, names, *o)
		},
	}
	flags := command.Flags()
	flags.StringVarP(&o.namespace, "namespace", "n", resolveKubernetesNamespace(), "Kubernetes namespace containing the source Secrets")
	flags.StringVar(&o.destinationNamespace, "destination-namespace", "", "rewrite copied Secrets into this destination namespace")
	flags.StringVar(&o.databaseURL, "database-url", firstConfiguredValue(os.Getenv("AGENTAPI_KV_STORE_SECONDARY_DATABASE_URL"), os.Getenv("AGENTAPI_KV_STORE_PRIMARY_DATABASE_URL"), os.Getenv("AGENTAPI_KV_STORE_DATABASE_URL")), "destination libSQL database URL")
	flags.StringVar(&o.authToken, "auth-token", firstConfiguredValue(os.Getenv("AGENTAPI_KV_STORE_SECONDARY_AUTH_TOKEN"), os.Getenv("AGENTAPI_KV_STORE_PRIMARY_AUTH_TOKEN"), os.Getenv("AGENTAPI_KV_STORE_AUTH_TOKEN")), "destination libSQL authentication token")
	flags.StringVar(&o.encryptionActiveKeyID, "encryption-active-key-id", firstConfiguredValue(os.Getenv("AGENTAPI_KV_STORE_SECONDARY_ENCRYPTION_ACTIVE_KEY_ID"), os.Getenv("AGENTAPI_KV_STORE_PRIMARY_ENCRYPTION_ACTIVE_KEY_ID"), os.Getenv("AGENTAPI_KV_ENCRYPTION_ACTIVE_KEY_ID")), "active encryption key ID")
	flags.StringVar(&o.encryptionKeysJSON, "encryption-keys-json", firstConfiguredValue(os.Getenv("AGENTAPI_KV_STORE_SECONDARY_ENCRYPTION_KEYS"), os.Getenv("AGENTAPI_KV_STORE_PRIMARY_ENCRYPTION_KEYS"), os.Getenv("AGENTAPI_KV_ENCRYPTION_KEYS")), "JSON object mapping key IDs to local keys or KMS key references")
	flags.StringVar(&o.encryptionProvider, "encryption-provider", firstConfiguredValue(os.Getenv("AGENTAPI_KV_STORE_SECONDARY_ENCRYPTION_PROVIDER"), os.Getenv("AGENTAPI_KV_STORE_PRIMARY_ENCRYPTION_PROVIDER"), os.Getenv("AGENTAPI_KV_ENCRYPTION_PROVIDER")), "encryption provider")
	flags.StringVar(&o.encryptionKMSRegion, "encryption-kms-region", firstConfiguredValue(os.Getenv("AGENTAPI_KV_STORE_SECONDARY_ENCRYPTION_KMS_REGION"), os.Getenv("AGENTAPI_KV_STORE_PRIMARY_ENCRYPTION_KMS_REGION"), os.Getenv("AGENTAPI_KV_ENCRYPTION_KMS_REGION")), "AWS KMS region")
	flags.BoolVar(&o.dryRun, "dry-run", false, "inspect Secrets and conflicts without writing to libSQL")
	flags.BoolVar(&o.overwrite, "overwrite", false, "replace different Secrets that already exist in libSQL")
	flags.StringVarP(&o.output, "output", "o", "text", "output format: text or json")
	return command
}

func firstConfiguredValue(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

func runKVStoreBackfillSecret(ctx context.Context, command *cobra.Command, names []string, o kvStoreBackfillSecretOptions) error {
	primary, secondary, err := buildMigrationStores(ctx,
		migrationStoreConfig{backend: "kubernetes"},
		migrationStoreConfig{backend: "libsql-encrypted", databaseURL: o.databaseURL, authToken: o.authToken},
	)
	if err != nil {
		return err
	}
	encrypted, err := encryptedMigrationDestination(ctx, secondary, true, o.encryptionProvider, o.encryptionActiveKeyID, o.encryptionKMSRegion, o.encryptionKeysJSON)
	if err != nil {
		_ = errors.Join(primary.Close(), secondary.Close())
		return fmt.Errorf("configure destination encryption: %w", err)
	}
	secondary = encrypted
	defer func() { _ = errors.Join(primary.Close(), secondary.Close()) }()

	records, err := collectNamedSecretRecords(ctx, primary, o.namespace, names)
	if err != nil {
		return err
	}
	result, backfillErr := migrateSelectedKVStoreRecords(ctx, primary, secondary, records, kvStoreMigrateOptions{
		namespace:            o.namespace,
		destinationNamespace: o.destinationNamespace,
		dryRun:               o.dryRun,
		overwrite:            o.overwrite,
	})
	if err := writeKVStoreMigrationResult(command.OutOrStdout(), result, o.output); err != nil {
		return err
	}
	return backfillErr
}

func collectNamedSecretRecords(ctx context.Context, source kvstore.Store, namespace string, names []string) ([]kvstore.Record, error) {
	unique := make(map[string]struct{}, len(names))
	for _, name := range names {
		name = strings.TrimSpace(name)
		if name == "" {
			return nil, errors.New("Secret name must not be empty")
		}
		unique[name] = struct{}{}
	}
	sorted := make([]string, 0, len(unique))
	for name := range unique {
		sorted = append(sorted, name)
	}
	sort.Strings(sorted)

	records := make([]kvstore.Record, 0, len(sorted))
	for _, name := range sorted {
		record, err := source.Get(ctx, kvstore.KindSecret, namespace, name)
		if errors.Is(err, kvstore.ErrNotFound) {
			return nil, fmt.Errorf("source Secret %s/%s not found", namespace, name)
		}
		if err != nil {
			return nil, fmt.Errorf("read source Secret %s/%s: %w", namespace, name, err)
		}
		record.Version = 0
		records = append(records, record)
	}
	return records, nil
}
