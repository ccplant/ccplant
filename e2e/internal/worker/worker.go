package worker

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/exec"
	"regexp"
	"syscall"
	"time"

	"github.com/ccplant/ccplant/e2e/internal/domain"
)

type Worker struct {
	API, ID, Root string
	Client        *http.Client
}
type claim struct {
	Run      domain.Run      `json:"run"`
	Scenario domain.Scenario `json:"scenario"`
	Target   domain.Target   `json:"target"`
}

func (w *Worker) Run(ctx context.Context) error {
	if w.Client == nil {
		w.Client = &http.Client{Timeout: 10 * time.Second}
	}
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		c, ok, err := w.claim(ctx)
		if err != nil {
			time.Sleep(2 * time.Second)
			continue
		}
		if !ok {
			time.Sleep(time.Second)
			continue
		}
		w.execute(ctx, c)
	}
}
func (w *Worker) claim(ctx context.Context) (claim, bool, error) {
	var out claim
	request := workerAPIRequest{method: http.MethodPost, path: "/internal/v1/runs/claim", input: map[string]any{"worker_id": w.ID, "kinds": []string{"go-test", "playwright"}}, output: &out}
	status, err := w.call(ctx, request)
	return out, status == 200, err
}
func (w *Worker) execute(parent context.Context, c claim) {
	timeout, err := time.ParseDuration(c.Scenario.Timeout)
	if err != nil {
		w.finish(parent, c.Run.ID, "infra_error")
		return
	}
	ctx, cancel := context.WithTimeout(parent, timeout)
	defer cancel()
	cmd, err := w.commandForClaim(ctx, c)
	if err != nil {
		w.event(parent, c.Run.ID, domain.Event{Type: "error", Message: err.Error()})
		w.finish(parent, c.Run.ID, "infra_error")
		return
	}
	cmd.Env = append(os.Environ(), parameters(c.Run.Parameters)...)
	cmd.Env = append(cmd.Env, "E2E_TARGET_API_URL="+c.Target.APIURL, "E2E_TARGET_FRONTEND_URL="+c.Target.FrontendURL, "E2E_AUTH_TYPE="+c.Target.AuthType, "E2E_AUTH_VALUE="+c.Target.AuthValue)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		w.event(parent, c.Run.ID, domain.Event{Type: "error", Message: err.Error()})
		w.finish(parent, c.Run.ID, "infra_error")
		return
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		w.event(parent, c.Run.ID, domain.Event{Type: "error", Message: err.Error()})
		w.finish(parent, c.Run.ID, "infra_error")
		return
	}
	if err := cmd.Start(); err != nil {
		w.event(parent, c.Run.ID, domain.Event{Type: "error", Message: err.Error()})
		w.finish(parent, c.Run.ID, "infra_error")
		return
	}
	execution := claimExecution{worker: w, parent: parent, claim: c, cmd: cmd, stdout: stdout, stderr: stderr}
	execution.monitor(ctx, cancel)
}

type claimExecution struct {
	worker         *Worker
	parent         context.Context
	claim          claim
	cmd            *exec.Cmd
	stdout, stderr io.Reader
}

func (e claimExecution) monitor(ctx context.Context, cancel context.CancelFunc) {
	done := make(chan error, 1)
	cancelled := false
	go func() { done <- e.cmd.Wait() }()
	go e.worker.scan(e.parent, e.claim.Run.ID, e.stdout)
	go e.worker.scan(e.parent, e.claim.Run.ID, e.stderr)
	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case err := <-done:
			e.finish(ctx, err, cancelled)
			return
		case <-ticker.C:
			requested, err := e.heartbeat()
			if err != nil || requested {
				cancelled = requested
				cancel()
				e.terminate()
			}
		}
	}
}

func (e claimExecution) finish(ctx context.Context, commandErr error, cancelled bool) {
	status := "passed"
	if cancelled {
		status = "cancelled"
	} else if commandErr != nil {
		status = "failed"
	}
	if errors.Is(ctx.Err(), context.DeadlineExceeded) {
		status = "infra_error"
	}
	e.worker.finish(e.parent, e.claim.Run.ID, status)
}

func (e claimExecution) heartbeat() (bool, error) {
	var heartbeat struct {
		Cancel bool `json:"cancel_requested"`
	}
	request := workerAPIRequest{method: http.MethodPost, path: "/internal/v1/runs/" + e.claim.Run.ID + "/heartbeat", input: map[string]string{"worker_id": e.worker.ID}, output: &heartbeat}
	_, err := e.worker.call(e.parent, request)
	return heartbeat.Cancel, err
}

func (e claimExecution) terminate() {
	if e.cmd.Process == nil {
		return
	}
	if err := e.cmd.Process.Signal(syscall.SIGTERM); err != nil {
		log.Printf("failed to terminate cancelled worker command: %v", err)
	}
}

func (w *Worker) commandForClaim(ctx context.Context, c claim) (*exec.Cmd, error) {
	var cmd *exec.Cmd
	switch c.Scenario.Kind {
	case "go-test":
		if !safeTest(c.Scenario.Test) {
			return nil, fmt.Errorf("unsafe Go test name %q", c.Scenario.Test)
		}
		cmd = exec.CommandContext(ctx, "go", "test", "-json", "-tags=e2e", c.Scenario.Package, "-run", "^"+c.Scenario.Test+"$", "-count=1", "-timeout", c.Scenario.Timeout)
	case "playwright":
		cmd = exec.CommandContext(ctx, "npx", "playwright", "test", "--grep", c.Scenario.Tag, "--reporter=json")
		cmd.Dir = w.Root + "/scenarios/frontend"
	default:
		return nil, fmt.Errorf("unsupported scenario kind %q", c.Scenario.Kind)
	}
	if cmd.Dir == "" {
		cmd.Dir = w.Root
	}
	return cmd, nil
}
func (w *Worker) scan(ctx context.Context, id string, r io.Reader) {
	s := bufio.NewScanner(r)
	buf := make([]byte, 64*1024)
	s.Buffer(buf, 1024*1024)
	for s.Scan() {
		line := s.Text()
		typ, test := "output", ""
		var event struct{ Action, Test, Output string }
		if json.Unmarshal([]byte(line), &event) == nil && event.Action != "" {
			typ = event.Action
			test = event.Test
			if event.Output != "" {
				line = event.Output
			}
		}
		w.event(ctx, id, domain.Event{Type: typ, Message: line, Test: test})
	}
}
func (w *Worker) event(ctx context.Context, id string, event domain.Event) {
	request := workerAPIRequest{method: http.MethodPost, path: "/internal/v1/runs/" + id + "/events", input: map[string]any{"events": []domain.Event{event}}}
	if _, err := w.call(ctx, request); err != nil {
		log.Printf("failed to report worker event: %v", err)
	}
}
func (w *Worker) finish(ctx context.Context, id, status string) {
	request := workerAPIRequest{method: http.MethodPost, path: "/internal/v1/runs/" + id + "/complete", input: map[string]string{"worker_id": w.ID, "status": status}}
	if _, err := w.call(ctx, request); err != nil {
		log.Printf("failed to report worker completion: %v", err)
	}
}

type workerAPIRequest struct {
	method, path string
	input        any
	output       any
}

func (w *Worker) call(ctx context.Context, request workerAPIRequest) (int, error) {
	var body io.Reader
	if request.input != nil {
		b, err := json.Marshal(request.input)
		if err != nil {
			return 0, fmt.Errorf("encode worker API request: %w", err)
		}
		body = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, request.method, w.API+request.path, body)
	if err != nil {
		return 0, fmt.Errorf("create worker API request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	res, err := w.Client.Do(req)
	if err != nil {
		return 0, err
	}
	defer res.Body.Close()
	if request.output != nil && res.StatusCode != http.StatusNoContent {
		if err := json.NewDecoder(res.Body).Decode(request.output); err != nil {
			return res.StatusCode, err
		}
	}
	if res.StatusCode >= 400 {
		return res.StatusCode, fmt.Errorf("api status %d", res.StatusCode)
	}
	return res.StatusCode, nil
}

var testPattern = regexp.MustCompile(`^Test[A-Za-z0-9_]+$`)

func safeTest(v string) bool { return testPattern.MatchString(v) }
func parameters(in map[string]any) []string {
	out := []string{}
	for k, v := range in {
		key := "E2E_PARAM_" + regexp.MustCompile(`[^A-Za-z0-9]`).ReplaceAllString(stringsUpper(k), "_")
		out = append(out, key+"="+fmt.Sprint(v))
	}
	return out
}
func stringsUpper(v string) string {
	b := []byte(v)
	for i, c := range b {
		if c >= 'a' && c <= 'z' {
			b[i] = c - 32
		}
	}
	return string(b)
}
