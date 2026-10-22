package cmd

import (
	"context"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/labstack/echo/v4"
	"github.com/spf13/cobra"
	"github.com/takutakahashi/agentapi-proxy/internal/buildinfo"
	"github.com/takutakahashi/agentapi-proxy/internal/infrastructure/services"
	infrasessioncontrol "github.com/takutakahashi/agentapi-proxy/internal/infrastructure/sessioncontrol"
	"github.com/takutakahashi/agentapi-proxy/internal/interfaces/controllers"
	"github.com/takutakahashi/agentapi-proxy/internal/modules/sessionmanager"
)

var NativeSessionManagerCmd = &cobra.Command{
	Use:   "native-session-manager",
	Short: "Run an External Session Manager using native host processes",
	RunE:  runNativeSessionManager,
}

var nativeSessionManagerOptions struct {
	listen, upstreamURL, connectionToken, upstreamAuthToken, stateDir, binaryPath, managerID, configPath string
	filesystemSandbox                                                                                    bool
	inheritRuntimeProfile                                                                                bool
}

type nativeFilesystemSandboxConfig struct {
	Enabled bool `json:"enabled"`
}

type nativeDaemonConfig struct {
	Listen                string                        `json:"listen"`
	UpstreamURL           string                        `json:"upstream_url"`
	ConnectionToken       string                        `json:"connection_token"`
	CredentialsPath       string                        `json:"credentials_path,omitempty"`
	UpstreamAuthToken     string                        `json:"upstream_auth_token,omitempty"`
	StateDir              string                        `json:"state_dir"`
	BinaryPath            string                        `json:"binary_path,omitempty"`
	ManagerID             string                        `json:"manager_id,omitempty"`
	Name                  string                        `json:"name,omitempty"`
	InstanceID            string                        `json:"instance_id,omitempty"`
	Scope                 string                        `json:"scope,omitempty"`
	TeamID                string                        `json:"team_id,omitempty"`
	Labels                map[string]string             `json:"labels,omitempty"`
	ManagerEnvironment    map[string]string             `json:"manager_environment,omitempty"`
	Version               string                        `json:"version,omitempty"`
	FilesystemSandbox     nativeFilesystemSandboxConfig `json:"filesystem_sandbox,omitempty"`
	InheritRuntimeProfile bool                          `json:"inherit_runtime_profile,omitempty"`
}

func init() {
	f := NativeSessionManagerCmd.Flags()
	f.StringVar(&nativeSessionManagerOptions.listen, "listen", ":8080", "HTTP listen address")
	f.StringVar(&nativeSessionManagerOptions.upstreamURL, "upstream-url", "", "parent agentapi-proxy URL")
	f.StringVar(&nativeSessionManagerOptions.connectionToken, "connection-token", "", "ESM connection/HMAC token")
	f.StringVar(&nativeSessionManagerOptions.upstreamAuthToken, "upstream-auth-token", "", "optional parent proxy authentication token")
	f.StringVar(&nativeSessionManagerOptions.stateDir, "state-dir", "./native-sessions", "native session state directory")
	f.StringVar(&nativeSessionManagerOptions.binaryPath, "binary", "", "agentapi-proxy binary used for provisioners")
	f.StringVar(&nativeSessionManagerOptions.managerID, "manager-id", "", "registered external session manager ID")
	f.StringVar(&nativeSessionManagerOptions.configPath, "config", "", "JSON daemon configuration file")
	f.BoolVar(&nativeSessionManagerOptions.filesystemSandbox, "filesystem-sandbox", false, "sandbox native session filesystem access on macOS")
	f.BoolVar(&nativeSessionManagerOptions.inheritRuntimeProfile, "inherit-runtime-profile", false, "apply runtime profile received from the parent proxy")
}

func runNativeSessionManager(command *cobra.Command, _ []string) error {
	o := nativeSessionManagerOptions
	if o.configPath != "" {
		cfg, err := readNativeConfig(o.configPath)
		if err != nil {
			return fmt.Errorf("read native daemon config: %w", err)
		}
		if !command.Flags().Changed("listen") {
			o.listen = cfg.Listen
		}
		if !command.Flags().Changed("upstream-url") {
			o.upstreamURL = cfg.UpstreamURL
		}
		if !command.Flags().Changed("connection-token") {
			o.connectionToken = cfg.ConnectionToken
		}
		if !command.Flags().Changed("upstream-auth-token") {
			o.upstreamAuthToken = cfg.UpstreamAuthToken
		}
		if !command.Flags().Changed("state-dir") {
			o.stateDir = cfg.StateDir
		}
		if !command.Flags().Changed("binary") {
			o.binaryPath = cfg.BinaryPath
		}
		if !command.Flags().Changed("manager-id") {
			o.managerID = cfg.ManagerID
		}
		if !command.Flags().Changed("filesystem-sandbox") {
			o.filesystemSandbox = cfg.FilesystemSandbox.Enabled
		}
		if !command.Flags().Changed("inherit-runtime-profile") {
			o.inheritRuntimeProfile = cfg.InheritRuntimeProfile
		}
	}
	if o.upstreamURL == "" || o.connectionToken == "" {
		return fmt.Errorf("--upstream-url and --connection-token are required")
	}
	localURL, err := nativeSessionManagerLocalURL(o.listen)
	if err != nil {
		return err
	}
	manager, err := services.NewNativeSessionManager(o.stateDir, localURL, o.connectionToken, o.upstreamAuthToken, o.binaryPath, o.filesystemSandbox)
	if err != nil {
		return err
	}

	e := echo.New()
	e.HideBanner = true
	provisionerController := controllers.NewProvisionerController(manager, nil, nil, nil)
	e.POST("/internal/session-provisioners/connect", provisionerController.Connect)
	e.GET("/internal/session-provisioners/:sessionId/provision-requests", provisionerController.GetProvisionRequest)
	e.POST("/internal/session-provisioners/:sessionId/provision-requests/:requestId/status", provisionerController.UpdateProvisionRequestStatus)

	handlers := sessionmanager.NewHandlers(manager, o.connectionToken)
	if err := handlers.RegisterRoutes(e); err != nil {
		return err
	}
	e.GET("/healthz", func(c echo.Context) error { return c.JSON(http.StatusOK, map[string]string{"status": "ok"}) })
	registerNativeManagementRoutes(e, o, manager, []byte(o.connectionToken))
	registerNativeSessionControlRoutes(e, manager)

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	worker := sessionmanager.NewRunnerWorker(manager, o.upstreamURL, o.managerID, o.connectionToken)
	go worker.Start(ctx)
	controlWorker := sessionmanager.NewControlWorker(o.upstreamURL, o.connectionToken, o.upstreamAuthToken, localURL, o.managerID, o.connectionToken)
	go controlWorker.Start(ctx)
	go func() {
		<-ctx.Done()
		shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer shutdownCancel()
		_ = e.Shutdown(shutdownCtx)
	}()
	log.Printf("[NATIVE_ESM] listening on %s, upstream=%s, transport=outbound-control", o.listen, o.upstreamURL)
	if err := e.Start(o.listen); err != nil && err != http.ErrServerClosed {
		return err
	}
	return nil
}

func nativeSessionManagerLocalURL(listen string) (string, error) {
	host, port, err := net.SplitHostPort(listen)
	if err != nil {
		return "", fmt.Errorf("invalid native session manager listen address %q: %w", listen, err)
	}
	if host == "" || host == "0.0.0.0" || host == "::" {
		host = "127.0.0.1"
	}
	return "http://" + net.JoinHostPort(host, port), nil
}

func nativeBuildVersion() string {
	return buildinfo.Version
}

func registerNativeSessionControlRoutes(e *echo.Echo, manager *services.NativeSessionManager) {
	control := controllers.NewSessionControlController(infrasessioncontrol.NewMemoryStore(), manager)
	e.GET("/internal/session-control/:sessionId/commands", control.WaitCommands)
	e.POST("/internal/session-control/:sessionId/events", control.AppendEvents)
}
