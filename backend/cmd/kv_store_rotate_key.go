package cmd

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"
	"github.com/takutakahashi/agentapi-proxy/internal/infrastructure/kvstore"
)

type kvStoreRotateKeyOptions struct {
	namespace         string
	databaseURL       string
	authToken         string
	activeKeyID       string
	keysJSON          string
	provider          string
	kmsRegion         string
	dryRun            bool
	sourceActiveKeyID string
	sourceKeysJSON    string
	sourceProvider    string
}

func newKVStoreRotateKeyCommand() *cobra.Command {
	o := &kvStoreRotateKeyOptions{}
	command := &cobra.Command{
		Use:   "rotate-key",
		Short: "Rewrap encrypted libSQL data keys with the active key",
		Long: `Rewrap each encrypted record's data key without re-encrypting its value.

Stop every application process that can write the target database before
running this command. Keep every key currently referenced by a record in the
provided keyring until the command completes successfully.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if strings.TrimSpace(o.databaseURL) == "" {
				return errors.New("database URL is required")
			}
			var keys map[string]string
			if err := json.Unmarshal([]byte(o.keysJSON), &keys); err != nil {
				return fmt.Errorf("decode encryption keys JSON: %w", err)
			}
			store, err := kvstore.NewLibSQLStore(cmd.Context(), o.databaseURL, o.authToken)
			if err != nil {
				return err
			}
			defer func() { _ = store.Close() }()
			var keyring kvstore.EnvelopeKeyring
			switch o.provider {
			case "", "local":
				keyring, err = kvstore.NewLocalKeyring(o.activeKeyID, keys)
			case "aws-kms-branch-scoped":
				keyring, err = kvstore.NewScopedBranchKMSKeyring(cmd.Context(), o.activeKeyID, o.kmsRegion, keys, store, 0, 0)
			case "cloud-kms-branch-scoped":
				keyring, err = kvstore.NewScopedCloudBranchKMSKeyring(cmd.Context(), o.activeKeyID, keys, store, 0, 0)
			default:
				return fmt.Errorf("unsupported rotation provider %q", o.provider)
			}
			if err != nil {
				return err
			}
			sourceKeyring := keyring
			if strings.TrimSpace(o.sourceKeysJSON) != "" || strings.TrimSpace(o.sourceProvider) != "" {
				var sourceKeys map[string]string
				if err := json.Unmarshal([]byte(o.sourceKeysJSON), &sourceKeys); err != nil {
					return fmt.Errorf("decode source encryption keys JSON: %w", err)
				}
				switch o.sourceProvider {
				case "", "local":
					sourceKeyring, err = kvstore.NewLocalKeyring(o.sourceActiveKeyID, sourceKeys)
				case "cloud-kms-branch":
					sourceKeyring, err = kvstore.NewCloudBranchKMSKeyring(cmd.Context(), o.sourceActiveKeyID, sourceKeys, store, 0, 0)
				default:
					return fmt.Errorf("unsupported source rotation provider %q", o.sourceProvider)
				}
				if err != nil {
					return err
				}
			}
			result, err := kvstore.RewrapAllWithKeyrings(cmd.Context(), store, sourceKeyring, keyring, o.namespace, o.dryRun)
			if err != nil {
				return err
			}
			_, err = fmt.Fprintf(cmd.OutOrStdout(), "selected=%d rewrapped=%d skipped=%d dry_run=%t\n", result.Selected, result.Rewrapped, result.Skipped, o.dryRun)
			return err
		},
	}
	flags := command.Flags()
	flags.StringVarP(&o.namespace, "namespace", "n", resolveKubernetesNamespace(), "application KV namespace")
	flags.StringVar(&o.databaseURL, "database-url", os.Getenv("AGENTAPI_KV_STORE_DATABASE_URL"), "libSQL database URL")
	flags.StringVar(&o.authToken, "auth-token", os.Getenv("AGENTAPI_KV_STORE_AUTH_TOKEN"), "libSQL authentication token")
	flags.StringVar(&o.activeKeyID, "active-key-id", os.Getenv("AGENTAPI_KV_ENCRYPTION_ACTIVE_KEY_ID"), "new active key ID")
	flags.StringVar(&o.keysJSON, "keys-json", os.Getenv("AGENTAPI_KV_ENCRYPTION_KEYS"), "JSON object mapping key IDs to local keys or KMS key references")
	flags.StringVar(&o.provider, "provider", os.Getenv("AGENTAPI_KV_ENCRYPTION_PROVIDER"), "key provider: local, aws-kms-branch-scoped, or cloud-kms-branch-scoped")
	flags.StringVar(&o.kmsRegion, "kms-region", os.Getenv("AGENTAPI_KV_ENCRYPTION_KMS_REGION"), "AWS KMS region")
	flags.BoolVar(&o.dryRun, "dry-run", false, "verify every wrapped data key without writing")
	flags.StringVar(&o.sourceProvider, "source-provider", "", "existing key provider for a cross-provider migration (local or cloud-kms-branch)")
	flags.StringVar(&o.sourceActiveKeyID, "source-active-key-id", "", "existing local active key ID")
	flags.StringVar(&o.sourceKeysJSON, "source-keys-json", "", "JSON object containing existing local keys")
	return command
}
