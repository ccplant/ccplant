package controllers

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/labstack/echo/v4"
	core "github.com/takutakahashi/agentapi-proxy/internal/core/sessionrunner"
	"github.com/takutakahashi/agentapi-proxy/internal/domain/entities"
	"github.com/takutakahashi/agentapi-proxy/internal/infrastructure/kvstore"
	"github.com/takutakahashi/agentapi-proxy/internal/infrastructure/repositories"
	infra "github.com/takutakahashi/agentapi-proxy/internal/infrastructure/sessionrunner"
	portrepos "github.com/takutakahashi/agentapi-proxy/internal/usecases/ports/repositories"
	"k8s.io/client-go/kubernetes/fake"
)

func TestLastEnabledManageBindingCannotBeRemoved(t *testing.T) {
	store := infra.NewStore(kvstore.NewKubernetesStore(fake.NewSimpleClientset()), "test")
	controller := NewSessionPoolController(store, nil)
	alice := entities.NewUser(entities.UserID("alice"), entities.UserTypeAdmin, "alice")
	requireNoError(t, alice.SetRoles([]entities.Role{entities.RoleAdmin}))
	created := callSessionPoolHandlerAs(t, controller.CreateLogicalPool, http.MethodPost, "/session-pools",
		map[string]any{"name": "linux"}, nil, nil, alice)
	if created.Code != http.StatusCreated {
		t.Fatalf("create pool status=%d body=%s", created.Code, created.Body.String())
	}
	bindings, err := store.ListBindings(context.Background(), "linux")
	if err != nil || len(bindings) != 1 {
		t.Fatalf("creator binding: bindings=%+v err=%v", bindings, err)
	}
	binding := bindings[0]

	for name, patch := range map[string]map[string]any{
		"disable": {"enabled": false},
		"demote":  {"role": "use"},
	} {
		t.Run(name, func(t *testing.T) {
			result := callSessionPoolHandlerAs(t, controller.PatchBinding, http.MethodPatch, "/session-pools/linux/bindings/"+binding.ID,
				patch, map[string]string{"pool": "linux", "bindingId": binding.ID}, nil, alice)
			if result.Code != http.StatusConflict {
				t.Fatalf("status=%d body=%s", result.Code, result.Body.String())
			}
		})
	}
	deleted := callSessionPoolHandlerAs(t, controller.DeleteBinding, http.MethodDelete, "/session-pools/linux/bindings/"+binding.ID,
		nil, map[string]string{"pool": "linux", "bindingId": binding.ID}, nil, alice)
	if deleted.Code != http.StatusConflict {
		t.Fatalf("delete last manage binding status=%d body=%s", deleted.Code, deleted.Body.String())
	}

	second := &core.Binding{Pool: "linux", SubjectType: core.SubjectUser, SubjectID: "bob", Role: core.BindingRoleManage, Enabled: true}
	if err := store.CreateBinding(context.Background(), second); err != nil {
		t.Fatal(err)
	}
	deleted = callSessionPoolHandlerAs(t, controller.DeleteBinding, http.MethodDelete, "/session-pools/linux/bindings/"+binding.ID,
		nil, map[string]string{"pool": "linux", "bindingId": binding.ID}, nil, alice)
	if deleted.Code != http.StatusNoContent {
		t.Fatalf("delete manage binding with successor status=%d body=%s", deleted.Code, deleted.Body.String())
	}
}

func TestAllBindingCannotManagePool(t *testing.T) {
	store := infra.NewStore(kvstore.NewKubernetesStore(fake.NewSimpleClientset()), "test")
	controller := NewSessionPoolController(store, nil)
	alice := entities.NewUser(entities.UserID("alice"), entities.UserTypeAdmin, "alice")
	requireNoError(t, alice.SetRoles([]entities.Role{entities.RoleAdmin}))
	created := callSessionPoolHandlerAs(t, controller.CreateLogicalPool, http.MethodPost, "/session-pools",
		map[string]any{"name": "linux"}, nil, nil, alice)
	if created.Code != http.StatusCreated {
		t.Fatalf("create pool status=%d body=%s", created.Code, created.Body.String())
	}
	result := callSessionPoolHandlerAs(t, controller.CreateBinding, http.MethodPost, "/session-pools/linux/bindings",
		map[string]any{"subject_type": "all", "role": "manage"}, map[string]string{"pool": "linux"}, nil, alice)
	if result.Code != http.StatusBadRequest {
		t.Fatalf("all manage binding status=%d body=%s", result.Code, result.Body.String())
	}
}

func TestUseBindingCanListPoolAndOnlyToggleOwnUsage(t *testing.T) {
	store := infra.NewStore(kvstore.NewKubernetesStore(fake.NewSimpleClientset()), "test")
	controller := NewSessionPoolController(store, nil)
	if err := store.CreateLogicalPool(context.Background(), &core.LogicalPool{Name: "linux", Enabled: true}); err != nil {
		t.Fatal(err)
	}
	binding := &core.Binding{Pool: "linux", SubjectType: core.SubjectUser, SubjectID: "alice", Role: core.BindingRoleUse, Enabled: true}
	if err := store.CreateBinding(context.Background(), binding); err != nil {
		t.Fatal(err)
	}
	alice := entities.NewUser(entities.UserID("alice"), entities.UserTypeAPIKey, "alice")

	pools := callSessionPoolHandlerAs(t, controller.ListPools, http.MethodGet, "/session-pools", nil, nil, nil, alice)
	if pools.Code != http.StatusOK {
		t.Fatalf("list pools status=%d body=%s", pools.Code, pools.Body.String())
	}
	listed := callSessionPoolHandlerAs(t, controller.ListBindings, http.MethodGet, "/session-pools/linux/bindings", nil, map[string]string{"pool": "linux"}, nil, alice)
	if listed.Code != http.StatusOK {
		t.Fatalf("list bindings status=%d body=%s", listed.Code, listed.Body.String())
	}

	patched := callSessionPoolHandlerAs(t, controller.PatchBinding, http.MethodPatch, "/session-pools/linux/bindings/"+binding.ID,
		map[string]any{"enabled": false}, map[string]string{"pool": "linux", "bindingId": binding.ID}, nil, alice)
	if patched.Code != http.StatusOK {
		t.Fatalf("toggle use binding status=%d body=%s", patched.Code, patched.Body.String())
	}
	forbidden := callSessionPoolHandlerAs(t, controller.PatchBinding, http.MethodPatch, "/session-pools/linux/bindings/"+binding.ID,
		map[string]any{"priority": 10}, map[string]string{"pool": "linux", "bindingId": binding.ID}, nil, alice)
	if forbidden.Code != http.StatusForbidden {
		t.Fatalf("patch use binding priority status=%d body=%s", forbidden.Code, forbidden.Body.String())
	}
	forbidden = callSessionPoolHandlerAs(t, controller.PatchBinding, http.MethodPatch, "/session-pools/linux/bindings/"+binding.ID,
		map[string]any{"roles": []string{"manage"}}, map[string]string{"pool": "linux", "bindingId": binding.ID}, nil, alice)
	if forbidden.Code != http.StatusForbidden {
		t.Fatalf("patch use binding roles status=%d body=%s", forbidden.Code, forbidden.Body.String())
	}

	pools = callSessionPoolHandlerAs(t, controller.ListPools, http.MethodGet, "/session-pools", nil, nil, nil, alice)
	if pools.Code != http.StatusOK || !bytes.Contains(pools.Body.Bytes(), []byte(`"name":"linux"`)) {
		t.Fatalf("disabled use pool remains configurable: status=%d body=%s", pools.Code, pools.Body.String())
	}
}

func TestUseBindingCanCreateDisabledOverrideForGlobalPool(t *testing.T) {
	store := infra.NewStore(kvstore.NewKubernetesStore(fake.NewSimpleClientset()), "test")
	controller := NewSessionPoolController(store, nil)
	if err := store.CreateLogicalPool(context.Background(), &core.LogicalPool{Name: "linux", Enabled: true}); err != nil {
		t.Fatal(err)
	}
	if err := store.CreateBinding(context.Background(), &core.Binding{Pool: "linux", SubjectType: core.SubjectAll, Role: core.BindingRoleUse, Enabled: true}); err != nil {
		t.Fatal(err)
	}
	alice := entities.NewUser(entities.UserID("alice"), entities.UserTypeAPIKey, "alice")
	created := callSessionPoolHandlerAs(t, controller.CreateBinding, http.MethodPost, "/session-pools/linux/bindings",
		map[string]any{"subject_type": "user", "subject_id": "alice", "role": "use", "enabled": false}, map[string]string{"pool": "linux"}, nil, alice)
	if created.Code != http.StatusCreated {
		t.Fatalf("create use override status=%d body=%s", created.Code, created.Body.String())
	}
	denied := callSessionPoolHandlerAs(t, controller.CreateBinding, http.MethodPost, "/session-pools/linux/bindings",
		map[string]any{"subject_type": "user", "subject_id": "bob", "role": "use", "enabled": false}, map[string]string{"pool": "linux"}, nil, alice)
	if denied.Code != http.StatusForbidden {
		t.Fatalf("create another user's override status=%d body=%s", denied.Code, denied.Body.String())
	}
}

func TestTeamUseBindingCanBeToggledByTeamMember(t *testing.T) {
	store := infra.NewStore(kvstore.NewKubernetesStore(fake.NewSimpleClientset()), "test")
	controller := NewSessionPoolController(store, nil)
	if err := store.CreateLogicalPool(context.Background(), &core.LogicalPool{Name: "linux", Enabled: true}); err != nil {
		t.Fatal(err)
	}
	binding := &core.Binding{Pool: "linux", SubjectType: core.SubjectTeam, SubjectID: "org/platform", Role: core.BindingRoleUse, Enabled: true}
	if err := store.CreateBinding(context.Background(), binding); err != nil {
		t.Fatal(err)
	}
	member := entities.NewUser(entities.UserID("alice"), entities.UserTypeGitHub, "alice")
	member.SetGitHubInfo(entities.NewGitHubUserInfo(1, "alice", "", "", "", "", ""), []entities.GitHubTeamMembership{{Organization: "org", TeamSlug: "platform"}})

	patched := callSessionPoolHandlerAs(t, controller.PatchBinding, http.MethodPatch, "/session-pools/linux/bindings/"+binding.ID,
		map[string]any{"enabled": false}, map[string]string{"pool": "linux", "bindingId": binding.ID}, nil, member)
	if patched.Code != http.StatusOK {
		t.Fatalf("toggle team use binding status=%d body=%s", patched.Code, patched.Body.String())
	}

	outsider := entities.NewUser(entities.UserID("bob"), entities.UserTypeAPIKey, "bob")
	denied := callSessionPoolHandlerAs(t, controller.PatchBinding, http.MethodPatch, "/session-pools/linux/bindings/"+binding.ID,
		map[string]any{"enabled": true}, map[string]string{"pool": "linux", "bindingId": binding.ID}, nil, outsider)
	if denied.Code != http.StatusForbidden {
		t.Fatalf("outsider toggle team binding status=%d body=%s", denied.Code, denied.Body.String())
	}
}

func TestDeleteLogicalPoolCascadesRelatedResources(t *testing.T) {
	ctx := context.Background()
	store := infra.NewStore(kvstore.NewKubernetesStore(fake.NewSimpleClientset()), "test")
	controller := NewSessionPoolController(store, nil)
	if err := store.CreateManager(ctx, &core.Manager{ID: "manager-a", Name: "Manager A", Enabled: true}); err != nil {
		t.Fatal(err)
	}
	if err := store.CreateLogicalPool(ctx, &core.LogicalPool{Name: "linux", Enabled: true}); err != nil {
		t.Fatal(err)
	}
	if err := store.CreatePoolSupplier(ctx, &core.PoolSupplier{ManagerID: "manager-a", Pool: "linux", Enabled: true}); err != nil {
		t.Fatal(err)
	}
	if err := store.CreateRunner(ctx, &core.Runner{ID: "runner-a", ManagerID: "manager-a", Pool: "linux", Status: core.RunnerIdle}); err != nil {
		t.Fatal(err)
	}
	if err := store.CreateBinding(ctx, &core.Binding{Pool: "linux", SubjectType: core.SubjectUser, SubjectID: "alice", Enabled: true}); err != nil {
		t.Fatal(err)
	}
	if err := store.Enqueue(ctx, &core.Allocation{SessionID: "session-a", Pool: "linux"}); err != nil {
		t.Fatal(err)
	}

	result := callSessionPoolHandler(t, controller.DeleteLogicalPool, http.MethodDelete, "/admin/session-pools/linux", nil, map[string]string{"pool": "linux"}, nil)
	if result.Code != http.StatusNoContent {
		t.Fatalf("delete logical pool status=%d body=%s", result.Code, result.Body.String())
	}
	if _, err := store.GetLogicalPool(ctx, "linux"); !errors.Is(err, core.ErrNotFound) {
		t.Fatalf("logical pool still exists: %v", err)
	}
	if suppliers, _ := store.ListPoolSuppliers(ctx); len(suppliers) != 0 {
		t.Fatalf("suppliers remain: %+v", suppliers)
	}
	if runners, _ := store.ListRunners(ctx, "linux"); len(runners) != 0 {
		t.Fatalf("runners remain: %+v", runners)
	}
	if bindings, _ := store.ListBindings(ctx, "linux"); len(bindings) != 0 {
		t.Fatalf("bindings remain: %+v", bindings)
	}
	if _, err := store.GetAllocation(ctx, "session-a"); !errors.Is(err, core.ErrNotFound) {
		t.Fatalf("allocation still exists: %v", err)
	}
}

func callSessionPoolHandler(t *testing.T, handler echo.HandlerFunc, method, target string, body any, params, headers map[string]string) *httptest.ResponseRecorder {
	return callSessionPoolHandlerAs(t, handler, method, target, body, params, headers, nil)
}

func callSessionPoolHandlerAs(t *testing.T, handler echo.HandlerFunc, method, target string, body any, params, headers map[string]string, user *entities.User) *httptest.ResponseRecorder {
	t.Helper()
	var raw []byte
	if body != nil {
		raw, _ = json.Marshal(body)
	}
	req := httptest.NewRequest(method, target, bytes.NewReader(raw))
	req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
	for key, value := range headers {
		req.Header.Set(key, value)
	}
	recorder := httptest.NewRecorder()
	ctx := echo.New().NewContext(req, recorder)
	if user != nil {
		ctx.Set("internal_user", user)
	}
	names := make([]string, 0, len(params))
	values := make([]string, 0, len(params))
	for key, value := range params {
		names = append(names, key)
		values = append(values, value)
	}
	ctx.SetParamNames(names...)
	ctx.SetParamValues(values...)
	if err := handler(ctx); err != nil {
		echo.New().HTTPErrorHandler(err, ctx)
	}
	return recorder
}

func decodeRecorder(t *testing.T, recorder *httptest.ResponseRecorder, out any) {
	t.Helper()
	if err := json.Unmarshal(recorder.Body.Bytes(), out); err != nil {
		t.Fatalf("decode response %q: %v", recorder.Body.String(), err)
	}
}

func TestManagerHeartbeatUpdatesLegacyOneshotStatusNormally(t *testing.T) {
	for _, reported := range []string{"active", "stable", "running", "starting", "creating"} {
		t.Run(reported, func(t *testing.T) {
			ctx := context.Background()
			client := fake.NewSimpleClientset()
			routes := repositories.NewKubernetesSessionRouteRepository(client, "test")
			completedAt := time.Now().Add(-2 * time.Minute).UTC()
			route := &portrepos.SessionRoute{
				SessionID: "oneshot", RemoteSessionID: "runtime", ManagerID: "manager",
				Transport: portrepos.SessionRouteTransportDirectRuntime,
				Tags:      map[string]string{"oneshot": "true"}, Status: "stopped", StatusUpdatedAt: completedAt,
			}
			if err := routes.Save(ctx, route); err != nil {
				t.Fatal(err)
			}
			controller := NewSessionPoolController(nil, routes)
			if err := controller.reconcileManagerSessionStatuses(ctx, "manager", map[string]string{"runtime": reported}); err != nil {
				t.Fatal(err)
			}
			got, err := routes.Get(ctx, "oneshot")
			if err != nil {
				t.Fatal(err)
			}
			expected := reported
			if reported == "stable" {
				expected = "active"
			}
			if got.Status != expected || !got.StatusUpdatedAt.After(completedAt) {
				t.Fatalf("heartbeat %q was not recorded normally: status=%s updated=%s", reported, got.Status, got.StatusUpdatedAt)
			}
		})
	}
}

func TestManagerHeartbeatPreservesInteractiveTTLCompletion(t *testing.T) {
	for _, status := range []string{"running", "active"} {
		for _, reported := range []string{"active", "stable", "running", "starting", "creating"} {
			t.Run(status+"/"+reported, func(t *testing.T) {
				ctx := context.Background()
				routes := repositories.NewKubernetesSessionRouteRepository(fake.NewSimpleClientset(), "test")
				updatedAt := time.Now().Add(-2 * time.Hour).UTC()
				route := &portrepos.SessionRoute{
					SessionID: "interactive", RemoteSessionID: "runtime", ManagerID: "manager",
					Transport: portrepos.SessionRouteTransportDirectRuntime,
					Tags:      map[string]string{"session_ttl": "1h"}, Status: status, StatusUpdatedAt: updatedAt,
				}
				if err := routes.Save(ctx, route); err != nil {
					t.Fatal(err)
				}
				controller := NewSessionPoolController(nil, routes)
				if err := controller.reconcileManagerSessionStatuses(ctx, "manager", map[string]string{"runtime": reported}); err != nil {
					t.Fatal(err)
				}
				got, err := routes.Get(ctx, route.SessionID)
				if err != nil {
					t.Fatal(err)
				}
				if got.Status != status || !got.StatusUpdatedAt.Equal(updatedAt) {
					t.Fatalf("heartbeat replaced runtime TTL state: status=%s updated=%s", got.Status, got.StatusUpdatedAt)
				}
			})
		}
	}
}

func TestManagerHeartbeatPreservesDirectRuntimeError(t *testing.T) {
	for _, reported := range []string{"active", "stable", "running", "starting", "creating", "unhealthy"} {
		t.Run(reported, func(t *testing.T) {
			ctx := context.Background()
			routes := repositories.NewKubernetesSessionRouteRepository(fake.NewSimpleClientset(), "test")
			failedAt := time.Now().Add(-time.Minute).UTC()
			route := &portrepos.SessionRoute{
				SessionID: "public", RemoteSessionID: "runtime", ManagerID: "manager",
				Transport: portrepos.SessionRouteTransportDirectRuntime,
				Status:    "error", StatusUpdatedAt: failedAt,
			}
			if err := routes.Save(ctx, route); err != nil {
				t.Fatal(err)
			}
			controller := NewSessionPoolController(nil, routes)
			if err := controller.reconcileManagerSessionStatuses(ctx, "manager", map[string]string{"runtime": reported}); err != nil {
				t.Fatal(err)
			}
			got, err := routes.Get(ctx, "public")
			if err != nil {
				t.Fatal(err)
			}
			if got.Status != "error" || !got.StatusUpdatedAt.Equal(failedAt) {
				t.Fatalf("heartbeat %q replaced runtime error: status=%s updated=%s", reported, got.Status, got.StatusUpdatedAt)
			}
		})
	}
}

func TestManagerHeartbeatMovesDirectRuntimeErrorWhenWorkloadIsGone(t *testing.T) {
	for _, reported := range []string{"suspended", "stopped"} {
		t.Run(reported, func(t *testing.T) {
			ctx := context.Background()
			routes := repositories.NewKubernetesSessionRouteRepository(fake.NewSimpleClientset(), "test")
			route := &portrepos.SessionRoute{
				SessionID: "public", RemoteSessionID: "runtime", ManagerID: "manager",
				Transport: portrepos.SessionRouteTransportDirectRuntime,
				Status:    "error", StatusUpdatedAt: time.Now().Add(-time.Minute).UTC(),
			}
			if err := routes.Save(ctx, route); err != nil {
				t.Fatal(err)
			}
			controller := NewSessionPoolController(nil, routes)
			if err := controller.reconcileManagerSessionStatuses(ctx, "manager", map[string]string{"runtime": reported}); err != nil {
				t.Fatal(err)
			}
			got, err := routes.Get(ctx, "public")
			if err != nil {
				t.Fatal(err)
			}
			if got.Status != reported {
				t.Fatalf("heartbeat %q was not recorded: status=%s", reported, got.Status)
			}
		})
	}
}
