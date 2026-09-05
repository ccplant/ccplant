package app

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
	sessionrunnercore "github.com/takutakahashi/agentapi-proxy/internal/core/sessionrunner"
	"github.com/takutakahashi/agentapi-proxy/internal/domain/entities"
	portrepos "github.com/takutakahashi/agentapi-proxy/internal/usecases/ports/repositories"
	"github.com/takutakahashi/agentapi-proxy/pkg/sessionsettings"
)

type rejectedProfileSettingsManager struct{ portrepos.SessionManager }

func (rejectedProfileSettingsManager) BuildRemoteProvisionSettings(context.Context, string, *entities.RunServerRequest) (*sessionsettings.SessionSettings, error) {
	return nil, errors.New("team membership is required")
}
func TestPoolSessionDoesNotIgnoreSettingsAuthorizationError(t *testing.T) {
	// No allocation store: the function must stop before enqueueing anything.
	server := &Server{sessionManager: rejectedProfileSettingsManager{}}
	result, err := server.createPoolSession(context.Background(), &sessionrunnercore.ResolvedPool{Pool: &sessionrunnercore.LogicalPool{Name: "pool"}, Binding: &sessionrunnercore.Binding{}}, "session", entities.StartRequest{ResolvedSessionProfileID: "profile"}, "user", nil)
	require.Nil(t, result)
	require.ErrorContains(t, err, "team membership is required")
}
