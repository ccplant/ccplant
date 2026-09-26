package app

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"sort"
	"strings"
	"time"

	sessionrunnercore "github.com/takutakahashi/agentapi-proxy/internal/core/sessionrunner"
	"github.com/takutakahashi/agentapi-proxy/pkg/config"
)

const builtinSessionManagerReconcileInterval = 30 * time.Second

func startBuiltInSessionManagerReconciler(ctx context.Context, store sessionrunnercore.Store, cfg *config.Config) {
	if ctx == nil || store == nil || cfg == nil || !cfg.SessionManager.Builtin.Enabled {
		return
	}
	if err := reconcileBuiltInSessionManager(ctx, store, cfg); err != nil {
		log.Printf("[SESSION_BUILTIN] Startup reconcile failed: %v", err)
	}
	go func() {
		ticker := time.NewTicker(builtinSessionManagerReconcileInterval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				if err := reconcileBuiltInSessionManager(ctx, store, cfg); err != nil {
					log.Printf("[SESSION_BUILTIN] Reconcile failed: %v", err)
				}
			}
		}
	}()
}

func reconcileBuiltInSessionManager(ctx context.Context, store sessionrunnercore.Store, cfg *config.Config) error {
	if ctx == nil || store == nil || cfg == nil {
		return errors.New("built-in session manager reconcile inputs are required")
	}
	registration := cfg.SessionManager.Builtin
	managerID := trimRegistrationValue(registration.ManagerID)
	poolName := trimRegistrationValue(registration.Pool.Name)
	connectionToken := registration.ConnectionToken
	if managerID == "" || poolName == "" || connectionToken == "" {
		return errors.New("built-in session manager_id, pool.name, and connection_token are required")
	}

	tokenHash := sha256.Sum256([]byte(connectionToken))
	tokenHashString := hex.EncodeToString(tokenHash[:])
	manager, err := ensureBuiltInManager(ctx, store, managerID, registration, tokenHashString)
	if err != nil {
		return fmt.Errorf("ensure manager %s: %w", managerID, err)
	}

	suppliers, err := store.ListPoolSuppliers(ctx)
	if err != nil {
		return fmt.Errorf("list pool suppliers: %w", err)
	}
	for _, supplier := range suppliers {
		if supplier != nil && supplier.ManagerID == managerID {
			log.Printf("[SESSION_BUILTIN] Manager %s kept existing_pool_assignment pool=%s", managerID, supplier.Pool)
			return nil
		}
	}
	if err := ensureBuiltInPool(ctx, store, manager, registration); err != nil {
		return err
	}
	log.Printf("[SESSION_BUILTIN] Registered manager %s pool=%s", managerID, poolName)
	return nil
}

func ensureBuiltInManager(ctx context.Context, store sessionrunnercore.Store, managerID string, registration config.SessionManagerBuiltinRegistrationConfig, tokenHash string) (*sessionrunnercore.Manager, error) {
	manager, err := store.GetManager(ctx, managerID)
	switch {
	case errors.Is(err, sessionrunnercore.ErrNotFound):
		manager = &sessionrunnercore.Manager{
			ID:      managerID,
			Origin:  sessionrunnercore.ManagerOriginBuiltin,
			Name:    builtinManagerName(registration),
			Scope:   sessionrunnercore.ManagerScopeSystem,
			Enabled: true,
		}
		if err := store.CreateManager(ctx, manager); err != nil {
			return nil, err
		}
	case err != nil:
		return nil, err
	case manager == nil:
		return nil, sessionrunnercore.ErrNotFound
	case manager.Origin != "" && manager.Origin != sessionrunnercore.ManagerOriginBuiltin:
		return nil, fmt.Errorf("origin %q is not adoptable", manager.Origin)
	case manager.ConnectionTokenHash != tokenHash:
		return nil, errors.New("connection token does not match existing registration")
	}

	manager.ConnectionTokenHash = tokenHash
	if manager.Scope == "" {
		manager.Scope = sessionrunnercore.ManagerScopeSystem
	}
	if manager.Name == "" {
		manager.Name = builtinManagerName(registration)
	} else if manager.Origin == sessionrunnercore.ManagerOriginBuiltin {
		manager.Name = builtinManagerName(registration)
	}
	manager.Labels = builtInManagerLabels(manager.Labels)
	manager.Enabled = !manager.Draining
	manager.Capabilities = builtInCapabilities(manager.Capabilities)
	if err := store.UpdateManager(ctx, manager); err != nil {
		return nil, err
	}
	return manager, nil
}

func ensureBuiltInPool(ctx context.Context, store sessionrunnercore.Store, manager *sessionrunnercore.Manager, registration config.SessionManagerBuiltinRegistrationConfig) error {
	poolName := trimRegistrationValue(registration.Pool.Name)
	_, err := store.GetLogicalPool(ctx, poolName)
	switch {
	case errors.Is(err, sessionrunnercore.ErrNotFound):
		poolLabels, labelErr := builtInPoolLabelsJSON(registration.Pool.Labels, registration.Pool.LabelsJSON)
		if labelErr != nil {
			return labelErr
		}
		pool := &sessionrunnercore.LogicalPool{
			Name: poolName, Labels: poolLabels, Enabled: true,
		}
		if err := store.CreateLogicalPool(ctx, pool); err != nil {
			return fmt.Errorf("create pool %s: %w", poolName, err)
		}
	case err != nil:
		return fmt.Errorf("get pool %s: %w", poolName, err)
	}

	_, err = store.GetPoolSupplier(ctx, manager.ID, poolName)
	switch {
	case errors.Is(err, sessionrunnercore.ErrNotFound):
		supplier := &sessionrunnercore.PoolSupplier{
			Pool: poolName, ManagerID: manager.ID, Enabled: true,
		}
		if err := store.CreatePoolSupplier(ctx, supplier); err != nil && !errors.Is(err, sessionrunnercore.ErrConflict) {
			return fmt.Errorf("create pool supplier: %w", err)
		}
	case err != nil:
		return fmt.Errorf("get pool supplier: %w", err)
	}

	subjectType := sessionrunnercore.SubjectType(trimRegistrationValue(registration.Pool.Binding.SubjectType))
	if subjectType == "" {
		subjectType = sessionrunnercore.SubjectAll
	}
	role := sessionrunnercore.BindingRole(trimRegistrationValue(registration.Pool.Binding.Role))
	if role == "" {
		role = sessionrunnercore.BindingRoleUse
	}
	bindings, err := store.ListBindings(ctx, poolName)
	if err != nil {
		return fmt.Errorf("list bindings: %w", err)
	}
	for _, binding := range bindings {
		if binding != nil && binding.SubjectType == subjectType && binding.SubjectID == "" {
			return nil
		}
	}
	if err := store.CreateBinding(ctx, &sessionrunnercore.Binding{
		Pool: poolName, SubjectType: subjectType, Role: role,
		ExplicitOnly: registration.Pool.ExplicitOnly, Priority: registration.Pool.Priority,
		Enabled: true,
	}); err != nil {
		return fmt.Errorf("create pool binding: %w", err)
	}
	return nil
}

func builtInPoolLabels(current map[string]string) map[string]string {
	labels := make(map[string]string, len(current))
	for key, value := range current {
		if value != "" {
			labels[key] = value
		}
	}
	return labels
}

func builtInPoolLabelsJSON(current map[string]string, raw string) (map[string]string, error) {
	if raw == "" {
		return builtInPoolLabels(current), nil
	}
	if len(current) != 0 {
		return nil, errors.New("pool.labels and pool.labels_json are mutually exclusive")
	}
	labels := map[string]string{}
	if err := json.Unmarshal([]byte(raw), &labels); err != nil {
		return nil, fmt.Errorf("decode pool.labels_json: %w", err)
	}
	return builtInPoolLabels(labels), nil
}

func builtInManagerLabels(current map[string]string) map[string]string {
	labels := make(map[string]string, len(current)+1)
	for key, value := range current {
		if value != "" {
			labels[key] = value
		}
	}
	labels["agentapi.proxy/manager-kind"] = sessionrunnercore.ManagerOriginBuiltin
	return labels
}

func builtInCapabilities(current []string) []string {
	required := []string{
		sessionrunnercore.CapabilityRunnerClaimV1,
		sessionrunnercore.CapabilityDirectRuntimeV1,
		sessionrunnercore.CapabilityCodexDeviceAuthV1,
	}
	set := make(map[string]struct{}, len(current)+len(required))
	for _, capability := range current {
		if capability != "" {
			set[capability] = struct{}{}
		}
	}
	for _, capability := range required {
		set[capability] = struct{}{}
	}
	capabilities := make([]string, 0, len(set))
	for capability := range set {
		capabilities = append(capabilities, capability)
	}
	sort.Strings(capabilities)
	return capabilities
}

func builtinManagerName(registration config.SessionManagerBuiltinRegistrationConfig) string {
	if name := trimRegistrationValue(registration.Name); name != "" {
		return name
	}
	return "Built-in session manager"
}

func trimRegistrationValue(value string) string {
	return strings.TrimSpace(value)
}
