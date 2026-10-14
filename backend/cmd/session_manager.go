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
	"github.com/takutakahashi/agentapi-proxy/internal/buildinfo"
	"github.com/takutakahashi/agentapi-proxy/pkg/telemetry"
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
	SessionManagerCmd.AddCommand(newSessionManagerInstallCommand())
	SessionManagerCmd.Flags().StringVarP(&sessionManagerPort, "port", "p", "8080", "Port to listen on")
	SessionManagerCmd.Flags().StringVarP(&sessionManagerConfigPath, "config", "c", "config.json", "Configuration file path")
	SessionManagerCmd.Flags().BoolVarP(&sessionManagerVerbose, "verbose", "v", false, "Enable verbose logging")
}

func runSessionManager(_ *cobra.Command, _ []string) error {
	shutdownTelemetry, err := telemetry.Setup(context.Background())
	if err != nil {
		return err
	}
	defer func() {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = shutdownTelemetry(shutdownCtx)
	}()

	cfg, err := loadRuntimeConfig(sessionManagerConfigPath)
	if err != nil {
		return err
	}
	// The session workload must run the exact same release as the manager.
	// Do not trust a deployment or parent-provided version override here: the
	// linked build version is the authoritative identity of this binary.
	cfg.SessionManager.CurrentVersion = buildinfo.Version
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	runtime, err := app.NewSessionManagerRuntime(ctx, cfg, sessionManagerVerbose)
	if err != nil {
		return err
	}
	go func() {
		if err := runtime.Echo().Start(":" + sessionManagerPort); err != nil && err != http.ErrServerClosed {
			log.Printf("Session manager failed: %v", err)
			cancel()
		}
	}()
	<-ctx.Done()
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer shutdownCancel()
	if err := runtime.Echo().Shutdown(shutdownCtx); err != nil {
		return err
	}
	return runtime.Shutdown(25 * time.Second)
}
