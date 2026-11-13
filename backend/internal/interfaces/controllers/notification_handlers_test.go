package controllers

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/takutakahashi/agentapi-proxy/internal/domain/entities"
	"github.com/takutakahashi/agentapi-proxy/internal/usecases/ports/repositories"
)

type contextNotificationManager struct {
	session entities.Session
	err     error
}

func (m *contextNotificationManager) GetSessionContext(context.Context, string) (entities.Session, error) {
	return m.session, m.err
}
func (m *contextNotificationManager) GetSession(string) entities.Session { return m.session }
func (m *contextNotificationManager) ListSessions(entities.SessionFilter) []entities.Session {
	return nil
}
func (m *contextNotificationManager) CreateSession(context.Context, string, *entities.RunServerRequest, []byte) (entities.Session, error) {
	return nil, nil
}
func (m *contextNotificationManager) DeleteSession(string) error                        { return nil }
func (m *contextNotificationManager) SendMessage(context.Context, string, string) error { return nil }
func (m *contextNotificationManager) StopAgent(context.Context, string) error           { return nil }
func (m *contextNotificationManager) GetMessages(context.Context, string) ([]repositories.Message, error) {
	return nil, nil
}
func (m *contextNotificationManager) Shutdown(time.Duration) error { return nil }

type notificationRouteRepository struct {
	route *repositories.SessionRoute
	err   error
}

func (r *notificationRouteRepository) Save(context.Context, *repositories.SessionRoute) error {
	return nil
}
func (r *notificationRouteRepository) Get(context.Context, string) (*repositories.SessionRoute, error) {
	return r.route, r.err
}
func (r *notificationRouteRepository) List(context.Context, string) ([]*repositories.SessionRoute, error) {
	return nil, nil
}
func (r *notificationRouteRepository) Delete(context.Context, string) error { return nil }

func TestResolveNotificationSessionFallsBackToDurableRoute(t *testing.T) {
	handler := NewNotificationHandlers(nil,
		&contextNotificationManager{err: errors.New("manager unavailable")},
		&notificationRouteRepository{route: &repositories.SessionRoute{
			SessionID: "public-id", UserID: "owner", Scope: string(entities.ScopeUser), InitialMessage: "task",
		}},
	)

	session, err := handler.resolveNotificationSession(context.Background(), "public-id")
	require.NoError(t, err)
	require.Equal(t, "public-id", session.ID)
	require.Equal(t, "owner", session.UserID)
	require.Equal(t, "task", session.InitialMessage)
}

func TestResolveNotificationSessionPreservesLookupFailure(t *testing.T) {
	handler := NewNotificationHandlers(nil,
		&contextNotificationManager{err: errors.New("manager unavailable")},
		&notificationRouteRepository{},
	)

	session, err := handler.resolveNotificationSession(context.Background(), "missing")
	require.Nil(t, session)
	require.ErrorContains(t, err, "manager unavailable")
}

func TestResolveNotificationSessionPreservesRouteFailure(t *testing.T) {
	handler := NewNotificationHandlers(nil, &contextNotificationManager{},
		&notificationRouteRepository{err: errors.New("route store unavailable")},
	)

	session, err := handler.resolveNotificationSession(context.Background(), "missing")
	require.Nil(t, session)
	require.ErrorContains(t, err, "route store unavailable")
}
