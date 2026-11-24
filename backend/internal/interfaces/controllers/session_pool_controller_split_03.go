package controllers

import (
	"net/http"

	"github.com/labstack/echo/v4"
	core "github.com/takutakahashi/agentapi-proxy/internal/core/sessionrunner"
	"github.com/takutakahashi/agentapi-proxy/pkg/auth"
)

func (c *SessionPoolController) PatchPoolSupplier(ctx echo.Context) error {
	pool, err := c.store.GetPoolSupplier(ctx.Request().Context(), ctx.Param("id"), ctx.Param("pool"))
	if err != nil {
		return sessionRunnerStoreError(err)
	}
	if err := c.requirePoolManage(ctx, pool.Pool); err != nil {
		return err
	}
	var patch struct {
		Labels     map[string]string `json:"labels,omitempty"`
		MinIdle    *int              `json:"min_idle,omitempty"`
		MaxRunners *int              `json:"max_runners,omitempty"`
		Enabled    *bool             `json:"enabled,omitempty"`
		Draining   *bool             `json:"draining,omitempty"`
	}
	if err := ctx.Bind(&patch); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "invalid request")
	}
	if patch.Labels != nil {
		pool.Labels = patch.Labels
	}
	if patch.MinIdle != nil {
		pool.MinIdle = *patch.MinIdle
	}
	if patch.MaxRunners != nil {
		pool.MaxRunners = *patch.MaxRunners
	}
	if patch.Enabled != nil {
		pool.Enabled = *patch.Enabled
	}
	if patch.Draining != nil {
		pool.Draining = *patch.Draining
	}
	if pool.MinIdle < 0 || pool.MaxRunners < 0 || (pool.MaxRunners > 0 && pool.MinIdle > pool.MaxRunners) {
		return echo.NewHTTPError(http.StatusBadRequest, "invalid runner limits")
	}
	if err := c.store.UpdatePoolSupplier(ctx.Request().Context(), pool); err != nil {
		return sessionRunnerStoreError(err)
	}
	return ctx.JSON(http.StatusOK, pool)
}

func (c *SessionPoolController) DeletePoolSupplier(ctx echo.Context) error {
	requestCtx := ctx.Request().Context()
	managerID, poolName := ctx.Param("id"), ctx.Param("pool")
	if _, err := c.store.GetPoolSupplier(requestCtx, managerID, poolName); err != nil {
		return sessionRunnerStoreError(err)
	}
	if err := c.requirePoolManage(ctx, poolName); err != nil {
		return err
	}
	runners, err := c.store.ListRunners(requestCtx, poolName)
	if err != nil {
		return sessionRunnerStoreError(err)
	}
	for _, runner := range runners {
		if runner.ManagerID == managerID && runnerIsActive(runner) {
			return echo.NewHTTPError(http.StatusConflict, "pool supplier has active runners")
		}
	}
	for _, runner := range runners {
		if runner.ManagerID == managerID {
			if err := c.store.DeleteRunner(requestCtx, runner.ID); err != nil {
				return sessionRunnerStoreError(err)
			}
		}
	}
	if err := c.store.DeletePoolSupplier(requestCtx, managerID, poolName); err != nil {
		return sessionRunnerStoreError(err)
	}
	return ctx.NoContent(http.StatusNoContent)
}

func runnerIsActive(runner *core.Runner) bool {
	return runner.Status == core.RunnerRunning || runner.Status == core.RunnerClaiming
}

func allocationIsActive(allocation *core.Allocation) bool {
	return allocation.Status == core.AllocationLeased ||
		allocation.Status == core.AllocationClaimed ||
		allocation.Status == core.AllocationRunning
}

func (c *SessionPoolController) CreateBinding(ctx echo.Context) error {
	var binding core.Binding
	binding.Enabled = true
	if err := ctx.Bind(&binding); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "invalid request")
	}
	binding.Pool = ctx.Param("pool")
	if len(binding.Roles) > 0 {
		binding.Role = ""
	} else if binding.Role == "" {
		binding.Role = core.BindingRoleUse
	}
	binding.NormalizeRoles()
	if err := validatePoolBindingSubject(binding.SubjectType, binding.SubjectID); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, err.Error())
	}
	if err := validatePoolBindingRole(binding.SubjectType, &binding); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, err.Error())
	}
	if binding.MaxConcurrent < 0 {
		return echo.NewHTTPError(http.StatusBadRequest, "max_concurrent must be non-negative")
	}
	if err := c.ensureLogicalPoolExists(ctx.Request().Context(), binding.Pool); err != nil {
		return sessionRunnerStoreError(err)
	}
	user := auth.GetUserFromContext(ctx)
	canManage := user == nil || user.IsAdmin()
	if !canManage {
		var err error
		canManage, err = c.canManagePool(ctx.Request().Context(), user, binding.Pool)
		if err != nil {
			return sessionRunnerStoreError(err)
		}
	}
	if !canManage {
		if binding.Role != core.BindingRoleUse || !bindingAppliesToUser(&binding, user) || binding.SubjectType == core.SubjectAll {
			return echo.NewHTTPError(http.StatusForbidden, "pool manage binding required")
		}
		bindings, listErr := c.store.ListBindings(ctx.Request().Context(), binding.Pool)
		if listErr != nil {
			return sessionRunnerStoreError(listErr)
		}
		var globalUse *core.Binding
		for _, candidate := range bindings {
			if candidate.Enabled && candidate.Role == core.BindingRoleUse && candidate.SubjectType == core.SubjectAll && candidate.SubjectID == "" {
				globalUse = candidate
				break
			}
		}
		if globalUse == nil {
			return echo.NewHTTPError(http.StatusForbidden, "pool manage binding required")
		}
		// Self-service bindings may narrow a global grant, but must not override
		// its scheduling policy or quota.
		binding.Priority = globalUse.Priority
		binding.MaxConcurrent = globalUse.MaxConcurrent
		binding.ExplicitOnly = globalUse.ExplicitOnly
	}
	if err := c.store.CreateBinding(ctx.Request().Context(), &binding); err != nil {
		return sessionRunnerStoreError(err)
	}
	return ctx.JSON(http.StatusCreated, &binding)
}

func (c *SessionPoolController) DeleteBinding(ctx echo.Context) error {
	pool := ctx.Param("pool")
	if err := c.requirePoolManage(ctx, pool); err != nil {
		return err
	}
	bindings, err := c.store.ListBindings(ctx.Request().Context(), pool)
	if err != nil {
		return sessionRunnerStoreError(err)
	}
	found := false
	var target *core.Binding
	for _, binding := range bindings {
		if binding.ID == ctx.Param("bindingId") {
			found = true
			target = binding
		}
	}
	if !found {
		return echo.NewHTTPError(http.StatusNotFound, "binding not found")
	}
	if target.Enabled && target.GrantsManage() && !hasOtherEnabledManageBinding(bindings, target.ID) {
		return echo.NewHTTPError(http.StatusConflict, "cannot remove the last enabled manage binding")
	}
	if err := c.store.DeleteBinding(ctx.Request().Context(), ctx.Param("bindingId")); err != nil {
		return sessionRunnerStoreError(err)
	}
	return ctx.NoContent(http.StatusNoContent)
}

func (c *SessionPoolController) ListBindings(ctx echo.Context) error {
	pool := ctx.Param("pool")
	bindings, err := c.store.ListBindings(ctx.Request().Context(), pool)
	if err != nil {
		return sessionRunnerStoreError(err)
	}
	user := auth.GetUserFromContext(ctx)
	if user != nil && !user.IsAdmin() {
		canManage, checkErr := c.canManagePool(ctx.Request().Context(), user, pool)
		if checkErr != nil {
			return sessionRunnerStoreError(checkErr)
		}
		if !canManage {
			visible := make([]*core.Binding, 0, len(bindings))
			for _, binding := range bindings {
				if bindingAppliesToUser(binding, user) && (binding.SubjectType != core.SubjectAll || binding.Enabled) {
					visible = append(visible, binding)
				}
			}
			if len(visible) == 0 {
				return echo.NewHTTPError(http.StatusForbidden, "pool use or manage binding required")
			}
			bindings = visible
		}
	}
	return ctx.JSON(http.StatusOK, map[string]any{"pool_bindings": bindings})
}

func (c *SessionPoolController) PatchBinding(ctx echo.Context) error {
	pool := ctx.Param("pool")
	bindings, err := c.store.ListBindings(ctx.Request().Context(), pool)
	if err != nil {
		return sessionRunnerStoreError(err)
	}
	var binding *core.Binding
	for _, candidate := range bindings {
		if candidate.ID == ctx.Param("bindingId") {
			binding = candidate
			break
		}
	}
	if binding == nil {
		return echo.NewHTTPError(http.StatusNotFound, "binding not found")
	}
	wasEnabledManage := binding.Enabled && binding.GrantsManage()
	var patch struct {
		Role          *core.BindingRole   `json:"role,omitempty"`
		Roles         *[]core.BindingRole `json:"roles,omitempty"`
		Enabled       *bool               `json:"enabled,omitempty"`
		Priority      *int                `json:"priority,omitempty"`
		MaxConcurrent *int                `json:"max_concurrent,omitempty"`
	}
	if err := ctx.Bind(&patch); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "invalid request")
	}
	user := auth.GetUserFromContext(ctx)
	canManage := user == nil || user.IsAdmin()
	if !canManage {
		canManage, err = c.canManagePool(ctx.Request().Context(), user, pool)
		if err != nil {
			return sessionRunnerStoreError(err)
		}
	}
	if !canManage {
		if binding.Role != core.BindingRoleUse || binding.SubjectType == core.SubjectAll || !bindingAppliesToUser(binding, user) {
			return echo.NewHTTPError(http.StatusForbidden, "pool manage binding required")
		}
		if patch.Enabled == nil || patch.Role != nil || patch.Roles != nil || patch.Priority != nil || patch.MaxConcurrent != nil {
			return echo.NewHTTPError(http.StatusForbidden, "use binding may only change enabled")
		}
	}
	if patch.Role != nil {
		binding.Role = *patch.Role
		binding.Roles = nil
	}
	if patch.Roles != nil {
		binding.Roles = *patch.Roles
		binding.Role = ""
		binding.NormalizeRoles()
	}
	if patch.Enabled != nil {
		binding.Enabled = *patch.Enabled
	}
	if patch.Priority != nil {
		binding.Priority = *patch.Priority
	}
	if patch.MaxConcurrent != nil {
		if *patch.MaxConcurrent < 0 {
			return echo.NewHTTPError(http.StatusBadRequest, "max_concurrent must be non-negative")
		}
		binding.MaxConcurrent = *patch.MaxConcurrent
	}
	if err := validatePoolBindingRole(binding.SubjectType, binding); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, err.Error())
	}
	if wasEnabledManage && (!binding.GrantsManage() || !binding.Enabled) {
		if !hasOtherEnabledManageBinding(bindings, binding.ID) {
			return echo.NewHTTPError(http.StatusConflict, "cannot remove the last enabled manage binding")
		}
	}
	if err := c.store.UpdateBinding(ctx.Request().Context(), binding); err != nil {
		return sessionRunnerStoreError(err)
	}
	return ctx.JSON(http.StatusOK, binding)
}

func hasOtherEnabledManageBinding(bindings []*core.Binding, bindingID string) bool {
	for _, binding := range bindings {
		if binding.ID != bindingID && binding.Enabled && binding.GrantsManage() {
			return true
		}
	}
	return false
}

func (c *SessionPoolController) ListAvailablePools(ctx echo.Context) error {
	user := auth.GetUserFromContext(ctx)
	if user == nil {
		return echo.NewHTTPError(http.StatusUnauthorized, "authentication required")
	}
	pools, err := c.resolver.AvailablePools(ctx.Request().Context(), poolSubjectForUser(user))
	if err != nil {
		return sessionRunnerStoreError(err)
	}
	return ctx.JSON(http.StatusOK, map[string]any{"session_pools": pools})
}

type runnerRegisterRequest struct {
	RunnerID     string            `json:"runner_id"`
	Pool         string            `json:"pool"`
	PodName      string            `json:"pod_name,omitempty"`
	Namespace    string            `json:"namespace,omitempty"`
	Capabilities map[string]string `json:"capabilities,omitempty"`
}

func (c *SessionPoolController) RegisterRunner(ctx echo.Context) error {
	managerID := ctx.Request().Header.Get("X-Session-Manager-ID")
	manager, err := c.store.GetManager(ctx.Request().Context(), managerID)
	if err != nil {
		return echo.NewHTTPError(http.StatusUnauthorized, "invalid manager")
	}
	if !verifySessionRunnerToken(manager.ConnectionTokenHash, bearerToken(ctx.Request())) {
		return echo.NewHTTPError(http.StatusUnauthorized, "invalid manager token")
	}
	var input runnerRegisterRequest
	if err := ctx.Bind(&input); err != nil || input.Pool == "" {
		return echo.NewHTTPError(http.StatusBadRequest, "pool is required")
	}
	pool, err := c.store.GetPoolSupplier(ctx.Request().Context(), manager.ID, input.Pool)
	if err != nil || !pool.Enabled || pool.Draining {
		return echo.NewHTTPError(http.StatusForbidden, "pool is unavailable")
	}
	token, tokenHash, err := newSessionRunnerToken()
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "failed to create runner token")
	}
	runner := &core.Runner{ID: input.RunnerID, ManagerID: manager.ID, Pool: pool.Pool, Capabilities: input.Capabilities, TokenHash: tokenHash, Status: core.RunnerIdle, PodName: input.PodName, Namespace: input.Namespace}
	if err := c.store.CreateRunner(ctx.Request().Context(), runner); err != nil {
		return sessionRunnerStoreError(err)
	}
	copy := *runner
	copy.TokenHash = ""
	return ctx.JSON(http.StatusCreated, map[string]any{"runner": &copy, "runner_token": token})
}
