package controllers

import (
	"context"
	"encoding/json"
	"fmt"
	"log"

	core "github.com/takutakahashi/agentapi-proxy/internal/core/sessionrunner"
	"github.com/takutakahashi/agentapi-proxy/internal/usecases/ports/repositories"
	"github.com/takutakahashi/agentapi-proxy/pkg/sessionsettings"
)

type sessionRestartOperation struct {
	controller    *SessionController
	store         sessionConfigurationStore
	config        *core.Configuration
	old           *sessionsettings.SessionSettings
	next          *sessionsettings.SessionSettings
	route         *repositories.SessionRoute
	input         sessionRuntimeChangeInput
	pause         bool
	requestID     string
	raw           []byte
	refresh       func(context.Context) (*sessionsettings.SessionSettings, error)
	previousPhase string
}

func (operation *sessionRestartOperation) run(ctx context.Context, cancel context.CancelFunc) {
	defer cancel()
	if err := operation.controller.waitRestartIdle(ctx, operation.config.SessionID, operation.route, operation.input.BusyPolicy); err != nil {
		operation.config.Phase = operation.previousPhase
		operation.config.Error = "Could not finish the current turn; stop it and retry."
		operation.save(context.WithoutCancel(ctx))
		return
	}
	if err := operation.pauseCurrentRuntime(ctx); err != nil {
		operation.fail(ctx, err)
		return
	}
	if operation.pause {
		operation.config.Phase = "paused"
		operation.save(context.WithoutCancel(ctx))
		return
	}
	if err := operation.refreshRestartSettings(ctx); err != nil {
		operation.fail(ctx, err)
		return
	}
	if err := operation.restart(ctx); err != nil {
		operation.fail(ctx, err)
		return
	}
	operation.config.Phase = "ready"
	operation.config.Error = ""
	operation.save(context.WithoutCancel(ctx))
}

func (operation *sessionRestartOperation) pauseCurrentRuntime(ctx context.Context) error {
	operation.config.Phase = "pausing"
	if err := operation.store.SaveConfiguration(ctx, operation.config); err != nil {
		return err
	}
	if operation.route != nil {
		return operation.controller.sendRestartToManager(ctx, managerRestartRequest{
			route: operation.route, action: "pause", id: operation.requestID, settings: operation.old,
		})
	}
	manager, ok := operation.controller.getSessionManager().(repositories.SessionRestarter)
	if !ok {
		return fmt.Errorf("unsupported manager")
	}
	return manager.PauseSession(ctx, operation.config.SessionID)
}

func (operation *sessionRestartOperation) refreshRestartSettings(ctx context.Context) error {
	if operation.refresh == nil {
		return nil
	}
	next, err := operation.refresh(ctx)
	if err != nil {
		return err
	}
	if err := sessionsettings.ValidateRestart(operation.old, next); err != nil {
		return err
	}
	next.Session.ID = operation.old.Session.ID
	next.Session.PersistenceEnabled = operation.old.Session.PersistenceEnabled
	next.ParentRuntime = operation.old.ParentRuntime
	next.Session.ResumeFrom = next.Session.ID
	next.Restart = true
	next.InitialMessage = ""
	next.WebhookPayload = ""
	sessionsettings.PrepareRestart(operation.old, next)
	raw, err := json.Marshal(next)
	if err != nil {
		return err
	}
	operation.next = next
	operation.raw = raw
	return nil
}

func (operation *sessionRestartOperation) restart(ctx context.Context) error {
	operation.config.Phase = "restarting"
	operation.config.Settings = operation.raw
	if err := operation.store.SaveConfiguration(ctx, operation.config); err != nil {
		return err
	}
	if operation.route != nil {
		if err := operation.store.UpdateProvisionSettings(ctx, operation.config.SessionID, operation.raw); err != nil {
			return err
		}
		return operation.controller.sendRestartToManager(ctx, managerRestartRequest{
			route: operation.route, action: "restart", id: operation.requestID, settings: operation.next,
		})
	}
	manager, ok := operation.controller.getSessionManager().(repositories.SessionRestarter)
	if !ok {
		return fmt.Errorf("unsupported manager")
	}
	return manager.RestartSession(ctx, operation.config.SessionID, operation.requestID, operation.next)
}

func (operation *sessionRestartOperation) fail(ctx context.Context, err error) {
	log.Printf("[SESSION_RESTART] session=%s request=%s failed (%T)", operation.config.SessionID, operation.config.RequestID, err)
	operation.config.Phase = "failed"
	operation.config.Error = "Session operation failed; check runtime status and retry."
	operation.save(context.WithoutCancel(ctx))
}

func (operation *sessionRestartOperation) save(ctx context.Context) {
	if err := operation.store.SaveConfiguration(ctx, operation.config); err != nil {
		log.Printf("[SESSION_RESTART] session=%s request=%s failed to save operation state: %v", operation.config.SessionID, operation.config.RequestID, err)
	}
}
