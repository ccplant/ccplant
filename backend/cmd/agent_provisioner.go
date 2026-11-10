package cmd

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/spf13/cobra"
	"github.com/takutakahashi/agentapi-proxy/pkg/provisioner"
	"github.com/takutakahashi/agentapi-proxy/pkg/proxybinary"
)

// AgentProvisionerCmd is the "agent-provisioner" sub-command.
// It starts a local HTTP server (default :9001) for probes/status and pulls
// provision requests from the proxy internal API. The provisioner then:
//
//  1. Runs the full setup sequence (write-pem, clone-repo, compile, sync-extra)
//  2. Starts agentapi (or an ACP bridge) as a subprocess
//  3. Waits for agentapi to become ready
//  4. Sends the initial message (if any)
//
// On Pod restart, if --settings-file already exists (mounted from the K8s
// Secret), provisioning is triggered automatically as Pod restart recovery.
var AgentProvisionerCmd = &cobra.Command{
	Use:   "agent-provisioner",
	Short: "Pull-based provisioner for session Pods",
	Long: `Starts the session Pod provisioner.

Endpoints:
  GET  /livez     – liveness probe (always 200)
  GET  /healthz   – readiness probe (200 after startup prefetch)
  GET  /status    – current provisioning state as JSON

Provision requests are pulled from the proxy internal provisioner API.`,
	RunE: runAgentProvisioner,
}

func init() {
	AgentProvisionerCmd.Flags().Int("port", 9001,
		"TCP port for the provisioner HTTP server")
	AgentProvisionerCmd.Flags().String("settings-file",
		"/session-settings/settings.yaml",
		"Path to the session settings YAML file used for auto-provisioning on Pod restart")
}

func runAgentProvisioner(cmd *cobra.Command, args []string) error {
	if err := installAgentInstructions(); err != nil {
		return err
	}
	port, err := cmd.Flags().GetInt("port")
	if err != nil {
		return err
	}
	settingsFile, err := cmd.Flags().GetString("settings-file")
	if err != nil {
		return err
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	srv := provisioner.New(port, settingsFile)
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)
	defer signal.Stop(sigCh)
	go func() {
		sig := <-sigCh
		log.Printf("[PROVISIONER] Received signal %s; preparing persistent session for termination", sig)
		if strings.TrimSpace(os.Getenv("AGENTAPI_SESSION_STATE_VOLUME_PATH")) != "" {
			terminationCtx, terminationCancel := context.WithTimeout(context.Background(), 90*time.Second)
			err := preparePersistentSessionForTermination(
				terminationCtx,
				srv,
				runTerminationStateBackup,
				notifyTerminationSuspend,
			)
			terminationCancel()
			if err != nil {
				log.Printf("[PROVISIONER] Persistent session termination preparation failed: %v", err)
			}
		}
		cancel()
	}()

	errCh := make(chan error, 1)
	go func() {
		errCh <- srv.Start(ctx)
	}()

	pullErrCh := make(chan error, 1)
	go func() {
		pullErrCh <- provisioner.RunPullClient(ctx, srv, provisioner.PullClientConfig{
			ProxyURL:            os.Getenv("PROVISIONER_PROXY_URL"),
			LocalProxyURL:       os.Getenv("PROVISIONER_LOCAL_PROXY_URL"),
			Token:               os.Getenv("PROVISIONER_TOKEN"),
			SessionControlToken: os.Getenv("SESSION_CONTROL_TOKEN"),
			UpstreamAuthToken:   os.Getenv("PROVISIONER_UPSTREAM_AUTH_TOKEN"),
			SessionID:           os.Getenv("AGENTAPI_SESSION_ID"),
			PodName:             os.Getenv("POD_NAME"),
			Namespace:           os.Getenv("POD_NAMESPACE"),
			CAFile:              os.Getenv("NODE_EXTRA_CA_CERTS"),
			RunnerPool:          os.Getenv("AGENTAPI_SESSION_RUNNER_POOL"),
			RunnerID:            os.Getenv("AGENTAPI_SESSION_RUNNER_ID"),
			RunnerToken:         os.Getenv("AGENTAPI_SESSION_RUNNER_TOKEN"),
		})
	}()

	select {
	case err := <-errCh:
		return err
	case err := <-pullErrCh:
		return err
	case <-ctx.Done():
		return ctx.Err()
	}
}

type terminationPreparer interface {
	PrepareForTermination(context.Context) error
}

func preparePersistentSessionForTermination(
	ctx context.Context,
	preparer terminationPreparer,
	backup func(context.Context) error,
	suspend func(context.Context) error,
) error {
	var errs []error
	if err := preparer.PrepareForTermination(ctx); err != nil {
		errs = append(errs, fmt.Errorf("stop agent: %w", err))
	}
	if err := backup(ctx); err != nil {
		errs = append(errs, fmt.Errorf("backup session state: %w", err))
		if !sessionStateArchiveExists() {
			return errors.Join(errs...)
		}
	}
	if err := suspend(ctx); err != nil {
		errs = append(errs, fmt.Errorf("suspend terminating session: %w", err))
	}
	return errors.Join(errs...)
}

func sessionStateArchiveExists() bool {
	path := strings.TrimSpace(os.Getenv("AGENTAPI_SESSION_STATE_VOLUME_PATH"))
	if path == "" {
		return false
	}
	info, err := os.Stat(path)
	return err == nil && info.Mode().IsRegular()
}

func runTerminationStateBackup(ctx context.Context) error {
	binary := proxybinary.Resolve(os.Getenv(proxybinary.EnvName))
	output, err := exec.CommandContext(ctx, binary, "client", "backup-session-state").CombinedOutput()
	if err != nil {
		return fmt.Errorf("%w: %s", err, strings.TrimSpace(string(output)))
	}
	return nil
}

func notifyTerminationSuspend(ctx context.Context) error {
	proxy := strings.TrimRight(os.Getenv("PROVISIONER_PROXY_URL"), "/")
	token := os.Getenv("PROVISIONER_TOKEN")
	sessionID := os.Getenv("AGENTAPI_SESSION_ID")
	if proxy == "" || token == "" || sessionID == "" {
		return fmt.Errorf("PROVISIONER_PROXY_URL, PROVISIONER_TOKEN and AGENTAPI_SESSION_ID are required")
	}
	req, err := internalRequest(http.MethodPost, proxy+"/internal/session-state/"+sessionID+"/terminate", token, nil)
	if err != nil {
		return err
	}
	resp, err := (&http.Client{}).Do(req.WithContext(ctx))
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusNoContent {
		return fmt.Errorf("termination suspend failed: HTTP %d", resp.StatusCode)
	}
	return nil
}

func installAgentInstructions() error {
	home, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("resolve home directory for agent instructions: %w", err)
	}
	files := [][2]string{
		{"/tmp/config/CLAUDE.md", filepath.Join(home, ".claude", "CLAUDE.md")},
		{"/tmp/config/AGENTS.md", filepath.Join(home, ".codex", "AGENTS.md")},
	}
	for _, file := range files {
		data, err := os.ReadFile(file[0])
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return fmt.Errorf("read agent instructions %s: %w", file[0], err)
		}
		if err := os.MkdirAll(filepath.Dir(file[1]), 0o755); err != nil {
			return fmt.Errorf("create agent instructions directory: %w", err)
		}
		if err := os.WriteFile(file[1], data, 0o644); err != nil {
			return fmt.Errorf("write agent instructions %s: %w", file[1], err)
		}
	}
	return nil
}
