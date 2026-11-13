// Package herdr provides a deliberately small, local-only terminal control API.
// It never starts a server, resumes an agent, or exposes arbitrary Herdr RPCs.
package herdr

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"strings"
	"time"
)

var ErrUnavailable = errors.New("herdr is unavailable")
var ErrChanged = errors.New("agent changed or is not waiting for input; refresh before sending")

const maxResponse = 2 << 20

type Agent struct {
	ServerID   string `json:"server_id"`
	ServerName string `json:"server_name"`
	Runtime    string `json:"runtime"`
	PaneID     string `json:"pane_id"`
	TerminalID string `json:"terminal_id"`
	Agent      string `json:"agent"`
	Status     string `json:"status"`
	Label      string `json:"label"`
	SessionID  string `json:"session_id,omitempty"`
	Generation string `json:"generation"`
	CanInput   bool   `json:"can_input"`
}

type Target struct {
	ServerID   string `json:"server_id"`
	PaneID     string `json:"pane_id"`
	Generation string `json:"generation"`
}

type endpoint struct{ path, name, runtime string }

type Client struct {
	// Tests inject discovery; HTTP callers can never provide socket paths.
	discover func() []endpoint
}

func NewClient() *Client { return &Client{discover: discoverEndpoints} }

func digest(s string) string  { return fmt.Sprintf("%x", sha256.Sum256([]byte(s))) }
func (e endpoint) id() string { return digest(e.path)[:16] }

func discoverEndpoints() []endpoint {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil
	}
	config := os.Getenv("XDG_CONFIG_HOME")
	if config == "" {
		config = filepath.Join(home, ".config")
	}
	state := os.Getenv("XDG_STATE_HOME")
	if state == "" {
		state = filepath.Join(home, ".local", "state")
	}
	var result []endpoint
	seen := map[string]bool{}
	add := func(path, name, runtime string) {
		if seen[path] || len(result) >= 32 {
			return
		}
		info, err := os.Lstat(path)
		if err != nil || info.Mode()&os.ModeSocket == 0 {
			return
		}
		seen[path] = true
		result = append(result, endpoint{path, name, runtime})
	}
	add(filepath.Join(config, "herdr", "herdr.sock"), "default", "herdr")
	paths, _ := filepath.Glob(filepath.Join(config, "herdr", "sessions", "*", "herdr.sock"))
	for _, p := range paths {
		add(p, filepath.Base(filepath.Dir(p)), "herdr")
	}
	// hc's documented Unix endpoint rules. Do not use `hc ls`: it can start
	// a core, and may include remote machines which are outside this Pod.
	for _, root := range []string{
		filepath.Join(os.Getenv("XDG_RUNTIME_DIR"), "herdr-core"),
		fmt.Sprintf("/run/user/%d/herdr-core", os.Getuid()),
		filepath.Join(state, "herdr-core", "*"),
	} {
		if !filepath.IsAbs(root) {
			continue
		}
		paths, _ := filepath.Glob(filepath.Join(root, "*.sock"))
		for _, p := range paths {
			name := strings.TrimSuffix(filepath.Base(p), ".sock")
			// Only client sockets, not holder sockets.
			if strings.HasPrefix(name, "holder") {
				continue
			}
			add(p, name, "core")
		}
	}
	if p := os.Getenv("HC_SOCKET"); filepath.IsAbs(p) {
		add(p, "configured", "core")
	}
	return result
}

func call(ctx context.Context, e endpoint, method string, params any, result any) error {
	conn, err := (&net.Dialer{}).DialContext(ctx, "unix", e.path)
	if err != nil {
		return ErrUnavailable
	}
	defer func() { _ = conn.Close() }()
	deadline := time.Now().Add(5 * time.Second)
	if method == "terminal.send" {
		// hc may wait for a human's draft (up to 60 seconds) before delivery.
		deadline = time.Now().Add(75 * time.Second)
	}
	if d, ok := ctx.Deadline(); ok && d.Before(deadline) {
		deadline = d
	}
	if err := conn.SetDeadline(deadline); err != nil {
		return err
	}
	stop := context.AfterFunc(ctx, func() { _ = conn.Close() })
	defer stop()
	request := map[string]any{"id": "ccplant", "method": method, "params": params}
	if e.runtime == "core" {
		request["jsonrpc"] = "2.0"
	}
	if err := json.NewEncoder(conn).Encode(request); err != nil {
		return ErrUnavailable
	}
	var response struct {
		Result json.RawMessage `json:"result"`
		Error  json.RawMessage `json:"error"`
	}
	if err := json.NewDecoder(io.LimitReader(conn, maxResponse)).Decode(&response); err != nil {
		return ErrUnavailable
	}
	if len(response.Error) > 0 && string(response.Error) != "null" {
		var rpcError struct {
			Data struct {
				Kind string `json:"kind"`
			} `json:"data"`
		}
		if json.Unmarshal(response.Error, &rpcError) == nil {
			switch rpcError.Data.Kind {
			case "status_mismatch", "blocked", "finished", "not_found":
				return ErrChanged
			}
		}
		return ErrUnavailable
	}
	if len(response.Result) == 0 || string(response.Result) == "null" {
		return ErrUnavailable
	}
	return json.Unmarshal(response.Result, result)
}

func knownAgent(s string) string {
	s = strings.ToLower(s)
	switch s {
	case "claude", "claude-code", "codex", "pi", "opencode", "omp", "cursor", "copilot", "aider":
		return s
	}
	return ""
}

func list(ctx context.Context, e endpoint) ([]Agent, error) {
	var agents []Agent
	if e.runtime == "core" {
		var response struct {
			Terminals []struct {
				Address string   `json:"address"`
				Name    string   `json:"name"`
				State   string   `json:"state"`
				Created uint64   `json:"created_ms"`
				PID     uint32   `json:"pid"`
				Argv    []string `json:"argv"`
				Keep    bool     `json:"keep"`
				Status  struct {
					App   string `json:"app"`
					State string `json:"state"`
				} `json:"status"`
				Foreground struct {
					Name string `json:"name"`
					PID  int    `json:"pid"`
				} `json:"foreground"`
			} `json:"terminals"`
		}
		if err := call(ctx, e, "terminal.list", map[string]any{}, &response); err != nil {
			return nil, err
		}
		for _, t := range response.Terminals {
			if t.State != "live" || strings.Contains(t.Address, "/") {
				continue
			}
			name := knownAgent(t.Status.App)
			if name == "" {
				name = knownAgent(t.Foreground.Name)
			}
			if name == "" && len(t.Argv) > 0 {
				name = knownAgent(filepath.Base(t.Argv[0]))
			}
			if name == "" {
				continue
			}
			status := t.Status.State
			if status == "" {
				status = "unknown"
			}
			// hc send may queue behind a draft. A direct, non-kept AI cannot
			// recover into a replacement shell while delivery is pending.
			canInput := !t.Keep && len(t.Argv) > 0 && knownAgent(filepath.Base(t.Argv[0])) != "" && t.PID != 0 && t.Foreground.PID == int(t.PID) && status == "idle"
			agents = append(agents, Agent{ServerID: e.id(), ServerName: e.name, Runtime: e.runtime, PaneID: t.Address, TerminalID: t.Address, Agent: name, Status: status, Label: t.Name, CanInput: canInput,
				Generation: digest(fmt.Sprintf("%s:%d:%d:%d:%s", t.Address, t.Created, t.PID, t.Foreground.PID, name))})
		}
	} else {
		var response struct {
			Panes []struct {
				PaneID     string `json:"pane_id"`
				TerminalID string `json:"terminal_id"`
				Agent      string `json:"agent"`
				Status     string `json:"agent_status"`
				Label      string `json:"label"`
				Session    struct {
					Kind  string `json:"kind"`
					Value string `json:"value"`
				} `json:"agent_session"`
			} `json:"panes"`
		}
		if err := call(ctx, e, "pane.list", map[string]any{}, &response); err != nil {
			return nil, err
		}
		for _, p := range response.Panes {
			if knownAgent(p.Agent) == "" {
				continue
			}
			sid := ""
			if p.Session.Kind == "id" {
				sid = p.Session.Value
			}
			canInput := p.Session.Value != "" && (p.Status == "idle" || p.Status == "done")
			agents = append(agents, Agent{ServerID: e.id(), ServerName: e.name, Runtime: e.runtime, PaneID: p.PaneID, TerminalID: p.TerminalID, Agent: p.Agent, Status: p.Status, Label: p.Label, SessionID: sid, CanInput: canInput,
				Generation: digest(p.TerminalID + ":" + p.Agent + ":" + p.Session.Value)})
		}
	}
	return agents, nil
}

func (c *Client) Agents(ctx context.Context) ([]Agent, int) {
	agents := []Agent{}
	unavailable := 0
	for _, e := range c.discover() {
		if ctx.Err() != nil {
			break
		}
		items, err := list(ctx, e)
		if err != nil {
			unavailable++
			continue
		}
		agents = append(agents, items...)
	}
	return agents, unavailable
}

func (c *Client) resolve(ctx context.Context, target Target) (endpoint, Agent, error) {
	for _, e := range c.discover() {
		if e.id() != target.ServerID {
			continue
		}
		agents, err := list(ctx, e)
		if err != nil {
			return e, Agent{}, err
		}
		for _, a := range agents {
			if a.PaneID == target.PaneID && a.Generation == target.Generation {
				return e, a, nil
			}
		}
		return e, Agent{}, ErrChanged
	}
	return endpoint{}, Agent{}, ErrChanged
}

func (c *Client) Output(ctx context.Context, target Target) (string, error) {
	e, _, err := c.resolve(ctx, target)
	if err != nil {
		return "", err
	}
	if e.runtime == "core" {
		var result struct {
			Text string `json:"text"`
		}
		// Screen only: hc may scroll a TUI when history is requested.
		err = call(ctx, e, "terminal.read", map[string]any{"terminal": target.PaneID, "lines": 0}, &result)
		return result.Text, err
	}
	var result struct {
		Read struct {
			Text string `json:"text"`
		} `json:"read"`
	}
	err = call(ctx, e, "pane.read", map[string]any{"pane_id": target.PaneID, "source": "recent_unwrapped", "lines": 150, "format": "text", "strip_ansi": true}, &result)
	return result.Read.Text, err
}

func (c *Client) Send(ctx context.Context, target Target, text string) error {
	e, a, err := c.resolve(ctx, target)
	if err != nil {
		return err
	}
	if !a.CanInput {
		return ErrChanged
	}
	var result json.RawMessage
	if e.runtime == "core" {
		// Native conditional send also checks status at delivery time. Never
		// force permission dialogs, and never fall back to a raw shell write.
		return call(ctx, e, "terminal.send", map[string]any{"terminal": a.PaneID, "text": text, "if": "idle", "wait": false}, &result)
	}
	return call(ctx, e, "pane.send_input", map[string]any{"pane_id": a.PaneID, "text": text, "keys": []string{"Enter"}}, &result)
}
