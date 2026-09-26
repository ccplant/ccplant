package app

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"testing"
	"time"

	sessionrunnercore "github.com/takutakahashi/agentapi-proxy/internal/core/sessionrunner"
	"github.com/takutakahashi/agentapi-proxy/internal/infrastructure/kvstore"
	infrasessionrunner "github.com/takutakahashi/agentapi-proxy/internal/infrastructure/sessionrunner"
	"github.com/takutakahashi/agentapi-proxy/pkg/config"
	"k8s.io/client-go/kubernetes/fake"
)

func newBuiltInTestStore(t *testing.T) sessionrunnercore.Store {
	t.Helper()
	return infrasessionrunner.NewStore(kvstore.NewKubernetesStore(fake.NewSimpleClientset()), "test")
}

func builtInTestConfig() *config.Config {
	cfg := &config.Config{}
	cfg.SessionManager.Builtin.Enabled = true
	cfg.SessionManager.Builtin.ManagerID = "builtin-release"
	cfg.SessionManager.Builtin.ConnectionToken = "connection-token"
	cfg.SessionManager.Builtin.Pool.Name = "builtin"
	cfg.SessionManager.Builtin.Pool.Labels = map[string]string{"runtime": "kubernetes"}
	cfg.SessionManager.Builtin.Pool.ExplicitOnly = true
	cfg.SessionManager.Builtin.Pool.Priority = -1
	cfg.SessionManager.Builtin.Pool.Binding.SubjectType = "all"
	cfg.SessionManager.Builtin.Pool.Binding.Role = "use"
	return cfg
}

func builtInTokenHash(token string) string {
	hash := sha256.Sum256([]byte(token))
	return hex.EncodeToString(hash[:])
}

func TestReconcileBuiltInSessionManagerCreatesRegistryResources(t *testing.T) {
	store := newBuiltInTestStore(t)
	cfg := builtInTestConfig()

	if err := reconcileBuiltInSessionManager(context.Background(), store, cfg); err != nil {
		t.Fatal(err)
	}

	manager, err := store.GetManager(context.Background(), cfg.SessionManager.Builtin.ManagerID)
	if err != nil {
		t.Fatal(err)
	}
	if manager.Origin != sessionrunnercore.ManagerOriginBuiltin || manager.Scope != sessionrunnercore.ManagerScopeSystem || !manager.Enabled {
		t.Fatalf("manager = %#v, want adopted system-owned builtin manager", manager)
	}
	for _, capability := range []string{
		sessionrunnercore.CapabilityRunnerClaimV1,
		sessionrunnercore.CapabilityDirectRuntimeV1,
		sessionrunnercore.CapabilityCodexDeviceAuthV1,
	} {
		if !containsCapability(manager.Capabilities, capability) {
			t.Fatalf("capabilities = %v, want %q", manager.Capabilities, capability)
		}
	}
	pool, err := store.GetLogicalPool(context.Background(), cfg.SessionManager.Builtin.Pool.Name)
	if err != nil {
		t.Fatal(err)
	}
	if pool.Labels["runtime"] != "kubernetes" {
		t.Fatalf("pool labels = %#v, want runtime=kubernetes", pool.Labels)
	}
	supplier, err := store.GetPoolSupplier(context.Background(), manager.ID, pool.Name)
	if err != nil {
		t.Fatal(err)
	}
	if !supplier.Enabled {
		t.Fatalf("supplier = %#v, want enabled", supplier)
	}
	bindings, err := store.ListBindings(context.Background(), pool.Name)
	if err != nil {
		t.Fatal(err)
	}
	if len(bindings) != 1 || !bindings[0].ExplicitOnly || bindings[0].Priority != -1 || !bindings[0].Role.GrantsUse() {
		t.Fatalf("bindings = %#v, want explicit low-priority use binding", bindings)
	}
}

func TestReconcileBuiltInSessionManagerPreservesExistingPoolAssignment(t *testing.T) {
	store := newBuiltInTestStore(t)
	cfg := builtInTestConfig()
	managerID := cfg.SessionManager.Builtin.ManagerID
	now := time.Now().UTC()
	if err := store.CreateManager(context.Background(), &sessionrunnercore.Manager{
		ID: managerID, Origin: sessionrunnercore.ManagerOriginBuiltin, Name: "Operator placement",
		Scope: sessionrunnercore.ManagerScopeSystem, ConnectionTokenHash: builtInTokenHash("connection-token"),
		Enabled: true, LastHeartbeatAt: now, CreatedAt: now, UpdatedAt: now,
	}); err != nil {
		t.Fatal(err)
	}
	if err := store.CreateLogicalPool(context.Background(), &sessionrunnercore.LogicalPool{Name: "operator", Enabled: true}); err != nil {
		t.Fatal(err)
	}
	if err := store.CreatePoolSupplier(context.Background(), &sessionrunnercore.PoolSupplier{
		Pool: "operator", ManagerID: managerID, Enabled: true,
	}); err != nil {
		t.Fatal(err)
	}

	if err := reconcileBuiltInSessionManager(context.Background(), store, cfg); err != nil {
		t.Fatal(err)
	}
	if _, err := store.GetLogicalPool(context.Background(), cfg.SessionManager.Builtin.Pool.Name); err == nil {
		t.Fatal("created builtin pool despite existing pool assignment")
	}
	supplier, err := store.GetPoolSupplier(context.Background(), managerID, "operator")
	if err != nil {
		t.Fatal(err)
	}
	if !supplier.Enabled || supplier.Pool != "operator" {
		t.Fatalf("supplier = %#v, want preserved operator assignment", supplier)
	}
}

func TestReconcileBuiltInSessionManagerRejectsUnadoptableExternalManager(t *testing.T) {
	store := newBuiltInTestStore(t)
	cfg := builtInTestConfig()
	now := time.Now().UTC()
	if err := store.CreateManager(context.Background(), &sessionrunnercore.Manager{
		ID: cfg.SessionManager.Builtin.ManagerID, Origin: "external",
		Name: "User ESM", ConnectionTokenHash: builtInTokenHash("other-token"),
		CreatedAt: now, UpdatedAt: now,
	}); err != nil {
		t.Fatal(err)
	}

	err := reconcileBuiltInSessionManager(context.Background(), store, cfg)
	if err == nil {
		t.Fatal("reconcile unexpectedly adopted an external manager")
	}
}

func containsCapability(capabilities []string, wanted string) bool {
	for _, capability := range capabilities {
		if capability == wanted {
			return true
		}
	}
	return false
}
