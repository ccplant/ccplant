package app

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
	sessionrunnercore "github.com/takutakahashi/agentapi-proxy/internal/core/sessionrunner"
	"github.com/takutakahashi/agentapi-proxy/internal/domain/entities"
	"github.com/takutakahashi/agentapi-proxy/internal/infrastructure/kvstore"
	infrasessionrunner "github.com/takutakahashi/agentapi-proxy/internal/infrastructure/sessionrunner"
	portrepos "github.com/takutakahashi/agentapi-proxy/internal/usecases/ports/repositories"
	"github.com/takutakahashi/agentapi-proxy/pkg/sessionsettings"
	"k8s.io/client-go/kubernetes/fake"
)

type rejectedProfileSettingsManager struct{ portrepos.SessionManager }

func (rejectedProfileSettingsManager) BuildRemoteProvisionSettings(context.Context, string, *entities.RunServerRequest) (*sessionsettings.SessionSettings, error) {
	return nil, errors.New("team membership is required")
}

type capturingPoolSettingsManager struct {
	portrepos.SessionManager
	request *entities.RunServerRequest
}

type poolSettingsRepository struct{ settings *entities.Settings }

func (r *poolSettingsRepository) Save(context.Context, *entities.Settings) error { return nil }
func (r *poolSettingsRepository) FindByName(_ context.Context, name string) (*entities.Settings, error) {
	if r.settings != nil && r.settings.Name() == name {
		return r.settings, nil
	}
	return nil, errors.New("not found")
}
func (r *poolSettingsRepository) Delete(context.Context, string) error               { return nil }
func (r *poolSettingsRepository) Exists(context.Context, string) (bool, error)       { return false, nil }
func (r *poolSettingsRepository) List(context.Context) ([]*entities.Settings, error) { return nil, nil }

func (m *capturingPoolSettingsManager) BuildRemoteProvisionSettings(_ context.Context, _ string, request *entities.RunServerRequest) (*sessionsettings.SessionSettings, error) {
	m.request = request
	return &sessionsettings.SessionSettings{}, nil
}

func TestPoolSessionPreservesDockerRequirement(t *testing.T) {
	store := infrasessionrunner.NewStore(kvstore.NewKubernetesStore(fake.NewSimpleClientset()), "test")
	manager := &capturingPoolSettingsManager{}
	server := &Server{sessionManager: manager, sessionRunnerStore: store, sessionRouteRepo: &recordingSessionRouteRepository{}}

	_, err := server.createPoolSession(
		context.Background(),
		&sessionrunnercore.ResolvedPool{Pool: &sessionrunnercore.LogicalPool{Name: "pool"}, Binding: &sessionrunnercore.Binding{}},
		"session",
		entities.StartRequest{Params: &entities.SessionParams{Docker: &entities.DockerParams{Enabled: true}}},
		"user",
		nil,
	)

	require.NoError(t, err)
	require.NotNil(t, manager.request)
	require.NotNil(t, manager.request.Docker)
	require.True(t, manager.request.Docker.Enabled)
	allocation, err := store.GetAllocation(context.Background(), "session")
	require.NoError(t, err)
	require.Equal(t, "true", allocation.Requirements["dind"])
}
func TestPoolSessionDoesNotIgnoreSettingsAuthorizationError(t *testing.T) {
	// No allocation store: the function must stop before enqueueing anything.
	server := &Server{sessionManager: rejectedProfileSettingsManager{}}
	result, err := server.createPoolSession(context.Background(), &sessionrunnercore.ResolvedPool{Pool: &sessionrunnercore.LogicalPool{Name: "pool"}, Binding: &sessionrunnercore.Binding{}}, "session", entities.StartRequest{ResolvedSessionProfileID: "profile"}, "user", nil)
	require.Nil(t, result)
	require.ErrorContains(t, err, "team membership is required")
}

func TestPoolSessionOverlaysScopedAutoSuspendPolicy(t *testing.T) {
	store := infrasessionrunner.NewStore(kvstore.NewKubernetesStore(fake.NewSimpleClientset()), "test")
	manager := &capturingPoolSettingsManager{}
	userSettings := entities.NewSettings("user")
	userSettings.SetAutoSuspend(&entities.AutoSuspendSettings{Enabled: true, IdleTimeoutMinutes: 1})
	server := &Server{
		sessionManager: manager, sessionRunnerStore: store, sessionRouteRepo: &recordingSessionRouteRepository{},
		settingsRepo: &poolSettingsRepository{settings: userSettings},
	}

	_, err := server.createPoolSession(context.Background(),
		&sessionrunnercore.ResolvedPool{Pool: &sessionrunnercore.LogicalPool{Name: "pool"}, Binding: &sessionrunnercore.Binding{}},
		"session", entities.StartRequest{Scope: entities.ScopeUser}, "user", nil)
	require.NoError(t, err)
	allocation, err := store.GetAllocation(context.Background(), "session")
	require.NoError(t, err)
	var settings sessionsettings.SessionSettings
	require.NoError(t, json.Unmarshal(allocation.ProvisionSettings, &settings))
	require.NotNil(t, settings.Session.AutoSuspendEnabled)
	require.True(t, *settings.Session.AutoSuspendEnabled)
	require.Equal(t, 1, settings.Session.AutoSuspendMinutes)
}

func TestPoolSessionDoesNotAddDeprecatedPoolTag(t *testing.T) {
	store := infrasessionrunner.NewStore(kvstore.NewKubernetesStore(fake.NewSimpleClientset()), "test")
	routes := &recordingSessionRouteRepository{}
	server := &Server{sessionRunnerStore: store, sessionRouteRepo: routes}
	tags := map[string]string{"repository": "owner/repo"}

	result, err := server.createPoolSession(
		context.Background(),
		&sessionrunnercore.ResolvedPool{Pool: &sessionrunnercore.LogicalPool{Name: "pool"}, Binding: &sessionrunnercore.Binding{}},
		"session",
		entities.StartRequest{Tags: tags},
		"user",
		nil,
	)

	require.NoError(t, err)
	require.NotNil(t, result)
	require.Equal(t, tags, routes.route.Tags)
	require.NotContains(t, routes.route.Tags, "allocator.pool")
}
