package services

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"testing"

	"github.com/takutakahashi/agentapi-proxy/internal/domain/entities"
	portrepos "github.com/takutakahashi/agentapi-proxy/internal/usecases/ports/repositories"
	"github.com/takutakahashi/agentapi-proxy/pkg/config"
	"github.com/takutakahashi/agentapi-proxy/pkg/sessionsettings"
)

type builtInRuntimeRouteRepo struct {
	route   *portrepos.SessionRoute
	deleted string
}

func (r *builtInRuntimeRouteRepo) Save(_ context.Context, route *portrepos.SessionRoute) error {
	copy := *route
	r.route = &copy
	return nil
}
func (r *builtInRuntimeRouteRepo) Get(context.Context, string) (*portrepos.SessionRoute, error) {
	return r.route, nil
}
func (r *builtInRuntimeRouteRepo) List(context.Context, string) ([]*portrepos.SessionRoute, error) {
	return nil, nil
}
func (r *builtInRuntimeRouteRepo) Delete(_ context.Context, id string) error {
	r.deleted = id
	return nil
}

func TestPrepareBuiltInDirectRuntimeCreatesOutboundRoute(t *testing.T) {
	repo := &builtInRuntimeRouteRepo{}
	manager := &KubernetesSessionManager{
		k8sConfig:        &config.KubernetesSessionConfig{ProvisionerProxyURL: "https://parent.example.test/"},
		sessionRouteRepo: repo,
		namespace:        "sessions",
	}
	req := &entities.RunServerRequest{UserID: "user-1", Scope: entities.ScopeUser, InitialMessage: "hello"}

	cleanup, err := manager.prepareBuiltInDirectRuntime(context.Background(), "session-1", req)
	if err != nil {
		t.Fatal(err)
	}
	if cleanup == nil || req.ParentRuntime == nil || !req.ParentRuntime.Enabled {
		t.Fatalf("parent runtime was not configured: %#v", req.ParentRuntime)
	}
	if req.ParentRuntime.Endpoint != "https://parent.example.test" || req.ParentRuntime.SessionID != "session-1" || req.ParentRuntime.Generation != 1 || req.ParentRuntime.Token == "" {
		t.Fatalf("parent runtime = %#v", req.ParentRuntime)
	}
	if repo.route == nil || repo.route.Transport != portrepos.SessionRouteTransportDirectRuntime || repo.route.RemoteSessionID != "session-1" {
		t.Fatalf("route = %#v", repo.route)
	}
	digest := sha256.Sum256([]byte(req.ParentRuntime.Token))
	if repo.route.RuntimeTokenHash != hex.EncodeToString(digest[:]) {
		t.Fatal("route token hash does not match the Pod credential")
	}

	cleanup()
	if repo.deleted != "session-1" {
		t.Fatalf("cleanup deleted %q", repo.deleted)
	}
}

func TestPrepareBuiltInDirectRuntimePreservesAllocatedParentRuntime(t *testing.T) {
	repo := &builtInRuntimeRouteRepo{}
	manager := &KubernetesSessionManager{k8sConfig: &config.KubernetesSessionConfig{}, sessionRouteRepo: repo}
	existing := &sessionsettings.ParentRuntimeConfig{Enabled: true, SessionID: "public", Token: "existing", Generation: 2}
	req := &entities.RunServerRequest{ParentRuntime: existing}

	cleanup, err := manager.prepareBuiltInDirectRuntime(context.Background(), "local", req)
	if err != nil {
		t.Fatal(err)
	}
	if cleanup != nil || req.ParentRuntime != existing || repo.route != nil {
		t.Fatal("externally allocated runtime configuration was modified")
	}
}
