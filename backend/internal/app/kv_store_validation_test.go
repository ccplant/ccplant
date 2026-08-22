package app

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/takutakahashi/agentapi-proxy/pkg/config"
)

func TestValidateAPIKVStoreSupportsCustomerBackends(t *testing.T) {
	for _, backend := range []string{"kubernetes", "libsql", "libsql-encrypted"} {
		t.Run(backend, func(t *testing.T) {
			if err := validateAPIKVStore(config.KVStoreConfig{Backend: backend}); err != nil {
				t.Fatalf("validateAPIKVStore(%q): %v", backend, err)
			}
		})
	}
}

func TestLibSQLBackendNameControlsEncryption(t *testing.T) {
	plain, err := buildKVBackend(config.KVStoreBackendConfig{
		Backend: "libsql", DatabaseURL: "file://" + filepath.Join(t.TempDir(), "plain.db"),
		Encryption: config.KVStoreEncryptionConfig{ActiveKeyID: "ignored", Keys: map[string]string{"ignored": "invalid"}},
	}, nil)
	if err != nil {
		t.Fatalf("plain libsql unexpectedly configured encryption: %v", err)
	}
	_ = plain.Close()

	_, err = buildKVBackend(config.KVStoreBackendConfig{
		Backend: "libsql-encrypted", DatabaseURL: "file://" + filepath.Join(t.TempDir(), "encrypted.db"),
	}, nil)
	if err == nil || !strings.Contains(err.Error(), "encryption keys are required") {
		t.Fatalf("libsql-encrypted missing-key error = %v", err)
	}
}

func TestValidateAPIKVStoreRejectsUnknownBackend(t *testing.T) {
	if err := validateAPIKVStore(config.KVStoreConfig{Backend: "unknown"}); err == nil {
		t.Fatal("validateAPIKVStore accepted an unknown backend")
	}
}
