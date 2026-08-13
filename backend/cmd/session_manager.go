package cmd

import (
	"context"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/spf13/cobra"
	"github.com/takutakahashi/agentapi-proxy/internal/app"
	"github.com/takutakahashi/agentapi-proxy/internal/modules/sessionmanager"
)

var (
	sessionManagerPort       string
	sessionManagerConfigPath string
	sessionManagerVerbose    bool
)

// SessionManagerCmd runs only the session-manager API and its upstream control loops.
var SessionManagerCmd = &cobra.Command{
	Use:   "session-manager",
	Short: "Start the AgentAPI session manager",
	Args:  cobra.NoArgs,
	RunE:  runSessionManager,
}

func init() {
	SessionManagerCmd.Flags().StringVarP(&sessionManagerPort, "port", "p", "8080", "Port to listen on")
	SessionManagerCmd.Flags().StringVarP(&sessionManagerConfigPath, "config", "c", "config.json", "Configuration file path")
	SessionManagerCmd.Flags().BoolVarP(&sessionManagerVerbose, "verbose", "v", false, "Enable verbose logging")
}

func runSessionManager(_ *cobra.Command, _ []string) error {
	cfg, err := loadRuntimeConfig(sessionManagerConfigPath)
	if err != nil {
		return err
	}
	runtime := app.NewServer(cfg, sessionManagerVerbose)
	cfg = runtime.GetConfig()
	manager := runtime.GetSessionManager()
	if manager == nil {
		return nil
	}

	// Register directly on Echo so this process owns the session-manager endpoint.
	if err := sessionmanager.NewHandlers(manager, cfg.SessionManager.HMACSecret).RegisterRoutes(runtime.GetEcho()); err != nil {
		return err
	}
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	startSessionAllocator(cfg, runtime)
	startSessionManagerAllocator(ctx, cfg, runtime)
	go func() {
		if err := runtime.GetEcho().Start(":" + sessionManagerPort); err != nil && err != http.ErrServerClosed {
			log.Printf("Session manager failed: %v", err)
			cancel()
		}
	}()
	<-ctx.Done()
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer shutdownCancel()
	if err := runtime.GetEcho().Shutdown(shutdownCtx); err != nil {
		return err
	}
	return runtime.Shutdown(25 * time.Second)
}
