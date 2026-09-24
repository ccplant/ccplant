package app

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	sessionrunnercore "github.com/takutakahashi/agentapi-proxy/internal/core/sessionrunner"
	"github.com/takutakahashi/agentapi-proxy/internal/domain/entities"
)

type authorizedRouteTestStore struct {
	sessionrunnercore.Store
	manager *sessionrunnercore.Manager
	pool    *sessionrunnercore.LogicalPool
	binding *sessionrunnercore.Binding
}

func TestCreateSessionFailsClosedWithoutAuthorizedRouting(t *testing.T) {
	server := &Server{localSessionFallbackEnabled: true}
	session, err := server.createSession(context.Background(), "session", entities.StartRequest{}, "alice", "user", nil)
	require.Nil(t, session)
	require.ErrorContains(t, err, "authorized session routing is unavailable")
}

func TestPoolSessionRequiresAuthorizedRoute(t *testing.T) {
	server := &Server{}
	session, err := server.createPoolSession(context.Background(), nil, "session", entities.StartRequest{}, "alice", nil)
	require.Nil(t, session)
	require.ErrorContains(t, err, "authorized session route is required")
}

func (s authorizedRouteTestStore) ListManagers(context.Context) ([]*sessionrunnercore.Manager, error) {
	return []*sessionrunnercore.Manager{s.manager}, nil
}

func (s authorizedRouteTestStore) ListLogicalPools(context.Context) ([]*sessionrunnercore.LogicalPool, error) {
	return []*sessionrunnercore.LogicalPool{s.pool}, nil
}

func (s authorizedRouteTestStore) ListBindings(context.Context, string) ([]*sessionrunnercore.Binding, error) {
	return []*sessionrunnercore.Binding{s.binding}, nil
}

func (s authorizedRouteTestStore) ListPoolSuppliers(context.Context) ([]*sessionrunnercore.PoolSupplier, error) {
	return []*sessionrunnercore.PoolSupplier{{Pool: s.pool.Name, ManagerID: s.manager.ID, Enabled: true}}, nil
}

func testAuthorizedRoute(t *testing.T, pool string, binding *sessionrunnercore.Binding) sessionrunnercore.AuthorizedRoute {
	t.Helper()
	if binding == nil {
		binding = &sessionrunnercore.Binding{}
	}
	binding.Pool = pool
	binding.SubjectType = sessionrunnercore.SubjectUser
	binding.SubjectID = "test-user"
	binding.Role = sessionrunnercore.BindingRoleUse
	binding.Enabled = true
	store := authorizedRouteTestStore{
		manager: &sessionrunnercore.Manager{ID: "test-manager", Enabled: true},
		pool:    &sessionrunnercore.LogicalPool{Name: pool, Enabled: true},
		binding: binding,
	}
	route, err := sessionrunnercore.NewResolver(store, 0).ResolveRoute(context.Background(), sessionrunnercore.Subject{Type: sessionrunnercore.SubjectUser, ID: "test-user"}, sessionrunnercore.RouteRequest{RequestedPool: pool})
	require.NoError(t, err)
	require.NotNil(t, route)
	return route
}
