package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadConfigWithReplicatedKVStore(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	contents := []byte(`kv_store:
  primary:
    backend: kubernetes
  secondary:
    backend: libsql-encrypted
    database_url: file:///tmp/secondary.db
    auth_token: token
    encryption:
      provider: aws-kms-branch
      active_key_id: dev
      kms_region: ap-northeast-1
      keys:
        dev: arn:aws:kms:ap-northeast-1:123456789012:key/example
  replication:
    mode: rollback
`)
	if err := os.WriteFile(path, contents, 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := LoadConfig(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.KVStore.Primary == nil || cfg.KVStore.Primary.Backend != "kubernetes" {
		t.Fatalf("primary = %#v", cfg.KVStore.Primary)
	}
	if cfg.KVStore.Secondary == nil || cfg.KVStore.Secondary.Backend != "libsql-encrypted" || cfg.KVStore.Secondary.AuthToken != "token" {
		t.Fatalf("secondary = %#v", cfg.KVStore.Secondary)
	}
	if cfg.KVStore.Secondary.Encryption.Provider != "aws-kms-branch" || cfg.KVStore.Secondary.Encryption.ActiveKeyID != "dev" {
		t.Fatalf("secondary encryption = %#v", cfg.KVStore.Secondary.Encryption)
	}
	if cfg.KVStore.Replication.Mode != "rollback" {
		t.Fatalf("replication mode = %q", cfg.KVStore.Replication.Mode)
	}
}

func TestLoadConfigWithReplicatedKVStoreEnvironment(t *testing.T) {
	t.Setenv("AGENTAPI_KV_STORE_PRIMARY_BACKEND", "kubernetes")
	t.Setenv("AGENTAPI_KV_STORE_SECONDARY_BACKEND", "libsql-encrypted")
	t.Setenv("AGENTAPI_KV_STORE_SECONDARY_DATABASE_URL", "file:///tmp/secondary.db")
	t.Setenv("AGENTAPI_KV_STORE_SECONDARY_ENCRYPTION_PROVIDER", "cloud-kms-branch")
	t.Setenv("AGENTAPI_KV_STORE_SECONDARY_ENCRYPTION_ACTIVE_KEY_ID", "dev")
	t.Setenv("AGENTAPI_KV_STORE_REPLICATION_MODE", "best_effort")
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte("{}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := LoadConfig(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.KVStore.Primary == nil || cfg.KVStore.Primary.Backend != "kubernetes" {
		t.Fatalf("primary = %#v", cfg.KVStore.Primary)
	}
	if cfg.KVStore.Secondary == nil || cfg.KVStore.Secondary.DatabaseURL != "file:///tmp/secondary.db" {
		t.Fatalf("secondary = %#v", cfg.KVStore.Secondary)
	}
	if cfg.KVStore.Secondary.Encryption.Provider != "cloud-kms-branch" || cfg.KVStore.Secondary.Encryption.ActiveKeyID != "dev" {
		t.Fatalf("secondary encryption = %#v", cfg.KVStore.Secondary.Encryption)
	}
	if cfg.KVStore.Replication.Mode != "best_effort" {
		t.Fatalf("replication mode = %q", cfg.KVStore.Replication.Mode)
	}
}

func TestLoadConfigWithKVEncryptionEnvironment(t *testing.T) {
	t.Setenv("AGENTAPI_KV_ENCRYPTION_ACTIVE_KEY_ID", "current")
	t.Setenv("AGENTAPI_KV_ENCRYPTION_KEYS", `{"previous":"old-key","current":"new-key"}`)
	t.Setenv("AGENTAPI_KV_ENCRYPTION_BRANCH_CACHE_TTL_SECONDS", "600")
	t.Setenv("AGENTAPI_KV_ENCRYPTION_BRANCH_CACHE_MAX_ENTRIES", "64")
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte("{}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := LoadConfig(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.KVStore.Encryption.ActiveKeyID != "current" {
		t.Fatalf("active key ID = %q", cfg.KVStore.Encryption.ActiveKeyID)
	}
	if cfg.KVStore.Encryption.Keys["previous"] != "old-key" || cfg.KVStore.Encryption.Keys["current"] != "new-key" {
		t.Fatalf("keys = %#v", cfg.KVStore.Encryption.Keys)
	}
	if cfg.KVStore.Encryption.BranchCacheTTLSeconds != 600 || cfg.KVStore.Encryption.BranchCacheMaxEntries != 64 {
		t.Fatalf("branch cache config = ttl:%d max:%d", cfg.KVStore.Encryption.BranchCacheTTLSeconds, cfg.KVStore.Encryption.BranchCacheMaxEntries)
	}
}
