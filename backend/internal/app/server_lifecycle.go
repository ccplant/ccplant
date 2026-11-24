package app

import (
	"context"
	"errors"
	"time"

	"github.com/labstack/echo/v4"
	"github.com/takutakahashi/agentapi-proxy/internal/runtimeconfig"
	portrepos "github.com/takutakahashi/agentapi-proxy/internal/usecases/ports/repositories"
	"github.com/takutakahashi/agentapi-proxy/pkg/config"
	"github.com/takutakahashi/agentapi-proxy/pkg/notification"
)

func (s *Server) DeleteSessionByID(sessionID string) error {
	// Delete associated share link if exists (ignore errors as share may not exist)
	if s.shareRepo != nil {
		_ = s.shareRepo.Delete(sessionID)
	}

	return s.sessionManager.DeleteSession(sessionID)
}

// DeletePendingSessionAllocation deletes an allocation request that has not
// produced a session yet, including one orphaned after being claimed.
func (s *Server) DeletePendingSessionAllocation(ctx context.Context, sessionID string) (bool, error) {
	manager, ok := s.sessionManager.(interface {
		DeletePendingSessionAllocation(context.Context, string) (bool, error)
	})
	if !ok {
		return false, nil
	}
	return manager.DeletePendingSessionAllocation(ctx, sessionID)
}

func (s *Server) DeleteProvisionRequest(ctx context.Context, sessionID string) error {
	if manager, ok := s.sessionManager.(interface {
		DeleteProvisionRequest(context.Context, string) error
	}); ok {
		return manager.DeleteProvisionRequest(ctx, sessionID)
	}
	return nil
}

func (s *Server) DeleteSessionPoolAllocation(ctx context.Context, sessionID string) error {
	if s.sessionRunnerStore == nil {
		return nil
	}
	allocation, err := s.sessionRunnerStore.GetAllocation(ctx, sessionID)
	if err == nil && allocation.RunnerID != "" {
		_ = s.sessionRunnerStore.DeleteRunner(ctx, allocation.RunnerID)
	}
	return s.sessionRunnerStore.DeleteAllocation(ctx, sessionID)
}

func (s *Server) startRouteStatusRelay(controller interface{ StartRouteStatusRelay(context.Context) }) {
	if s.routeStatusBus == nil {
		return
	}
	if s.routeStatusCancel != nil {
		s.routeStatusCancel()
	}
	ctx, cancel := context.WithCancel(context.Background())
	s.routeStatusCancel = cancel
	controller.StartRouteStatusRelay(ctx)
}

// Shutdown gracefully stops all running sessions and waits for them to terminate
func (s *Server) Shutdown(timeout time.Duration) error {
	if s.runtimeConfigCancel != nil {
		s.runtimeConfigCancel()
	}
	if s.routeStatusCancel != nil {
		s.routeStatusCancel()
	}
	var managerErr error
	if s.sessionManager != nil {
		managerErr = s.sessionManager.Shutdown(timeout)
	}
	var usageErr error
	if s.usageRepo != nil {
		usageErr = s.usageRepo.Close()
	}
	var sessionCountErr error
	if s.sessionCountRepo != nil {
		if s.sessionCountCancel != nil {
			s.sessionCountCancel()
		}
		s.sessionCountWorkers.Wait()
		sessionCountErr = s.sessionCountRepo.Close()
	}
	var notifierErr error
	if s.sessionAllocationRedis != nil {
		notifierErr = s.sessionAllocationRedis.Close()
	}
	if s.kvStore != nil {
		return errors.Join(managerErr, usageErr, sessionCountErr, notifierErr, s.kvStore.Close())
	}
	return errors.Join(managerErr, usageErr, sessionCountErr, notifierErr)
}

// GetEcho returns the Echo instance for external access
func (s *Server) GetEcho() *echo.Echo {
	return s.echo
}

// GetConfig returns the server configuration
func (s *Server) GetConfig() *config.Config {
	if s.configProvider != nil {
		return s.configProvider.Current()
	}
	return s.config
}

func (s *Server) GetConfigProvider() *runtimeconfig.Provider { return s.configProvider }

// GetNotificationService returns the notification service
func (s *Server) GetNotificationService() *notification.Service {
	return s.notificationSvc
}

// GetSettingsRepository returns the settings repository
func (s *Server) GetSettingsRepository() portrepos.SettingsRepository {
	return s.settingsRepo
}

// GetSessionProfileRepository returns the session profile repository
func (s *Server) GetSessionProfileRepository() portrepos.SessionProfileRepository {
	return s.sessionProfileRepo
}

// ExtractRepositoryInfo extracts repository information from tags.
// This is a public function that can be used by other packages (e.g., schedule).
// The cloneDir parameter is typically the session ID.
