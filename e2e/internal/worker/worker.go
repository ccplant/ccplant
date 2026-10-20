package worker

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/ccplant/ccplant/e2e/internal/domain"
	"io"
	"net/http"
	"os"
	"os/exec"
	"regexp"
	"syscall"
	"time"
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
	status, err := w.call(ctx, "POST", "/internal/v1/runs/claim", map[string]any{"worker_id": w.ID, "kinds": []string{"go-test", "playwright"}}, &out)
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
	var cmd *exec.Cmd
	switch c.Scenario.Kind {
	case "go-test":
		if !safeTest(c.Scenario.Test) {
			w.finish(parent, c.Run.ID, "infra_error")
			return
		}
		cmd = exec.CommandContext(ctx, "go", "test", "-json", "-tags=e2e", c.Scenario.Package, "-run", "^"+c.Scenario.Test+"$", "-count=1", "-timeout", c.Scenario.Timeout)
	case "playwright":
		cmd = exec.CommandContext(ctx, "npx", "playwright", "test", "--grep", c.Scenario.Tag, "--reporter=json")
		cmd.Dir = w.Root + "/scenarios/frontend"
	default:
		w.finish(parent, c.Run.ID, "infra_error")
		return
	}
	if cmd.Dir == "" {
		cmd.Dir = w.Root
	}
	cmd.Env = append(os.Environ(), parameters(c.Run.Parameters)...)
	cmd.Env = append(cmd.Env, "E2E_TARGET_API_URL="+c.Target.APIURL, "E2E_TARGET_FRONTEND_URL="+c.Target.FrontendURL, "E2E_AUTH_TYPE="+c.Target.AuthType, "E2E_AUTH_VALUE="+c.Target.AuthValue)
	stdout, _ := cmd.StdoutPipe()
	stderr, _ := cmd.StderrPipe()
	if err := cmd.Start(); err != nil {
		w.event(parent, c.Run.ID, "error", err.Error(), "")
		w.finish(parent, c.Run.ID, "infra_error")
		return
	}
	done := make(chan error, 1)
	cancelled := false
	go func() { done <- cmd.Wait() }()
	go w.scan(parent, c.Run.ID, stdout)
	go w.scan(parent, c.Run.ID, stderr)
	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case err := <-done:
			status := "passed"
			if cancelled {
				status = "cancelled"
			} else if err != nil {
				status = "failed"
			}
			if errors.Is(ctx.Err(), context.DeadlineExceeded) {
				status = "infra_error"
			}
			w.finish(parent, c.Run.ID, status)
			return
		case <-ticker.C:
			var hb struct {
				Cancel bool `json:"cancel_requested"`
			}
			_, err := w.call(parent, "POST", "/internal/v1/runs/"+c.Run.ID+"/heartbeat", map[string]string{"worker_id": w.ID}, &hb)
			if err != nil || hb.Cancel {
				cancelled = hb.Cancel
				cancel()
				if cmd.Process != nil {
					cmd.Process.Signal(syscall.SIGTERM)
				}
			}
		}
	}
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
		w.event(ctx, id, typ, line, test)
	}
}
func (w *Worker) event(ctx context.Context, id, typ, msg, test string) {
	w.call(ctx, "POST", "/internal/v1/runs/"+id+"/events", map[string]any{"events": []domain.Event{{Type: typ, Message: msg, Test: test}}}, nil)
}
func (w *Worker) finish(ctx context.Context, id, status string) {
	w.call(ctx, "POST", "/internal/v1/runs/"+id+"/complete", map[string]string{"worker_id": w.ID, "status": status}, nil)
}
func (w *Worker) call(ctx context.Context, method, path string, in, out any) (int, error) {
	var body io.Reader
	if in != nil {
		b, _ := json.Marshal(in)
		body = bytes.NewReader(b)
	}
	req, _ := http.NewRequestWithContext(ctx, method, w.API+path, body)
	req.Header.Set("Content-Type", "application/json")
	res, err := w.Client.Do(req)
	if err != nil {
		return 0, err
	}
	defer res.Body.Close()
	if out != nil && res.StatusCode != 204 {
		if err := json.NewDecoder(res.Body).Decode(out); err != nil {
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
