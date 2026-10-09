package cmd

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/spf13/cobra"
	"github.com/takutakahashi/agentapi-proxy/pkg/executorproxy"
)

const defaultExecutorSocket = "/run/ccplant-executor/executor.sock"

var ExecutorServerCmd = &cobra.Command{
	Use:   "executor-server",
	Short: "Run commands received over the session executor Unix socket",
	RunE: func(cmd *cobra.Command, _ []string) error {
		socket, _ := cmd.Flags().GetString("socket")
		shell, _ := cmd.Flags().GetString("shell")
		workspaceRoot, _ := cmd.Flags().GetString("workspace-root")
		return runExecutorServer(cmd.Context(), socket, shell, workspaceRoot)
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
	ExecutorServerCmd.Flags().String("socket", defaultExecutorSocket, "Unix socket path")
	ExecutorServerCmd.Flags().String("shell", "/bin/bash", "real shell executable")
	ExecutorServerCmd.Flags().String("workspace-root", "/home/agentapi/workdir", "allowed working directory root")
}

func runExecutorServer(ctx context.Context, socket, shell, workspaceRoot string) error {
	if err := os.MkdirAll(filepath.Dir(socket), 0o770); err != nil {
		return err
	}
	_ = os.Remove(socket)
	listener, err := net.Listen("unix", socket)
	if err != nil {
		return err
	}
	defer func() { _ = listener.Close() }()
	if err := os.Chmod(socket, 0o660); err != nil {
		return err
	}
	go func() {
		<-ctx.Done()
		_ = listener.Close()
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

func handleExecutorConnection(parent context.Context, conn net.Conn, shell, workspaceRoot string) {
	defer func() { _ = conn.Close() }()
	var request executorproxy.Request
	if err := executorproxy.ReadJSON(conn, &request); err != nil {
		writeExecutorExit(conn, executorproxy.Exit{Code: 126, Error: err.Error()})
		return
	}
	if len(request.Args) == 0 {
		request.Args = []string{"bash"}
	}
	if !withinWorkspace(request.Cwd, workspaceRoot) {
		writeExecutorExit(conn, executorproxy.Exit{Code: 126, Error: "working directory is outside executor workspace"})
		return
	}
	ctx, cancel := context.WithCancel(parent)
	defer cancel()
	process := exec.CommandContext(ctx, shell, request.Args[1:]...)
	process.Dir = request.Cwd
	process.Env = request.Env
	process.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	stdin, err := process.StdinPipe()
	if err != nil {
		writeExecutorExit(conn, executorproxy.Exit{Code: 126, Error: err.Error()})
		return
	}
	stdout, _ := process.StdoutPipe()
	stderr, _ := process.StderrPipe()
	if err := process.Start(); err != nil {
		writeExecutorExit(conn, executorproxy.Exit{Code: 126, Error: err.Error()})
		return
	}
	var writeMu sync.Mutex
	copyOutput := func(kind byte, reader io.Reader) {
		buffer := make([]byte, 32*1024)
		for {
			n, readErr := reader.Read(buffer)
			if n > 0 {
				writeMu.Lock()
				_ = executorproxy.WriteFrame(conn, kind, buffer[:n])
				writeMu.Unlock()
			}
			if readErr != nil {
				return
			}
		}
	}
	var outputWG sync.WaitGroup
	outputWG.Add(2)
	go func() { defer outputWG.Done(); copyOutput(executorproxy.FrameStdout, stdout) }()
	go func() { defer outputWG.Done(); copyOutput(executorproxy.FrameStderr, stderr) }()
	go func() {
		for {
			kind, data, readErr := executorproxy.ReadFrame(conn)
			if readErr != nil {
				_ = syscall.Kill(-process.Process.Pid, syscall.SIGTERM)
				return
			}
			switch kind {
			case executorproxy.FrameStdin:
				_, _ = stdin.Write(data)
			case executorproxy.FrameStdinEOF:
				_ = stdin.Close()
			case executorproxy.FrameSignal:
				if number, parseErr := strconv.Atoi(string(data)); parseErr == nil {
					_ = syscall.Kill(-process.Process.Pid, syscall.Signal(number))
				}
			}
		}
	}()
	waitErr := process.Wait()
	outputWG.Wait()
	exit := executorproxy.Exit{}
	if waitErr != nil {
		var exitErr *exec.ExitError
		if errors.As(waitErr, &exitErr) {
			exit.Code = exitErr.ExitCode()
			if status, ok := exitErr.Sys().(syscall.WaitStatus); ok && status.Signaled() {
				exit.Signal = status.Signal().String()
			}
		} else {
			exit.Code, exit.Error = 126, waitErr.Error()
		}
	}
	writeMu.Lock()
	writeExecutorExit(conn, exit)
	writeMu.Unlock()
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

func writeExecutorExit(w io.Writer, exit executorproxy.Exit) {
	data, _ := json.Marshal(exit)
	_ = executorproxy.WriteFrame(w, executorproxy.FrameExit, data)
}

func runExecutorClient(args []string) error {
	socket := getenvDefault("CCPLANT_EXECUTOR_SOCKET", defaultExecutorSocket)
	deadline := time.Now().Add(30 * time.Second)
	var conn net.Conn
	var err error
	for {
		conn, err = net.DialTimeout("unix", socket, time.Second)
		if err == nil || os.Getenv("CCPLANT_EXECUTOR_REQUIRED") != "1" || time.Now().After(deadline) {
			break
		}
		time.Sleep(200 * time.Millisecond)
	}
	if err != nil {
		if os.Getenv("CCPLANT_EXECUTOR_REQUIRED") != "1" {
			realShell := getenvDefault("CCPLANT_EXECUTOR_REAL_SHELL", "/bin/bash")
			return syscall.Exec(realShell, append([]string{"bash"}, args...), os.Environ())
		}
		return fmt.Errorf("executor socket unavailable: %w", err)
	}
	defer func() { _ = conn.Close() }()
	cwd, err := os.Getwd()
	if err != nil {
		return err
	}
	request := executorproxy.Request{Args: append([]string{"bash"}, args...), Cwd: cwd, Env: os.Environ()}
	if err := executorproxy.WriteJSON(conn, request); err != nil {
		return err
	}
	var writeMu sync.Mutex
	go func() {
		buffer := make([]byte, 32*1024)
		for {
			n, readErr := os.Stdin.Read(buffer)
			writeMu.Lock()
			if n > 0 {
				_ = executorproxy.WriteFrame(conn, executorproxy.FrameStdin, buffer[:n])
			}
			if readErr != nil {
				_ = executorproxy.WriteFrame(conn, executorproxy.FrameStdinEOF, nil)
				writeMu.Unlock()
				return
			}
			writeMu.Unlock()
		}
	}()
	sigCh := make(chan os.Signal, 4)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM, syscall.SIGHUP)
	defer signal.Stop(sigCh)
	go func() {
		for sig := range sigCh {
			if unixSig, ok := sig.(syscall.Signal); ok {
				writeMu.Lock()
				_ = executorproxy.WriteFrame(conn, executorproxy.FrameSignal, []byte(strconv.Itoa(int(unixSig))))
				writeMu.Unlock()
			}
		}
	}()
	for {
		kind, data, readErr := executorproxy.ReadFrame(conn)
		if readErr != nil {
			return readErr
		}
		switch kind {
		case executorproxy.FrameStdout:
			_, _ = os.Stdout.Write(data)
		case executorproxy.FrameStderr:
			_, _ = os.Stderr.Write(data)
		case executorproxy.FrameExit:
			var exit executorproxy.Exit
			if err := json.Unmarshal(data, &exit); err != nil {
				return err
			}
			if exit.Error != "" {
				return errors.New(exit.Error)
			}
			if exit.Code != 0 {
				return &executorExitError{code: exit.Code}
			}
			return nil
		}
	}
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
