package cmd

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"
	"github.com/takutakahashi/agentapi-proxy/pkg/executorproxy"
)

const defaultExecutorSocket = "/run/ccplant-executor/executor.sock"
const executorManagedFilePathsEnv = "CCPLANT_EXECUTOR_MANAGED_FILE_PATHS"

var (
	executorSocket        = defaultExecutorSocket
	executorShell         = "/bin/bash"
	executorWorkspaceRoot = "/home/agentapi/workdir"
)

var ExecutorServerCmd = &cobra.Command{
	Use:   "executor-server",
	Short: "Run commands received over the session executor Unix socket",
	RunE: func(cmd *cobra.Command, _ []string) error {
		return runExecutorServer(cmd.Context(), executorSocket, executorShell, executorWorkspaceRoot)
	},
}

var ExecutorClientCmd = &cobra.Command{
	Use:                "executor-client [shell arguments...]",
	Short:              "Run a shell through the session executor Unix socket",
	DisableFlagParsing: true,
	RunE: func(_ *cobra.Command, args []string) error {
		return runExecutorClient(args)
	},
}

func init() {
	ExecutorServerCmd.Flags().StringVar(&executorSocket, "socket", defaultExecutorSocket, "Unix socket path")
	ExecutorServerCmd.Flags().StringVar(&executorShell, "shell", "/bin/bash", "real shell executable")
	ExecutorServerCmd.Flags().StringVar(&executorWorkspaceRoot, "workspace-root", "/home/agentapi/workdir", "allowed working directory root")
}

func runExecutorServer(ctx context.Context, socket, shell, workspaceRoot string) error {
	if err := os.MkdirAll(filepath.Dir(socket), 0o770); err != nil {
		return err
	}
	if err := os.Remove(socket); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("remove stale executor socket: %w", err)
	}
	listener, err := net.Listen("unix", socket)
	if err != nil {
		return err
	}
	defer func() {
		if err := listener.Close(); err != nil && !errors.Is(err, net.ErrClosed) {
			log.Printf("close executor listener: %v", err)
		}
	}()
	if err := os.Chmod(socket, 0o660); err != nil {
		return err
	}
	go func() {
		<-ctx.Done()
		if err := listener.Close(); err != nil && !errors.Is(err, net.ErrClosed) {
			log.Printf("close canceled executor listener: %v", err)
		}
	}()
	for {
		conn, err := listener.Accept()
		if err != nil {
			if ctx.Err() != nil {
				return nil
			}
			return err
		}
		go handleExecutorConnection(ctx, conn, shell, workspaceRoot)
	}
}

func withinWorkspace(path, root string) bool {
	clean, err := filepath.Abs(path)
	if err != nil {
		return false
	}
	root, err = filepath.Abs(root)
	if err != nil {
		return false
	}
	return clean == root || strings.HasPrefix(clean, root+string(filepath.Separator))
}

func readExecutorManagedFiles(encodedPaths string) ([]executorproxy.ManagedFile, error) {
	if strings.TrimSpace(encodedPaths) == "" {
		return nil, nil
	}
	var paths []string
	if err := json.Unmarshal([]byte(encodedPaths), &paths); err != nil {
		return nil, fmt.Errorf("decode executor managed file paths: %w", err)
	}
	files := make([]executorproxy.ManagedFile, 0, len(paths))
	for _, path := range paths {
		info, err := os.Stat(path)
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				files = append(files, executorproxy.ManagedFile{Path: path, Absent: true})
				continue
			}
			return nil, fmt.Errorf("stat executor managed file %s: %w", path, err)
		}
		if !info.Mode().IsRegular() {
			return nil, fmt.Errorf("executor managed file %s is not a regular file", path)
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return nil, fmt.Errorf("read executor managed file %s: %w", path, err)
		}
		files = append(files, executorproxy.ManagedFile{Path: path, Data: data, Mode: uint32(info.Mode().Perm())})
	}
	return files, nil
}

func hydrateExecutorManagedFiles(files []executorproxy.ManagedFile) error {
	for _, file := range files {
		if !filepath.IsAbs(file.Path) {
			return fmt.Errorf("executor managed file path must be absolute: %s", file.Path)
		}
		if file.Absent {
			if err := os.Remove(file.Path); err != nil && !errors.Is(err, os.ErrNotExist) {
				return fmt.Errorf("remove absent executor managed file %s: %w", file.Path, err)
			}
			continue
		}
		if err := os.MkdirAll(filepath.Dir(file.Path), 0o755); err != nil {
			return fmt.Errorf("create executor managed file directory %s: %w", file.Path, err)
		}
		mode := os.FileMode(file.Mode) & os.ModePerm
		if mode == 0 {
			mode = 0o600
		}
		if err := os.WriteFile(file.Path, file.Data, mode); err != nil {
			return fmt.Errorf("write executor managed file %s: %w", file.Path, err)
		}
	}
	return nil
}

type executorExitError struct{ code int }

func (e *executorExitError) Error() string {
	return fmt.Sprintf("executor command exited with code %d", e.code)
}

// ExecutorExitCode preserves the remote command's exit status through Cobra.
func ExecutorExitCode(err error) (int, bool) {
	var exitErr *executorExitError
	if errors.As(err, &exitErr) {
		return exitErr.code, true
	}
	return 0, false
}

func getenvDefault(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}

// ExecutorClientArgsForBasename dispatches an invocation through the proxy when
// the ccplant binary is mounted over a shell executable.
func ExecutorClientArgsForBasename(argv []string) ([]string, bool) {
	if len(argv) == 0 {
		return nil, false
	}
	base := filepath.Base(argv[0])
	return argv[1:], base == "bash" || base == "sh"
}
