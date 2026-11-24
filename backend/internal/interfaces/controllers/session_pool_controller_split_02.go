package controllers

import (
	"net/http"
	"strings"
	"time"

	"github.com/labstack/echo/v4"
	core "github.com/takutakahashi/agentapi-proxy/internal/core/sessionrunner"
	"github.com/takutakahashi/agentapi-proxy/pkg/auth"
	validation "k8s.io/apimachinery/pkg/util/validation"
)

func (c *SessionPoolController) GetOwnedManager(ctx echo.Context) error {
	manager, err := c.store.GetManager(ctx.Request().Context(), ctx.Param("id"))
	if err != nil {
		return sessionRunnerStoreError(err)
	}
	if !c.managerAuthorized(auth.GetUserFromContext(ctx), manager) {
		return echo.NewHTTPError(http.StatusNotFound, "session manager not found")
	}
	return ctx.JSON(http.StatusOK, redactManager(manager))
}

func (c *SessionPoolController) PatchOwnedManager(ctx echo.Context) error {
	manager, err := c.store.GetManager(ctx.Request().Context(), ctx.Param("id"))
	if err != nil {
		return sessionRunnerStoreError(err)
	}
	if !c.managerAuthorized(auth.GetUserFromContext(ctx), manager) {
		return echo.NewHTTPError(http.StatusNotFound, "session manager not found")
	}
	return c.PatchManager(ctx)
}

func (c *SessionPoolController) DeleteOwnedManager(ctx echo.Context) error {
	manager, err := c.store.GetManager(ctx.Request().Context(), ctx.Param("id"))
	if err != nil {
		return sessionRunnerStoreError(err)
	}
	if !c.managerAuthorized(auth.GetUserFromContext(ctx), manager) {
		return echo.NewHTTPError(http.StatusNotFound, "session manager not found")
	}
	return c.DeleteManager(ctx)
}

func (c *SessionPoolController) RotateManagerRegistrationToken(ctx echo.Context) error {
	manager, err := c.store.GetManager(ctx.Request().Context(), ctx.Param("id"))
	if err != nil {
		return sessionRunnerStoreError(err)
	}
	if !c.managerAuthorized(auth.GetUserFromContext(ctx), manager) {
		return echo.NewHTTPError(http.StatusNotFound, "session manager not found")
	}
	token, tokenHash, err := newSessionRunnerToken()
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "failed to create registration token")
	}
	manager.Enabled = false
	manager.RegistrationTokenHash = tokenHash
	manager.RegistrationExpiresAt = c.now().Add(15 * time.Minute)
	if err := c.store.UpdateManager(ctx.Request().Context(), manager); err != nil {
		return sessionRunnerStoreError(err)
	}
	return ctx.JSON(http.StatusOK, map[string]any{
		"manager": redactManager(manager), "registration_token": token,
		"expires_at": manager.RegistrationExpiresAt,
	})
}

func (c *SessionPoolController) CreateManager(ctx echo.Context) error {
	var input managerCreateRequest
	if err := ctx.Bind(&input); err != nil || strings.TrimSpace(input.Name) == "" {
		return echo.NewHTTPError(http.StatusBadRequest, "name is required")
	}
	token, tokenHash, err := newSessionRunnerToken()
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "failed to create connection token")
	}
	enabled := true
	if input.Enabled != nil {
		enabled = *input.Enabled
	}
	manager := &core.Manager{ID: input.ID, Name: input.Name, Labels: input.Labels, Capabilities: input.Capabilities, Enabled: enabled, ConnectionTokenHash: tokenHash}
	if err := c.store.CreateManager(ctx.Request().Context(), manager); err != nil {
		return sessionRunnerStoreError(err)
	}
	return ctx.JSON(http.StatusCreated, map[string]any{"manager": redactManager(manager), "connection_token": token})
}

func (c *SessionPoolController) ListManagers(ctx echo.Context) error {
	managers, err := c.store.ListManagers(ctx.Request().Context())
	if err != nil {
		return sessionRunnerStoreError(err)
	}
	result := make([]*core.Manager, 0, len(managers))
	for _, manager := range managers {
		result = append(result, redactManager(manager))
	}
	return ctx.JSON(http.StatusOK, map[string]any{"session_managers": result})
}

func (c *SessionPoolController) PatchManager(ctx echo.Context) error {
	manager, err := c.store.GetManager(ctx.Request().Context(), ctx.Param("id"))
	if err != nil {
		return sessionRunnerStoreError(err)
	}
	var patch struct {
		Name         *string           `json:"name,omitempty"`
		Labels       map[string]string `json:"labels,omitempty"`
		Capabilities []string          `json:"capabilities,omitempty"`
		Enabled      *bool             `json:"enabled,omitempty"`
		Draining     *bool             `json:"draining,omitempty"`
	}
	if err := ctx.Bind(&patch); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "invalid request")
	}
	if patch.Name != nil {
		manager.Name = *patch.Name
	}
	if patch.Labels != nil {
		manager.Labels = patch.Labels
	}
	if patch.Capabilities != nil {
		manager.Capabilities = patch.Capabilities
	}
	if patch.Enabled != nil {
		manager.Enabled = *patch.Enabled
	}
	if patch.Draining != nil {
		manager.Draining = *patch.Draining
	}
	if err := c.store.UpdateManager(ctx.Request().Context(), manager); err != nil {
		return sessionRunnerStoreError(err)
	}
	return ctx.JSON(http.StatusOK, redactManager(manager))
}

func (c *SessionPoolController) DeleteManager(ctx echo.Context) error {
	requestCtx := ctx.Request().Context()
	managerID := ctx.Param("id")
	if _, err := c.store.GetManager(requestCtx, managerID); err != nil {
		return sessionRunnerStoreError(err)
	}
	runners, err := c.store.ListRunners(requestCtx, "")
	if err != nil {
		return sessionRunnerStoreError(err)
	}
	allocations, err := c.store.ListAllocations(requestCtx, "")
	if err != nil {
		return sessionRunnerStoreError(err)
	}
	for _, runner := range runners {
		if runner.ManagerID == managerID && runnerIsActive(runner) {
			return echo.NewHTTPError(http.StatusConflict, "manager has active runners")
		}
	}
	for _, allocation := range allocations {
		if allocation.ManagerID == managerID && allocationIsActive(allocation) {
			return echo.NewHTTPError(http.StatusConflict, "manager has active allocations")
		}
	}
	suppliers, err := c.store.ListPoolSuppliers(requestCtx)
	if err != nil {
		return sessionRunnerStoreError(err)
	}
	for _, runner := range runners {
		if runner.ManagerID == managerID {
			if err := c.store.DeleteRunner(requestCtx, runner.ID); err != nil {
				return sessionRunnerStoreError(err)
			}
		}
	}
	for _, supplier := range suppliers {
		if supplier.ManagerID == managerID {
			if err := c.store.DeletePoolSupplier(requestCtx, managerID, supplier.Pool); err != nil {
				return sessionRunnerStoreError(err)
			}
		}
	}
	if err := c.store.DeleteManager(requestCtx, managerID); err != nil {
		return sessionRunnerStoreError(err)
	}
	return ctx.NoContent(http.StatusNoContent)
}

func (c *SessionPoolController) CreatePoolSupplier(ctx echo.Context) error {
	manager, err := c.store.GetManager(ctx.Request().Context(), ctx.Param("id"))
	if err != nil {
		return sessionRunnerStoreError(err)
	}
	if user := auth.GetUserFromContext(ctx); user != nil && !c.managerAuthorized(user, manager) {
		return echo.NewHTTPError(http.StatusNotFound, "session manager not found")
	}
	var supplier core.PoolSupplier
	supplier.Enabled = true
	if err := ctx.Bind(&supplier); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "invalid request")
	}
	if supplier.Pool == "" {
		supplier.Pool = ctx.Param("pool")
	}
	if strings.TrimSpace(supplier.Pool) == "" {
		return echo.NewHTTPError(http.StatusBadRequest, "pool name is required")
	}
	if problems := validation.IsValidLabelValue(supplier.Pool); len(problems) > 0 {
		return echo.NewHTTPError(http.StatusBadRequest, "pool name must be a valid Kubernetes label value")
	}
	if _, err := c.store.GetLogicalPool(ctx.Request().Context(), supplier.Pool); err != nil {
		return sessionRunnerStoreError(err)
	}
	if err := c.requirePoolManage(ctx, supplier.Pool); err != nil {
		return err
	}
	if supplier.MinIdle < 0 || supplier.MaxRunners < 0 || (supplier.MaxRunners > 0 && supplier.MinIdle > supplier.MaxRunners) {
		return echo.NewHTTPError(http.StatusBadRequest, "invalid runner limits")
	}
	supplier.ManagerID = manager.ID
	if err := c.store.CreatePoolSupplier(ctx.Request().Context(), &supplier); err != nil {
		return sessionRunnerStoreError(err)
	}
	return ctx.JSON(http.StatusCreated, &supplier)
}

func (c *SessionPoolController) CreateLogicalPool(ctx echo.Context) error {
	var input struct {
		Name    string            `json:"name"`
		Labels  map[string]string `json:"labels,omitempty"`
		Enabled *bool             `json:"enabled,omitempty"`
		TeamID  string            `json:"team_id,omitempty"`
	}
	if err := ctx.Bind(&input); err != nil || strings.TrimSpace(input.Name) == "" {
		return echo.NewHTTPError(http.StatusBadRequest, "pool name is required")
	}
	if problems := validation.IsValidLabelValue(input.Name); len(problems) > 0 {
		return echo.NewHTTPError(http.StatusBadRequest, "pool name must be a valid Kubernetes label value")
	}
	user := auth.GetUserFromContext(ctx)
	if user != nil && !user.IsAdmin() {
		return echo.NewHTTPError(http.StatusForbidden, "administrator access required")
	}
	enabled := true
	if input.Enabled != nil {
		enabled = *input.Enabled
	}
	pool := core.LogicalPool{Name: input.Name, Labels: input.Labels, Enabled: enabled}
	if err := c.store.CreateLogicalPool(ctx.Request().Context(), &pool); err != nil {
		return sessionRunnerStoreError(err)
	}
	if user != nil {
		binding := &core.Binding{Pool: pool.Name, SubjectType: core.SubjectUser, SubjectID: string(user.ID()), Role: core.BindingRoleManageAndUse, Enabled: true}
		if input.TeamID != "" {
			binding.SubjectType, binding.SubjectID = core.SubjectTeam, input.TeamID
		}
		if err := c.store.CreateBinding(ctx.Request().Context(), binding); err != nil {
			_ = c.store.DeleteLogicalPool(ctx.Request().Context(), pool.Name)
			return sessionRunnerStoreError(err)
		}
	}
	return ctx.JSON(http.StatusCreated, &pool)
}

func (c *SessionPoolController) ListPoolSuppliers(ctx echo.Context) error {
	if poolName := ctx.Param("pool"); poolName != "" {
		if err := c.requirePoolManage(ctx, poolName); err != nil {
			return err
		}
	}
	suppliers, err := c.store.ListPoolSuppliers(ctx.Request().Context())
	if err != nil {
		return sessionRunnerStoreError(err)
	}
	managerID := ctx.Param("id")
	poolName := ctx.Param("pool")
	result := make([]*core.PoolSupplier, 0)
	for _, supplier := range suppliers {
		if (managerID == "" || supplier.ManagerID == managerID) && (poolName == "" || supplier.Pool == poolName) {
			result = append(result, supplier)
		}
	}
	return ctx.JSON(http.StatusOK, map[string]any{"pool_suppliers": result})
}

func (c *SessionPoolController) ListPools(ctx echo.Context) error {
	pools, err := c.store.ListLogicalPools(ctx.Request().Context())
	if err != nil {
		return sessionRunnerStoreError(err)
	}
	user := auth.GetUserFromContext(ctx)
	if user == nil || user.IsAdmin() {
		return ctx.JSON(http.StatusOK, map[string]any{"session_pools": pools})
	}
	result := make([]*core.LogicalPool, 0, len(pools))
	for _, pool := range pools {
		if ok, checkErr := c.canAccessPool(ctx.Request().Context(), user, pool.Name); checkErr != nil {
			return sessionRunnerStoreError(checkErr)
		} else if ok {
			result = append(result, pool)
		}
	}
	return ctx.JSON(http.StatusOK, map[string]any{"session_pools": result})
}

func (c *SessionPoolController) PatchLogicalPool(ctx echo.Context) error {
	pool, err := c.store.GetLogicalPool(ctx.Request().Context(), ctx.Param("pool"))
	if err != nil {
		return sessionRunnerStoreError(err)
	}
	if err := c.requirePoolManage(ctx, pool.Name); err != nil {
		return err
	}
	var patch struct {
		Labels  map[string]string `json:"labels,omitempty"`
		Enabled *bool             `json:"enabled,omitempty"`
	}
	if err := ctx.Bind(&patch); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "invalid request")
	}
	if patch.Labels != nil {
		pool.Labels = patch.Labels
	}
	if patch.Enabled != nil {
		pool.Enabled = *patch.Enabled
	}
	if err := c.store.UpdateLogicalPool(ctx.Request().Context(), pool); err != nil {
		return sessionRunnerStoreError(err)
	}
	return ctx.JSON(http.StatusOK, pool)
}

func (c *SessionPoolController) DeleteLogicalPool(ctx echo.Context) error {
	requestCtx := ctx.Request().Context()
	poolName := ctx.Param("pool")
	if _, err := c.store.GetLogicalPool(requestCtx, poolName); err != nil {
		return sessionRunnerStoreError(err)
	}
	if err := c.requirePoolManage(ctx, poolName); err != nil {
		return err
	}
	runners, err := c.store.ListRunners(requestCtx, poolName)
	if err != nil {
		return sessionRunnerStoreError(err)
	}
	allocations, err := c.store.ListAllocations(requestCtx, poolName)
	if err != nil {
		return sessionRunnerStoreError(err)
	}
	for _, runner := range runners {
		if runnerIsActive(runner) {
			return echo.NewHTTPError(http.StatusConflict, "pool has active runners")
		}
	}
	for _, allocation := range allocations {
		if allocationIsActive(allocation) {
			return echo.NewHTTPError(http.StatusConflict, "pool has active allocations")
		}
	}
	suppliers, err := c.store.ListPoolSuppliers(requestCtx)
	if err != nil {
		return sessionRunnerStoreError(err)
	}
	bindings, err := c.store.ListBindings(requestCtx, poolName)
	if err != nil {
		return sessionRunnerStoreError(err)
	}
	for _, allocation := range allocations {
		if err := c.store.DeleteAllocation(requestCtx, allocation.SessionID); err != nil {
			return sessionRunnerStoreError(err)
		}
	}
	for _, runner := range runners {
		if err := c.store.DeleteRunner(requestCtx, runner.ID); err != nil {
			return sessionRunnerStoreError(err)
		}
	}
	for _, supplier := range suppliers {
		if supplier.Pool == poolName {
			if err := c.store.DeletePoolSupplier(requestCtx, supplier.ManagerID, poolName); err != nil {
				return sessionRunnerStoreError(err)
			}
		}
	}
	for _, binding := range bindings {
		if err := c.store.DeleteBinding(requestCtx, binding.ID); err != nil {
			return sessionRunnerStoreError(err)
		}
	}
	if err := c.store.DeleteLogicalPool(requestCtx, poolName); err != nil {
		return sessionRunnerStoreError(err)
	}
	return ctx.NoContent(http.StatusNoContent)
}
