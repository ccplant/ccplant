package cmd

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"os"
	"os/exec"
	"os/signal"
	"strconv"
	"sync"
	"syscall"
	"time"

	"github.com/takutakahashi/agentapi-proxy/pkg/executorproxy"
)

type executorProcess struct {
	command *exec.Cmd
	stdin   io.WriteCloser
	stdout  io.ReadCloser
	stderr  io.ReadCloser
}

func handleExecutorConnection(parent context.Context, conn net.Conn, shell, workspaceRoot string) {
	defer closeExecutorConnection(conn)
	request, err := readExecutorRequest(conn, workspaceRoot)
	if err != nil {
		writeExecutorExit(conn, executorproxy.Exit{Code: 126, Error: err.Error()})
		return
	}
	ctx, cancel := context.WithCancel(parent)
	defer cancel()
	process, err := startExecutorProcess(ctx, shell, request)
	if err != nil {
		writeExecutorExit(conn, executorproxy.Exit{Code: 126, Error: err.Error()})
		return
	}
	serveExecutorProcess(conn, process)
}

func closeExecutorConnection(conn net.Conn) {
	if err := conn.Close(); err != nil {
		log.Printf("close executor connection: %v", err)
	}
}

func readExecutorRequest(conn net.Conn, workspaceRoot string) (executorproxy.Request, error) {
	var request executorproxy.Request
	if err := executorproxy.ReadJSON(conn, &request); err != nil {
		return request, err
	}
	if len(request.Args) == 0 {
		request.Args = []string{"bash"}
	}
	if !withinWorkspace(request.Cwd, workspaceRoot) {
		return request, errors.New("working directory is outside executor workspace")
	}
	if err := hydrateExecutorManagedFiles(request.Files); err != nil {
		return request, err
	}
	return request, nil
}

func startExecutorProcess(ctx context.Context, shell string, request executorproxy.Request) (executorProcess, error) {
	process := executorProcess{command: exec.CommandContext(ctx, shell, request.Args[1:]...)}
	process.command.Dir = request.Cwd
	process.command.Env = request.Env
	process.command.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	var err error
	if process.stdin, err = process.command.StdinPipe(); err != nil {
		return process, err
	}
	if process.stdout, err = process.command.StdoutPipe(); err != nil {
		return process, err
	}
	if process.stderr, err = process.command.StderrPipe(); err != nil {
		return process, err
	}
	return process, process.command.Start()
}

func serveExecutorProcess(conn net.Conn, process executorProcess) {
	var writeMu sync.Mutex
	var outputWG sync.WaitGroup
	outputWG.Add(2)
	go copyExecutorOutput(conn, executorproxy.FrameStdout, process.stdout, &writeMu, &outputWG)
	go copyExecutorOutput(conn, executorproxy.FrameStderr, process.stderr, &writeMu, &outputWG)
	go forwardExecutorInput(conn, process, &writeMu)
	waitErr := process.command.Wait()
	outputWG.Wait()
	writeMu.Lock()
	writeExecutorExit(conn, executorProcessExit(waitErr))
	writeMu.Unlock()
}

func copyExecutorOutput(conn net.Conn, kind byte, reader io.Reader, writeMu *sync.Mutex, outputWG *sync.WaitGroup) {
	defer outputWG.Done()
	buffer := make([]byte, 32*1024)
	for {
		n, readErr := reader.Read(buffer)
		if n > 0 {
			writeMu.Lock()
			err := executorproxy.WriteFrame(conn, kind, buffer[:n])
			writeMu.Unlock()
			if err != nil {
				log.Printf("write executor output frame: %v", err)
			}
		}
		if readErr != nil {
			return
		}
	}
}

func forwardExecutorInput(conn net.Conn, process executorProcess, writeMu *sync.Mutex) {
	for {
		kind, data, err := executorproxy.ReadFrame(conn)
		if err != nil {
			logProcessSignal(process.command, syscall.SIGTERM, "terminate disconnected executor process")
			return
		}
		switch kind {
		case executorproxy.FrameStdin:
			if _, err := process.stdin.Write(data); err != nil {
				log.Printf("write executor stdin: %v", err)
			}
		case executorproxy.FrameStdinEOF:
			if err := process.stdin.Close(); err != nil {
				log.Printf("close executor stdin: %v", err)
			}
		case executorproxy.FrameSignal:
			if number, parseErr := strconv.Atoi(string(data)); parseErr == nil {
				logProcessSignal(process.command, syscall.Signal(number), "signal executor process")
			}
		}
	}
}

func logProcessSignal(process *exec.Cmd, signal syscall.Signal, action string) {
	if err := syscall.Kill(-process.Process.Pid, signal); err != nil && !errors.Is(err, syscall.ESRCH) {
		log.Printf("%s: %v", action, err)
	}
}

func executorProcessExit(waitErr error) executorproxy.Exit {
	if waitErr == nil {
		return executorproxy.Exit{}
	}
	var exitErr *exec.ExitError
	if !errors.As(waitErr, &exitErr) {
		return executorproxy.Exit{Code: 126, Error: waitErr.Error()}
	}
	exit := executorproxy.Exit{Code: exitErr.ExitCode()}
	if status, ok := exitErr.Sys().(syscall.WaitStatus); ok && status.Signaled() {
		exit.Signal = status.Signal().String()
	}
	return exit
}

func writeExecutorExit(writer io.Writer, exit executorproxy.Exit) {
	data, err := json.Marshal(exit)
	if err != nil {
		log.Printf("encode executor exit: %v", err)
		return
	}
	if err := executorproxy.WriteFrame(writer, executorproxy.FrameExit, data); err != nil {
		log.Printf("write executor exit: %v", err)
	}
}

func runExecutorClient(args []string) error {
	conn, err := connectExecutorClient()
	if err != nil {
		if os.Getenv("CCPLANT_EXECUTOR_REQUIRED") != "1" {
			realShell := getenvDefault("CCPLANT_EXECUTOR_REAL_SHELL", "/bin/bash")
			return syscall.Exec(realShell, append([]string{"bash"}, args...), os.Environ())
		}
		return fmt.Errorf("executor socket unavailable: %w", err)
	}
	defer closeExecutorConnection(conn)
	if err := sendExecutorRequest(conn, args); err != nil {
		return err
	}
	var writeMu sync.Mutex
	go forwardClientInput(conn, &writeMu)
	stopSignals := forwardClientSignals(conn, &writeMu)
	defer stopSignals()
	return receiveExecutorOutput(conn)
}

func connectExecutorClient() (net.Conn, error) {
	socket := getenvDefault("CCPLANT_EXECUTOR_SOCKET", defaultExecutorSocket)
	deadline := time.Now().Add(30 * time.Second)
	for {
		conn, err := net.DialTimeout("unix", socket, time.Second)
		if err == nil || os.Getenv("CCPLANT_EXECUTOR_REQUIRED") != "1" || time.Now().After(deadline) {
			return conn, err
		}
		time.Sleep(200 * time.Millisecond)
	}
}

func sendExecutorRequest(conn net.Conn, args []string) error {
	cwd, err := os.Getwd()
	if err != nil {
		return err
	}
	files, err := readExecutorManagedFiles(os.Getenv(executorManagedFilePathsEnv))
	if err != nil {
		return err
	}
	request := executorproxy.Request{Args: append([]string{"bash"}, args...), Cwd: cwd, Env: os.Environ(), Files: files}
	return executorproxy.WriteJSON(conn, request)
}

func forwardClientInput(conn net.Conn, writeMu *sync.Mutex) {
	buffer := make([]byte, 32*1024)
	for {
		n, readErr := os.Stdin.Read(buffer)
		writeMu.Lock()
		if n > 0 {
			if err := executorproxy.WriteFrame(conn, executorproxy.FrameStdin, buffer[:n]); err != nil {
				log.Printf("forward executor stdin: %v", err)
			}
		}
		if readErr != nil {
			if err := executorproxy.WriteFrame(conn, executorproxy.FrameStdinEOF, nil); err != nil {
				log.Printf("forward executor stdin EOF: %v", err)
			}
			writeMu.Unlock()
			return
		}
		writeMu.Unlock()
	}
}

func forwardClientSignals(conn net.Conn, writeMu *sync.Mutex) func() {
	signals := make(chan os.Signal, 4)
	signal.Notify(signals, os.Interrupt, syscall.SIGTERM, syscall.SIGHUP)
	go func() {
		for received := range signals {
			unixSignal, ok := received.(syscall.Signal)
			if !ok {
				continue
			}
			writeMu.Lock()
			err := executorproxy.WriteFrame(conn, executorproxy.FrameSignal, []byte(strconv.Itoa(int(unixSignal))))
			writeMu.Unlock()
			if err != nil {
				log.Printf("forward executor signal: %v", err)
			}
		}
	}()
	return func() { signal.Stop(signals) }
}

func receiveExecutorOutput(conn net.Conn) error {
	for {
		kind, data, err := executorproxy.ReadFrame(conn)
		if err != nil {
			return err
		}
		switch kind {
		case executorproxy.FrameStdout:
			if _, err := os.Stdout.Write(data); err != nil {
				return fmt.Errorf("write executor stdout: %w", err)
			}
		case executorproxy.FrameStderr:
			if _, err := os.Stderr.Write(data); err != nil {
				return fmt.Errorf("write executor stderr: %w", err)
			}
		case executorproxy.FrameExit:
			return decodeExecutorExit(data)
		}
	}
}

func decodeExecutorExit(data []byte) error {
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
