package app

import (
	"path/filepath"
	"testing"

	"github.com/takutakahashi/agentapi-proxy/pkg/config"
	"k8s.io/client-go/kubernetes/fake"
)

func TestBuildApplicationKVStoreLegacyKubernetes(t *testing.T) {
	store, wrap, err := buildApplicationKVStore(config.KVStoreConfig{Backend: "kubernetes"}, fake.NewSimpleClientset())
	if err != nil || store != nil || wrap {
		t.Fatalf("store=%v wrap=%v err=%v", store, wrap, err)
	}
}

func TestBuildESMControlStoreRequiresRedis(t *testing.T) {
	if store := buildESMControlStore(&config.Config{}); store != nil {
		t.Fatalf("ESM control store without Redis = %T, want nil", store)
	}
}

func TestRuntimeTunnelEnabled(t *testing.T) {
	tests := []struct {
		name       string
		configured string
		redisAddr  string
		want       bool
	}{
		{name: "defaults on with redis", redisAddr: "redis:6379", want: true},
		{name: "defaults off without redis"},
		{name: "explicit true", configured: "true", want: true},
		{name: "explicit false overrides redis", configured: "false", redisAddr: "redis:6379"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := runtimeTunnelEnabled(tt.configured, tt.redisAddr); got != tt.want {
				t.Fatalf("runtimeTunnelEnabled(%q, %q) = %v, want %v", tt.configured, tt.redisAddr, got, tt.want)
			}
		})
	}
}

func TestBuildApplicationKVStoreReplicated(t *testing.T) {
	cfg := config.KVStoreConfig{
		Primary:   &config.KVStoreBackendConfig{Backend: "kubernetes"},
		Secondary: &config.KVStoreBackendConfig{Backend: "libsql", DatabaseURL: "file://" + filepath.Join(t.TempDir(), "secondary.db")},
	}
	store, wrap, err := buildApplicationKVStore(cfg, fake.NewSimpleClientset())
	if err != nil {
		t.Fatal(err)
	}
	if store == nil || !wrap {
		t.Fatalf("store=%v wrap=%v", store, wrap)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestBuildApplicationKVStoreRejectsMixedConfig(t *testing.T) {
	cfg := config.KVStoreConfig{
		Backend: "libsql",
		Primary: &config.KVStoreBackendConfig{Backend: "kubernetes"},
	}
	if _, _, err := buildApplicationKVStore(cfg, fake.NewSimpleClientset()); err == nil {
		t.Fatal("mixed legacy and nested config succeeded")
	}
}
