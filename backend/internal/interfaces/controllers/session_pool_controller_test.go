package controllers

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/labstack/echo/v4"
	core "github.com/takutakahashi/agentapi-proxy/internal/core/sessionrunner"
	"github.com/takutakahashi/agentapi-proxy/internal/infrastructure/kvstore"
	infra "github.com/takutakahashi/agentapi-proxy/internal/infrastructure/sessionrunner"
	"k8s.io/client-go/kubernetes/fake"
)

func TestSessionPoolRunnerClaimLifecycle(t *testing.T) {
	store := infra.NewStore(kvstore.NewKubernetesStore(fake.NewSimpleClientset()), "test")
	controller := NewSessionPoolController(store, nil)

	managerResult := callSessionPoolHandler(t, controller.CreateManager, http.MethodPost, "/admin/session-managers",
		map[string]any{"id": "manager-a", "name": "Manager A"}, nil, nil)
	if managerResult.Code != http.StatusCreated {
		t.Fatalf("create manager status=%d body=%s", managerResult.Code, managerResult.Body.String())
	}
	var created struct {
		ConnectionToken string `json:"connection_token"`
	}
	decodeRecorder(t, managerResult, &created)
	if created.ConnectionToken == "" {
		t.Fatal("manager connection token was not returned")
	}
	if _, err := store.GetManager(context.Background(), "manager-a"); err != nil {
		t.Fatalf("created manager not persisted: %v, body=%s", err, managerResult.Body.String())
	}

	logicalPoolResult := callSessionPoolHandler(t, controller.CreateLogicalPool, http.MethodPost, "/admin/session-pools",
		map[string]any{"name": "linux"}, nil, nil)
	if logicalPoolResult.Code != http.StatusCreated {
		t.Fatalf("create logical pool status=%d body=%s", logicalPoolResult.Code, logicalPoolResult.Body.String())
	}
	poolResult := callSessionPoolHandler(t, controller.CreatePoolSupplier, http.MethodPost, "/admin/session-managers/manager-a/pools",
		map[string]any{"pool": "linux", "min_idle": 1, "max_runners": 2}, map[string]string{"id": "manager-a"}, nil)
	if poolResult.Code != http.StatusCreated {
		t.Fatalf("create pool status=%d body=%s", poolResult.Code, poolResult.Body.String())
	}
	pool, err := store.GetPoolSupplier(context.Background(), "manager-a", "linux")
	if err != nil || !pool.Enabled {
		t.Fatalf("pool should default enabled: pool=%+v err=%v", pool, err)
	}

	bindingResult := callSessionPoolHandler(t, controller.CreateBinding, http.MethodPost, "/admin/session-pools/linux/bindings",
		map[string]any{"subject_type": "team", "subject_id": "acme/platform"}, map[string]string{"pool": "linux"}, nil)
	if bindingResult.Code != http.StatusCreated {
		t.Fatalf("create binding status=%d body=%s", bindingResult.Code, bindingResult.Body.String())
	}
	bindings, err := store.ListBindings(context.Background(), "linux")
	if err != nil || len(bindings) != 1 || !bindings[0].Enabled {
		t.Fatalf("binding should default enabled: bindings=%+v err=%v", bindings, err)
	}

	registerResult := callSessionPoolHandler(t, controller.RegisterRunner, http.MethodPost, "/internal/session-runners/register",
		map[string]any{"runner_id": "runner-a", "pool": "linux", "pod_name": "pod-a"}, nil,
		map[string]string{"Authorization": "Bearer " + created.ConnectionToken, "X-Session-Manager-ID": "manager-a"})
	if registerResult.Code != http.StatusCreated {
		t.Fatalf("register runner status=%d body=%s", registerResult.Code, registerResult.Body.String())
	}
	var registered struct {
		RunnerToken string `json:"runner_token"`
	}
	decodeRecorder(t, registerResult, &registered)

	if err := store.Enqueue(context.Background(), &core.Allocation{SessionID: "session-a", Pool: "linux", RuntimeToken: "runtime-secret"}); err != nil {
		t.Fatal(err)
	}
	claimResult := callSessionPoolHandler(t, controller.ClaimRunnerAllocation, http.MethodGet, "/internal/session-runners/allocations/next?wait=0s",
		nil, nil, map[string]string{"Authorization": "Bearer " + registered.RunnerToken, "X-Session-Runner-ID": "runner-a"})
	if claimResult.Code != http.StatusOK {
		t.Fatalf("claim status=%d body=%s", claimResult.Code, claimResult.Body.String())
	}
	var claim struct {
		LeaseID      string `json:"lease_id"`
		RuntimeToken string `json:"runtime_token"`
	}
	decodeRecorder(t, claimResult, &claim)
	if claim.LeaseID == "" || claim.RuntimeToken != "runtime-secret" {
		t.Fatalf("invalid claim response: %+v", claim)
	}

	ackResult := callSessionPoolHandler(t, controller.AckRunnerAllocation, http.MethodPost, "/internal/session-runners/allocations/session-a/ack",
		map[string]any{"lease_id": claim.LeaseID}, map[string]string{"sessionId": "session-a"},
		map[string]string{"Authorization": "Bearer " + registered.RunnerToken, "X-Session-Runner-ID": "runner-a"})
	if ackResult.Code != http.StatusOK {
		t.Fatalf("ack status=%d body=%s", ackResult.Code, ackResult.Body.String())
	}
	runner, err := store.GetRunner(context.Background(), "runner-a")
	if err != nil || runner.Status != core.RunnerRunning {
		t.Fatalf("runner should be running: runner=%+v err=%v", runner, err)
	}
}

func TestCreateClusterWidePoolBinding(t *testing.T) {
	store := infra.NewStore(kvstore.NewKubernetesStore(fake.NewSimpleClientset()), "test")
	controller := NewSessionPoolController(store, nil)
	if err := store.CreateLogicalPool(context.Background(), &core.LogicalPool{Name: "linux", Enabled: true}); err != nil {
		t.Fatal(err)
	}

	result := callSessionPoolHandler(t, controller.CreateBinding, http.MethodPost, "/admin/session-pools/linux/bindings",
		map[string]any{"subject_type": "all"}, map[string]string{"pool": "linux"}, nil)
	if result.Code != http.StatusCreated {
		t.Fatalf("create cluster-wide binding status=%d body=%s", result.Code, result.Body.String())
	}
	bindings, err := store.ListBindings(context.Background(), "linux")
	if err != nil || len(bindings) != 1 || bindings[0].SubjectType != core.SubjectAll || bindings[0].SubjectID != "" {
		t.Fatalf("unexpected cluster-wide binding: bindings=%+v err=%v", bindings, err)
	}

	invalid := callSessionPoolHandler(t, controller.CreateBinding, http.MethodPost, "/admin/session-pools/linux/bindings",
		map[string]any{"subject_type": "all", "subject_id": "unexpected"}, map[string]string{"pool": "linux"}, nil)
	if invalid.Code != http.StatusBadRequest {
		t.Fatalf("cluster-wide binding with subject ID status=%d body=%s", invalid.Code, invalid.Body.String())
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
	if err := store.PutPreference(ctx, &core.Preference{SubjectType: core.SubjectUser, SubjectID: "alice", Enabled: true, DefaultPool: "linux"}); err != nil {
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
	if _, err := store.GetPreference(ctx, core.SubjectUser, "alice"); !errors.Is(err, core.ErrNotFound) {
		t.Fatalf("preference still exists: %v", err)
	}
	if _, err := store.GetAllocation(ctx, "session-a"); !errors.Is(err, core.ErrNotFound) {
		t.Fatalf("allocation still exists: %v", err)
	}
}

func callSessionPoolHandler(t *testing.T, handler echo.HandlerFunc, method, target string, body any, params, headers map[string]string) *httptest.ResponseRecorder {
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
