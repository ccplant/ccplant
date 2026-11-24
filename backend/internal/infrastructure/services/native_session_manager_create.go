package services

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"syscall"
	"time"

	"github.com/takutakahashi/agentapi-proxy/internal/domain/entities"
)

type nativeSessionDirectories struct {
	root, home, workdir, runtime, build, temporary, herdrRuntime string
}

type nativeSessionPorts struct {
	agent, provisioner int
}

type nativeSessionMonitor struct {
	id      string
	session *NativeSession
	command *exec.Cmd
	logFile *os.File
	root    string
}

func prepareNativeSessionDirectories(stateDirectory, id string) (nativeSessionDirectories, error) {
	root := filepath.Join(stateDirectory, "sessions", id)
	paths := nativeSessionDirectories{
		root: root, home: filepath.Join(root, "home"), workdir: filepath.Join(root, "workdir"),
		runtime: filepath.Join(root, "runtime"), build: filepath.Join(root, "build"),
		temporary: filepath.Join(root, "tmp"), herdrRuntime: nativeHerdrRuntimeDir(id),
	}
	for _, directory := range []string{paths.home, paths.workdir, paths.runtime, paths.build, paths.temporary, paths.herdrRuntime} {
		if err := os.MkdirAll(directory, 0o700); err != nil {
			return paths, errors.Join(fmt.Errorf("create session directory %s: %w", directory, err), paths.cleanup())
		}
	}
	if err := os.Chmod(paths.herdrRuntime, 0o700); err != nil {
		return paths, errors.Join(fmt.Errorf("secure native herdr runtime directory: %w", err), paths.cleanup())
	}
	return paths, nil
}

func (p nativeSessionDirectories) cleanup() error {
	return errors.Join(os.RemoveAll(p.root), os.RemoveAll(p.herdrRuntime))
}

func reserveNativeSessionPorts() (nativeSessionPorts, error) {
	agentPort, err := reserveTCPPort()
	if err != nil {
		return nativeSessionPorts{}, err
	}
	provisionerPort, err := reserveTCPPort()
	return nativeSessionPorts{agent: agentPort, provisioner: provisionerPort}, err
}

func (m *NativeSessionManager) launchNativeSession(id string, request *entities.RunServerRequest, ports nativeSessionPorts, paths nativeSessionDirectories) (*NativeSession, error) {
	now := time.Now().UTC()
	sessionContext, cancel := context.WithCancel(context.Background())
	session := &NativeSession{
		id: id, request: request, rootDir: paths.root, agentPort: ports.agent,
		provisionerPort: ports.provisioner, startedAt: now, updatedAt: now,
		lastMessageAt: now, status: "creating", description: request.InitialMessage, cancel: cancel,
	}
	provisionRequest := &ProvisionRequest{
		RequestID: id + "-provision-1", SessionID: id, Type: provisionRequestType,
		Settings: request.ProvisionSettings, Status: "pending", UpdatedAt: now,
	}
	command, logFile, err := m.prepareNativeProvisionerCommand(sessionContext, id, ports, paths)
	if err != nil {
		cancel()
		return nil, err
	}
	m.mu.Lock()
	m.sessions[id], m.provisionRequests[id] = session, provisionRequest
	m.mu.Unlock()
	if err := command.Start(); err != nil {
		m.removeFailedNativeSession(id, cancel)
		return nil, errors.Join(fmt.Errorf("start agent-provisioner: %w", err), logFile.Close())
	}
	session.cmd, session.pid = command, command.Process.Pid
	if err := m.persistSession(session); err != nil {
		session.mu.Lock()
		session.status = "terminating"
		session.mu.Unlock()
		m.removeFailedNativeSession(id, cancel)
		terminateErr := terminateNativeProcessGroup(command.Process.Pid, paths.root)
		forceErr := command.Process.Kill()
		if errors.Is(forceErr, os.ErrProcessDone) {
			forceErr = nil
		}
		return nil, errors.Join(err, terminateErr, forceErr, command.Wait(), logFile.Close())
	}
	go m.monitorNativeSession(nativeSessionMonitor{id: id, session: session, command: command, logFile: logFile, root: paths.root})
	return session, nil
}

func (m *NativeSessionManager) removeFailedNativeSession(id string, cancel context.CancelFunc) {
	cancel()
	m.mu.Lock()
	delete(m.sessions, id)
	delete(m.provisionRequests, id)
	m.mu.Unlock()
}

func (m *NativeSessionManager) prepareNativeProvisionerCommand(ctx context.Context, id string, ports nativeSessionPorts, paths nativeSessionDirectories) (*exec.Cmd, *os.File, error) {
	logPath := filepath.Join(paths.runtime, "provisioner.log")
	logFile, err := os.OpenFile(logPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return nil, nil, fmt.Errorf("open provisioner log: %w", err)
	}
	command, err := m.newProvisionerCommand(ctx, paths.root, paths.runtime, ports.provisioner)
	if err != nil {
		return nil, nil, errors.Join(err, logFile.Close())
	}
	command.Dir = paths.workdir
	command.Env = nativeProvisionerEnvironment(os.Environ(),
		"HOME="+paths.home,
		"XDG_RUNTIME_DIR="+paths.herdrRuntime,
		"TMPDIR="+paths.temporary,
		"CFFIXED_USER_HOME="+paths.home,
		"AGENTAPI_NATIVE_SESSION_ROOT="+paths.root,
		"AGENTAPI_BUILD_DIR="+paths.build,
		"CCPLANT_BINARY_PATH="+m.binaryPath,
		"AGENTAPI_SESSION_ID="+id,
		"AGENTAPI_PORT="+strconv.Itoa(ports.agent),
		"PROVISIONER_PROXY_URL="+m.proxyURL,
		"PROVISIONER_TOKEN="+deriveSessionControlToken(m.provisionerToken, id),
		"SESSION_CONTROL_TOKEN="+deriveSessionControlToken(m.provisionerToken, id),
		"PROVISIONER_UPSTREAM_AUTH_TOKEN="+m.upstreamAuthToken,
		"POD_NAME=native-"+id,
		"POD_NAMESPACE=native",
		"PROVISIONER_PRE_SCRIPT=true",
	)
	command.Stdout = logFile
	command.Stderr = logFile
	command.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	return command, logFile, nil
}

func (m *NativeSessionManager) monitorNativeSession(monitor nativeSessionMonitor) {
	waitErr := monitor.command.Wait()
	closeErr := monitor.logFile.Close()
	monitor.session.mu.Lock()
	cleanup := monitor.session.status != "stopped" && monitor.session.status != "terminating"
	if cleanup {
		monitor.session.status = "error"
		if err := errors.Join(waitErr, closeErr); err != nil {
			monitor.session.statusMessage = err.Error()
		}
	}
	monitor.session.updatedAt = time.Now().UTC()
	monitor.session.mu.Unlock()
	if !cleanup {
		return
	}
	m.mu.Lock()
	delete(m.provisionRequests, monitor.id)
	m.mu.Unlock()
	if err := cleanupNativeSessionDirs(monitor.root); err != nil {
		m.recordNativeSessionCleanupFailure(monitor.session, err)
	}
}

func (m *NativeSessionManager) recordNativeSessionCleanupFailure(session *NativeSession, cleanupErr error) {
	session.mu.Lock()
	session.statusMessage = "session cleanup failed: " + cleanupErr.Error()
	session.updatedAt = time.Now().UTC()
	session.mu.Unlock()
	if err := m.persistSession(session); err != nil {
		session.mu.Lock()
		session.statusMessage += "; persist cleanup failure: " + err.Error()
		session.mu.Unlock()
	}
}
