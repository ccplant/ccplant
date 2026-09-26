package esmcontrol

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	sessionrunnercore "github.com/takutakahashi/agentapi-proxy/internal/core/sessionrunner"
	"github.com/takutakahashi/agentapi-proxy/pkg/codexauth"
)

type fakeTunnel struct {
	connected map[string]bool
	statuses  map[string]int // managerID -> status for Do
	requests  []recordedRequest
	err       error
}

type fakeLocalLauncher struct {
	starts  []codexauth.WorkloadRequest
	cancels []string
}

func (l *fakeLocalLauncher) StartCodexDeviceAuth(_ context.Context, request codexauth.WorkloadRequest) error {
	l.starts = append(l.starts, request)
	return nil
}

func (l *fakeLocalLauncher) CancelCodexDeviceAuth(_ context.Context, attemptID string) error {
	l.cancels = append(l.cancels, attemptID)
	return nil
}

type recordedRequest struct {
	managerID string
	sessionID string
	method    string
	path      string
	body      string
}

func (t *fakeTunnel) IsConnected(_ context.Context, managerID string) bool {
	return t.connected[managerID]
}

func (t *fakeTunnel) Do(_ context.Context, managerID, sessionID, _ string, req *http.Request) (*http.Response, error) {
	body := ""
	if req.Body != nil {
		data, _ := io.ReadAll(req.Body)
		body = string(data)
	}
	t.requests = append(t.requests, recordedRequest{managerID: managerID, sessionID: sessionID, method: req.Method, path: req.URL.Path, body: body})
	if t.err != nil {
		return nil, t.err
	}
	status := t.statuses[managerID]
	if status == 0 {
		status = http.StatusAccepted
	}
	return &http.Response{StatusCode: status, Status: http.StatusText(status), Body: io.NopCloser(bytes.NewReader(nil))}, nil
}

type fakeDirectory struct {
	managers  []*sessionrunnercore.Manager
	pools     []*sessionrunnercore.LogicalPool
	bindings  []*sessionrunnercore.Binding
	suppliers []*sessionrunnercore.PoolSupplier
	err       error
}

func (d *fakeDirectory) ListManagers(context.Context) ([]*sessionrunnercore.Manager, error) {
	return d.managers, d.err
}

func (d *fakeDirectory) ListLogicalPools(context.Context) ([]*sessionrunnercore.LogicalPool, error) {
	return d.pools, d.err
}

func (d *fakeDirectory) ListBindings(context.Context, string) ([]*sessionrunnercore.Binding, error) {
	return d.bindings, d.err
}

func (d *fakeDirectory) ListPoolSuppliers(context.Context) ([]*sessionrunnercore.PoolSupplier, error) {
	return d.suppliers, d.err
}

func managerEntry(id string, mutate func(*sessionrunnercore.Manager)) *sessionrunnercore.Manager {
	manager := &sessionrunnercore.Manager{ID: id, Enabled: true}
	if mutate != nil {
		mutate(manager)
	}
	return manager
}

func validWorkload() codexauth.WorkloadRequest {
	return codexauth.WorkloadRequest{
		AttemptID:   "cda-0123456789abcdef",
		CallbackURL: "https://api.example.internal/internal/codex-device-auth",
		Token:       "bootstrap-token",
		ExpiresAt:   time.Now().Add(10 * time.Minute),
		SubjectType: string(sessionrunnercore.SubjectUser),
		SubjectID:   "alice",
	}
}

func authorizedDirectory(managers ...*sessionrunnercore.Manager) *fakeDirectory {
	suppliers := make([]*sessionrunnercore.PoolSupplier, 0, len(managers))
	for _, manager := range managers {
		suppliers = append(suppliers, &sessionrunnercore.PoolSupplier{Pool: "authorized", ManagerID: manager.ID, Enabled: true})
	}
	return &fakeDirectory{
		managers:  managers,
		pools:     []*sessionrunnercore.LogicalPool{{Name: "authorized", Enabled: true}},
		bindings:  []*sessionrunnercore.Binding{{ID: "binding-alice", Pool: "authorized", SubjectType: sessionrunnercore.SubjectUser, SubjectID: "alice", Role: sessionrunnercore.BindingRoleUse, Enabled: true}},
		suppliers: suppliers,
	}
}

func TestLauncherStartRoutesToFirstConnectedManager(t *testing.T) {
	tunnel := &fakeTunnel{connected: map[string]bool{"manager-b": true}}
	launcher := NewCodexDeviceAuthLauncher(tunnel, authorizedDirectory(
		managerEntry("manager-a", nil),
		managerEntry("manager-b", nil),
	))
	if err := launcher.StartCodexDeviceAuth(context.Background(), validWorkload()); err != nil {
		t.Fatal(err)
	}
	if len(tunnel.requests) != 1 {
		t.Fatalf("requests = %d, want 1", len(tunnel.requests))
	}
	request := tunnel.requests[0]
	if request.managerID != "manager-b" || request.method != http.MethodPost || request.path != "/api/v1/codex-device-auth" {
		t.Fatalf("unexpected request: %#v", request)
	}
	if request.sessionID != "cda-0123456789abcdef" {
		t.Fatalf("session id = %q, want the attempt id for correlation", request.sessionID)
	}
	if !strings.Contains(request.body, `"attempt_id":"cda-0123456789abcdef"`) {
		t.Fatalf("body = %q", request.body)
	}
}

func TestLauncherStartRequiresRequestedManager(t *testing.T) {
	tunnel := &fakeTunnel{connected: map[string]bool{"manager-a": true, "manager-b": true}}
	launcher := NewCodexDeviceAuthLauncher(tunnel, authorizedDirectory(
		managerEntry("manager-a", nil),
		managerEntry("manager-b", nil),
	))
	request := validWorkload()
	request.ManagerID = "manager-b"
	if err := launcher.StartCodexDeviceAuth(context.Background(), request); err != nil {
		t.Fatal(err)
	}
	if len(tunnel.requests) != 1 {
		t.Fatalf("requests = %d, want one request to the requested manager", len(tunnel.requests))
	}
	if tunnel.requests[0].managerID != "manager-b" {
		t.Fatalf("manager id = %q, want manager-b", tunnel.requests[0].managerID)
	}
	if !strings.Contains(tunnel.requests[0].body, `"manager_id":"manager-b"`) {
		t.Fatalf("body = %q, want requested manager id", tunnel.requests[0].body)
	}
}

func TestLauncherStartSkipsManagersWithoutWorkloadSupport(t *testing.T) {
	tunnel := &fakeTunnel{
		connected: map[string]bool{"manager-a": true, "manager-b": true},
		statuses:  map[string]int{"manager-a": http.StatusNotFound, "manager-b": http.StatusAccepted},
	}
	launcher := NewCodexDeviceAuthLauncher(tunnel, authorizedDirectory(
		managerEntry("manager-a", nil),
		managerEntry("manager-b", nil),
	))
	if err := launcher.StartCodexDeviceAuth(context.Background(), validWorkload()); err != nil {
		t.Fatal(err)
	}
	if len(tunnel.requests) != 2 {
		t.Fatalf("requests = %d, want 2", len(tunnel.requests))
	}
	if tunnel.requests[0].managerID != "manager-a" || tunnel.requests[1].managerID != "manager-b" {
		t.Fatalf("request order = %q then %q", tunnel.requests[0].managerID, tunnel.requests[1].managerID)
	}
}

func TestLauncherStartFailsWithoutConnectedManager(t *testing.T) {
	tunnel := &fakeTunnel{connected: map[string]bool{}}
	launcher := NewCodexDeviceAuthLauncher(tunnel, authorizedDirectory(
		managerEntry("manager-a", nil),
	))
	err := launcher.StartCodexDeviceAuth(context.Background(), validWorkload())
	if err == nil || !strings.Contains(err.Error(), "no authorized and healthy session pool") {
		t.Fatalf("err = %v, want no eligible pool error", err)
	}
}

func TestLauncherStartRejectsIncompleteRequests(t *testing.T) {
	launcher := NewCodexDeviceAuthLauncher(&fakeTunnel{}, &fakeDirectory{})
	request := validWorkload()
	request.Token = ""
	if err := launcher.StartCodexDeviceAuth(context.Background(), request); err == nil {
		t.Fatal("expected validation error")
	}
}

func TestLauncherStartFailsClosedWithoutSubjectBinding(t *testing.T) {
	manager := managerEntry("private-manager", nil)
	directory := authorizedDirectory(manager)
	directory.bindings[0].SubjectID = "bob"
	tunnel := &fakeTunnel{connected: map[string]bool{"private-manager": true}}
	launcher := NewCodexDeviceAuthLauncher(tunnel, directory)

	err := launcher.StartCodexDeviceAuth(context.Background(), validWorkload())
	if err == nil || !strings.Contains(err.Error(), "no authorized and healthy session pool") {
		t.Fatalf("err = %v, want fail-closed authorization error", err)
	}
	if len(tunnel.requests) != 0 {
		t.Fatalf("unauthorized workload reached managers: %#v", tunnel.requests)
	}
}

func TestLauncherStartUsesResolverSelectedLocalRoute(t *testing.T) {
	directory := authorizedDirectory()
	local := &fakeLocalLauncher{}
	launcher := NewCodexDeviceAuthLauncher(nil, directory, local)

	if err := launcher.StartCodexDeviceAuth(context.Background(), validWorkload()); err != nil {
		t.Fatal(err)
	}
	if len(local.starts) != 1 || local.starts[0].AttemptID != validWorkload().AttemptID {
		t.Fatalf("local starts = %#v", local.starts)
	}
	if err := launcher.CancelCodexDeviceAuth(context.Background(), validWorkload().AttemptID); err != nil {
		t.Fatal(err)
	}
	if len(local.cancels) != 1 || local.cancels[0] != validWorkload().AttemptID {
		t.Fatalf("local cancels = %#v", local.cancels)
	}
}

func TestLauncherPoolRouteOutranksLocalRoute(t *testing.T) {
	manager := managerEntry("pool-manager", nil)
	directory := authorizedDirectory(manager)
	tunnel := &fakeTunnel{connected: map[string]bool{manager.ID: true}}
	local := &fakeLocalLauncher{}
	launcher := NewCodexDeviceAuthLauncher(tunnel, directory, local)

	if err := launcher.StartCodexDeviceAuth(context.Background(), validWorkload()); err != nil {
		t.Fatal(err)
	}
	if len(local.starts) != 0 {
		t.Fatalf("local route was selected ahead of pool: %#v", local.starts)
	}
	if len(tunnel.requests) != 1 || tunnel.requests[0].managerID != manager.ID {
		t.Fatalf("requests = %#v", tunnel.requests)
	}
}

func TestLauncherStartOnlyUsesSupplierOfResolvedPool(t *testing.T) {
	authorized := managerEntry("authorized-manager", nil)
	unauthorized := managerEntry("unauthorized-manager", func(m *sessionrunnercore.Manager) {
		m.Capabilities = []string{sessionrunnercore.CapabilityCodexDeviceAuthV1}
		m.Default = true
	})
	directory := authorizedDirectory(authorized)
	directory.managers = append(directory.managers, unauthorized)
	directory.pools = append(directory.pools, &sessionrunnercore.LogicalPool{Name: "private", Enabled: true})
	directory.suppliers = append(directory.suppliers, &sessionrunnercore.PoolSupplier{Pool: "private", ManagerID: unauthorized.ID, Enabled: true})
	tunnel := &fakeTunnel{connected: map[string]bool{authorized.ID: true, unauthorized.ID: true}}
	launcher := NewCodexDeviceAuthLauncher(tunnel, directory)

	if err := launcher.StartCodexDeviceAuth(context.Background(), validWorkload()); err != nil {
		t.Fatal(err)
	}
	if len(tunnel.requests) != 1 || tunnel.requests[0].managerID != authorized.ID {
		t.Fatalf("requests = %#v, want only authorized pool supplier", tunnel.requests)
	}
}

func TestLauncherStartIgnoresDisabledAndDrainingManagers(t *testing.T) {
	tunnel := &fakeTunnel{connected: map[string]bool{"disabled": true, "draining": true, "enabled": true}}
	launcher := NewCodexDeviceAuthLauncher(tunnel, authorizedDirectory(
		managerEntry("disabled", func(m *sessionrunnercore.Manager) { m.Enabled = false }),
		managerEntry("draining", func(m *sessionrunnercore.Manager) { m.Draining = true }),
		managerEntry("enabled", nil),
	))
	if err := launcher.StartCodexDeviceAuth(context.Background(), validWorkload()); err != nil {
		t.Fatal(err)
	}
	if len(tunnel.requests) != 1 || tunnel.requests[0].managerID != "enabled" {
		t.Fatalf("requests = %#v", tunnel.requests)
	}
}

func TestLauncherPrefersCapabilityThenDefaultThenHeartbeat(t *testing.T) {
	tunnel := &fakeTunnel{connected: map[string]bool{"plain": true, "default": true, "capable": true, "fresh": true}}
	launcher := NewCodexDeviceAuthLauncher(tunnel, authorizedDirectory(
		managerEntry("plain", nil),
		managerEntry("default", func(m *sessionrunnercore.Manager) { m.Default = true }),
		managerEntry("capable", func(m *sessionrunnercore.Manager) {
			m.Capabilities = []string{sessionrunnercore.CapabilityCodexDeviceAuthV1}
		}),
		managerEntry("fresh", func(m *sessionrunnercore.Manager) { m.LastHeartbeatAt = time.Now() }),
	))
	if err := launcher.StartCodexDeviceAuth(context.Background(), validWorkload()); err != nil {
		t.Fatal(err)
	}
	if tunnel.requests[0].managerID != "capable" {
		t.Fatalf("first manager = %q, want capable", tunnel.requests[0].managerID)
	}
}

func TestLauncherCancelTargetsOwnerManager(t *testing.T) {
	tunnel := &fakeTunnel{connected: map[string]bool{"manager-a": true, "manager-b": true}}
	launcher := NewCodexDeviceAuthLauncher(tunnel, authorizedDirectory(
		managerEntry("manager-a", nil), managerEntry("manager-b", nil),
	))
	if err := launcher.StartCodexDeviceAuth(context.Background(), validWorkload()); err != nil {
		t.Fatal(err)
	}
	if err := launcher.CancelCodexDeviceAuth(context.Background(), "cda-0123456789abcdef"); err != nil {
		t.Fatal(err)
	}
	if len(tunnel.requests) != 2 {
		t.Fatalf("requests = %d, want 2", len(tunnel.requests))
	}
	cancel := tunnel.requests[1]
	if cancel.managerID != "manager-a" || cancel.method != http.MethodDelete || cancel.path != "/api/v1/codex-device-auth/cda-0123456789abcdef" {
		t.Fatalf("unexpected cancel request: %#v", cancel)
	}
}

func TestLauncherCancelToleratesMissingWorkload(t *testing.T) {
	tunnel := &fakeTunnel{connected: map[string]bool{"manager-a": true}, statuses: map[string]int{}}
	launcher := NewCodexDeviceAuthLauncher(tunnel, authorizedDirectory(managerEntry("manager-a", nil)))
	if err := launcher.StartCodexDeviceAuth(context.Background(), validWorkload()); err != nil {
		t.Fatal(err)
	}
	tunnel.statuses["manager-a"] = http.StatusNotFound
	if err := launcher.CancelCodexDeviceAuth(context.Background(), "cda-0123456789abcdef"); err != nil {
		t.Fatalf("cancel should tolerate a missing workload: %v", err)
	}
}

func TestLauncherCancelUnknownAttemptBroadcasts(t *testing.T) {
	tunnel := &fakeTunnel{connected: map[string]bool{"manager-a": true, "manager-b": true, "manager-c": false}}
	launcher := NewCodexDeviceAuthLauncher(tunnel, authorizedDirectory(
		managerEntry("manager-a", nil), managerEntry("manager-b", nil), managerEntry("manager-c", nil),
	))
	if err := launcher.CancelCodexDeviceAuth(context.Background(), "cda-unknown"); err != nil {
		t.Fatal(err)
	}
	if len(tunnel.requests) != 2 {
		t.Fatalf("requests = %d, want 2 broadcasts", len(tunnel.requests))
	}
}

func TestLauncherAvailableRequiresConnectedManager(t *testing.T) {
	launcher := NewCodexDeviceAuthLauncher(
		&fakeTunnel{connected: map[string]bool{"manager-a": false}},
		authorizedDirectory(managerEntry("manager-a", nil)),
	)
	if launcher.Available(context.Background()) {
		t.Fatal("available = true, want false without a connected manager")
	}
	tunnel := &fakeTunnel{connected: map[string]bool{"manager-a": true}}
	launcher = NewCodexDeviceAuthLauncher(tunnel, authorizedDirectory(managerEntry("manager-a", nil)))
	if !launcher.AvailableForSubject(context.Background(), sessionrunnercore.Subject{Type: sessionrunnercore.SubjectUser, ID: "alice"}) {
		t.Fatal("available = false, want true with a connected manager")
	}
}

func TestLauncherSurfacesDirectoryFailure(t *testing.T) {
	launcher := NewCodexDeviceAuthLauncher(&fakeTunnel{}, &fakeDirectory{err: errors.New("boom")})
	if err := launcher.StartCodexDeviceAuth(context.Background(), validWorkload()); err == nil {
		t.Fatal("expected directory error")
	}
}

func TestLauncherNeverLaunchesInProcess(t *testing.T) {
	// The launcher has no Kubernetes dependency at all: with no managers it
	// must fail instead of silently "succeeding" against a local fake client,
	// which is the regression that motivated the tunnel-only design.
	tunnel := &fakeTunnel{connected: map[string]bool{}, err: nil}
	launcher := NewCodexDeviceAuthLauncher(tunnel, &fakeDirectory{})
	if err := launcher.StartCodexDeviceAuth(context.Background(), validWorkload()); err == nil {
		t.Fatal("expected failure when no manager is enrolled")
	}
	if len(tunnel.requests) != 0 {
		t.Fatalf("requests = %d, want 0", len(tunnel.requests))
	}
}
