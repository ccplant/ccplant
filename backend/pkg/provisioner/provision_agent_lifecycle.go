package provisioner

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"strings"
	"syscall"
	"time"

	"github.com/takutakahashi/agentapi-proxy/pkg/sessionsettings"
)

const provisionAgentAPIURL = "http://unix"

type provisionedAgent struct {
	command *exec.Cmd
	done    chan error
	stderr  *bytes.Buffer
	socket  string
}

func (s *Server) startProvisionedAgent(ctx context.Context, settings *sessionsettings.SessionSettings, envMap map[string]string, exited chan struct{}) (*provisionedAgent, error) {
	agentSocket := localAgentSocketPath()
	envMap["AGENTAPI_LOCAL_SOCKET"] = agentSocket
	if !usesACPUnixSocket(settings.Session.AgentType) {
		return nil, fmt.Errorf("agent type %q does not support the required Unix-only AgentAPI transport", settings.Session.AgentType)
	}
	agentCmd, agentArgs := s.buildAgentCommand(settings, envMap)
	agentEnv := sessionProcessEnvironment(os.Environ(), settings.UnsetEnv, envMap)
	agentEnv = prepareHerdrAgentEnvironment(ctx, agentEnv, settings.Session.AgentType, agentSocket)
	if shouldRequireConversationResume(settings) {
		agentArgs = append([]string{agentArgs[0], "--require-resume"}, agentArgs[1:]...)
	}
	agentCmd, agentArgs, err := wrapAgentCommand(settings, agentCmd, agentArgs)
	if err != nil {
		return nil, err
	}
	command, stderr := newAgentCommand(ctx, agentCmd, agentArgs, agentEnv)
	if err := command.Start(); err != nil {
		return nil, fmt.Errorf("failed to start agent: %w", err)
	}
	log.Printf("[PROVISIONER] Agent process started (pid %d)", command.Process.Pid)
	agent := &provisionedAgent{command: command, done: make(chan error, 1), stderr: stderr, socket: agentSocket}
	go waitForProvisionedAgentExit(agent, exited)
	return agent, nil
}

func prepareHerdrAgentEnvironment(ctx context.Context, agentEnv []string, agentType, agentSocket string) []string {
	parentID, err := ensureHerdrNotificationBridge(ctx, agentEnv, agentType, agentSocket)
	if err != nil {
		log.Printf("[PROVISIONER] Warning: failed to start herdr notification bridge: %v", err)
		return agentEnv
	}
	log.Printf("[PROVISIONER] Herdr notifications will be delivered through parent terminal %s", parentID)
	return mergeEnv(agentEnv, map[string]string{"HC_TERMINAL_ID": parentID})
}

func wrapAgentCommand(settings *sessionsettings.SessionSettings, command string, args []string) (string, []string, error) {
	if settings.Startup.CommandWrapperTemplate == "" {
		log.Printf("[PROVISIONER] Starting agent: %s %v", command, args)
		return command, args, nil
	}
	rendered, err := sessionsettings.RenderCommandWrapper(settings.Startup.CommandWrapperTemplate, append([]string{command}, args...))
	if err != nil {
		return "", nil, fmt.Errorf("failed to render command wrapper: %w", err)
	}
	log.Printf("[PROVISIONER] Starting agent through session profile command wrapper")
	return "/bin/sh", []string{"-c", rendered}, nil
}

func newAgentCommand(ctx context.Context, command string, args, environment []string) (*exec.Cmd, *bytes.Buffer) {
	cmd := exec.CommandContext(ctx, command, args...)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error { return syscall.Kill(-cmd.Process.Pid, syscall.SIGTERM) }
	cmd.WaitDelay = 10 * time.Second
	cmd.Env = environment
	cmd.Stdout = os.Stdout
	stderr := &bytes.Buffer{}
	cmd.Stderr = io.MultiWriter(os.Stderr, stderr)
	return cmd, stderr
}

func waitForProvisionedAgentExit(agent *provisionedAgent, exited chan struct{}) {
	agent.done <- agent.command.Wait()
	if err := syscall.Kill(-agent.command.Process.Pid, syscall.SIGKILL); err != nil && !errors.Is(err, syscall.ESRCH) {
		log.Printf("[PROVISIONER] Failed to kill remaining agent process group: %v", err)
	}
	close(exited)
}

func (s *Server) waitForProvisionedAgent(ctx context.Context, agent *provisionedAgent) bool {
	s.setPhase("provision:wait-agentapi-ready")
	log.Printf("[PROVISIONER] Waiting for agentapi to be ready at unix:%s", agent.socket)
	readinessCtx, cancelReadiness := context.WithCancel(ctx)
	readyResult := make(chan error, 1)
	go func() {
		readyResult <- waitForAgentAPIWithClient(readinessCtx, unixSocketHTTPClient(agent.socket), provisionAgentAPIURL, 120)
	}()
	select {
	case readinessErr := <-readyResult:
		cancelReadiness()
		return s.handleAgentReadinessResult(agent, readinessErr)
	case processErr := <-agent.done:
		cancelReadiness()
		s.setProvisionAgentError(agent.stderr.String(), fmt.Sprintf("agent process exited before becoming ready: %v", processErr))
		return false
	}
}

func (s *Server) handleAgentReadinessResult(agent *provisionedAgent, readinessErr error) bool {
	if readinessErr == nil {
		log.Printf("[PROVISIONER] agentapi is ready")
		return true
	}
	if err := agent.command.Process.Kill(); err != nil && !errors.Is(err, os.ErrProcessDone) {
		log.Printf("[PROVISIONER] Failed to stop unready agent process: %v", err)
	}
	if processErr := <-agent.done; processErr != nil {
		log.Printf("[PROVISIONER] Unready agent exited with an error: %v", processErr)
	}
	s.setProvisionAgentError(agent.stderr.String(), fmt.Sprintf("agentapi not ready: %v", readinessErr))
	return false
}

func (s *Server) setProvisionAgentError(stderr, fallback string) {
	if classifyProvisionError(stderr) == codexAuthRequiredErrorCode {
		s.setStatus(StatusError, "Codex authentication required")
		return
	}
	s.setStatus(StatusError, fallback)
}

func (s *Server) sendProvisionInitialMessage(ctx context.Context, settings *sessionsettings.SessionSettings) {
	if settings.InitialMessage == "" || settings.Restart {
		return
	}
	s.setPhase("provision:send-initial-message")
	log.Printf("[PROVISIONER] Sending initial message")
	waitSeconds := 2
	if value := os.Getenv("INITIAL_MESSAGE_WAIT_SECOND"); value != "" {
		if _, err := fmt.Sscanf(value, "%d", &waitSeconds); err != nil {
			log.Printf("[PROVISIONER] Warning: invalid INITIAL_MESSAGE_WAIT_SECOND=%q: %v", value, err)
		}
	}
	resumed := strings.TrimSpace(settings.Session.ResumeFrom) != ""
	request := initialMessageRequest{agentapiURL: provisionAgentAPIURL, message: settings.InitialMessage, agentType: settings.Session.AgentType, waitSec: waitSeconds, restored: resumed, markerPath: initialMessageMarkerPath(runtimeHome, settings.Session.ID)}
	sendInitialMessage(ctx, request)
}

func (s *Server) superviseProvisionedAgent(ctx context.Context, agentDone chan error) {
	err := <-agentDone
	if ctx.Err() != nil {
		return
	}
	if err != nil {
		s.setStatus(StatusError, fmt.Sprintf("agent process exited: %v", err))
		return
	}
	s.setStatus(StatusError, "agent process exited with code 0")
}
