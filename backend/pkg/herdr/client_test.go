package herdr

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/labstack/echo/v4"
	"github.com/stretchr/testify/require"
)

func socket(t *testing.T, runtime string, handler func(string, map[string]any) any) endpoint {
	t.Helper()
	dir, err := os.MkdirTemp("", "hc-")
	require.NoError(t, err)
	p := filepath.Join(dir, "test.sock")
	l, err := net.Listen("unix", p)
	require.NoError(t, err)
	var wg sync.WaitGroup
	go func() {
		for {
			conn, err := l.Accept()
			if err != nil {
				return
			}
			wg.Add(1)
			go func() {
				defer wg.Done()
				defer func() { _ = conn.Close() }()
				var req struct {
					Method  string         `json:"method"`
					Params  map[string]any `json:"params"`
					JSONRPC string         `json:"jsonrpc"`
				}
				if json.NewDecoder(conn).Decode(&req) != nil {
					return
				}
				if runtime == "core" && req.JSONRPC != "2.0" {
					return
				}
				_ = json.NewEncoder(conn).Encode(map[string]any{"id": "ccplant", "result": handler(req.Method, req.Params)})
			}()
		}
	}()
	t.Cleanup(func() { _ = l.Close(); wg.Wait(); _ = os.RemoveAll(dir) })
	return endpoint{p, "test", runtime}
}

func TestHerdrListReadSend(t *testing.T) {
	var input map[string]any
	e := socket(t, "herdr", func(method string, p map[string]any) any {
		switch method {
		case "pane.list":
			return map[string]any{"panes": []any{
				map[string]any{"pane_id": "w1:p1", "terminal_id": "term_1", "agent": "claude", "agent_status": "idle", "label": "review", "agent_session": map[string]string{"kind": "id", "value": "native-id"}},
				map[string]any{"pane_id": "w1:p2", "terminal_id": "term_shell"},
			}}
		case "pane.read":
			require.Equal(t, "recent_unwrapped", p["source"])
			return map[string]any{"read": map[string]string{"text": "<script>not HTML</script>"}}
		case "pane.send_input":
			input = p
			return map[string]string{"type": "ok"}
		default:
			t.Errorf("unexpected method %s", method)
			return nil
		}
	})
	c := &Client{discover: func() []endpoint { return []endpoint{e} }}
	agents, unavailable := c.Agents(context.Background())
	require.Zero(t, unavailable)
	require.Len(t, agents, 1)
	a := agents[0]
	require.Equal(t, "native-id", a.SessionID)
	target := Target{a.ServerID, a.PaneID, a.Generation}
	text, err := c.Output(context.Background(), target)
	require.NoError(t, err)
	require.Contains(t, text, "<script>")
	require.NoError(t, c.Send(context.Background(), target, "hello\nworld"))
	require.Equal(t, "hello\nworld", input["text"])
	require.Equal(t, []any{"Enter"}, input["keys"])
	target.Generation = "stale"
	require.ErrorIs(t, c.Send(context.Background(), target, "must not send"), ErrChanged)
	target.ServerID = "/tmp/attacker.sock"
	require.ErrorIs(t, c.Send(context.Background(), target, "must not send"), ErrChanged)
}

func TestCoreOnlyLocalRecognizedAgents(t *testing.T) {
	var input map[string]any
	e := socket(t, "core", func(method string, p map[string]any) any {
		switch method {
		case "terminal.list":
			return map[string]any{"terminals": []any{
				map[string]any{"address": "t1", "state": "live", "created_ms": 1, "pid": 10, "argv": []string{"pi"}, "foreground": map[string]any{"pid": 10, "name": "pi"}, "status": map[string]string{"app": "pi", "state": "idle"}},
				map[string]any{"address": "remote/t2", "state": "live", "status": map[string]string{"app": "pi", "state": "idle"}},
				map[string]any{"address": "t3", "state": "finished", "status": map[string]string{"app": "pi", "state": "idle"}},
				map[string]any{"address": "t4", "state": "live", "foreground": map[string]string{"name": "sh"}},
			}}
		case "terminal.read":
			require.Zero(t, p["lines"])
			return map[string]string{"text": "screen only"}
		case "terminal.send":
			input = p
			return map[string]bool{"sent": true}
		default:
			t.Errorf("unexpected method %s", method)
			return nil
		}
	})
	c := &Client{discover: func() []endpoint { return []endpoint{e} }}
	a, _ := c.Agents(context.Background())
	require.Len(t, a, 1)
	target := Target{a[0].ServerID, a[0].PaneID, a[0].Generation}
	text, err := c.Output(context.Background(), target)
	require.NoError(t, err)
	require.Equal(t, "screen only", text)
	require.NoError(t, c.Send(context.Background(), target, "hello"))
	require.Equal(t, "idle", input["if"])
	require.NotContains(t, input, "force")
}

func TestBusyAndUnknownAgentsRejectInput(t *testing.T) {
	for _, status := range []string{"working", "blocked", "unknown", "error"} {
		t.Run(status, func(t *testing.T) {
			e := socket(t, "herdr", func(method string, _ map[string]any) any {
				require.Equal(t, "pane.list", method)
				return map[string]any{"panes": []any{map[string]string{"pane_id": "p", "terminal_id": "t", "agent": "pi", "agent_status": status}}}
			})
			c := &Client{discover: func() []endpoint { return []endpoint{e} }}
			a, _ := c.Agents(context.Background())
			require.ErrorIs(t, c.Send(context.Background(), Target{a[0].ServerID, "p", a[0].Generation}, "no"), ErrChanged)
		})
	}
}

func TestHTTPValidationAndNoStore(t *testing.T) {
	e := echo.New()
	register(e, &Client{discover: func() []endpoint { return nil }}, filepath.Join(t.TempDir(), "pending"))
	for _, body := range []string{`{"text":""}`, `{"text":"\u001b[31m"}`, `{"method":"server.stop","text":"hello"}`, strings.Repeat("x", 20001)} {
		r := httptest.NewRequest(http.MethodPost, "/herdr/input", strings.NewReader(body))
		r.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		e.ServeHTTP(w, r)
		require.Equal(t, http.StatusBadRequest, w.Code)
	}
	w := httptest.NewRecorder()
	e.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/herdr/agents", nil))
	require.Equal(t, http.StatusOK, w.Code)
	require.Equal(t, "no-store", w.Header().Get("Cache-Control"))
	require.Contains(t, w.Body.String(), `"agents":[]`)
	w = httptest.NewRecorder()
	e.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/herdr/output?server_id=/tmp/x&pane_id=p", nil))
	require.Equal(t, http.StatusConflict, w.Code)
}

func TestCancellationAndDeadServers(t *testing.T) {
	e := endpoint{"/nonexistent/ccplant.sock", "dead", "core"}
	c := &Client{discover: func() []endpoint { return []endpoint{e} }}
	a, n := c.Agents(context.Background())
	require.Empty(t, a)
	require.Equal(t, 1, n)
	e = socket(t, "core", func(_ string, _ map[string]any) any { return nil })
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	require.Error(t, call(ctx, e, "terminal.list", map[string]any{}, new(any)))
}

func TestDiscoveryDoesNotFollowSocketSymlink(t *testing.T) {
	dir, err := os.MkdirTemp("", "hd-")
	require.NoError(t, err)
	defer func() { _ = os.RemoveAll(dir) }()
	t.Setenv("HOME", dir)
	t.Setenv("XDG_CONFIG_HOME", dir)
	t.Setenv("XDG_STATE_HOME", dir)
	t.Setenv("XDG_RUNTIME_DIR", dir)
	t.Setenv("HC_SOCKET", filepath.Join(dir, "symlink.sock"))
	e := socket(t, "core", func(_ string, _ map[string]any) any { return nil })
	require.NoError(t, os.Symlink(e.path, filepath.Join(dir, "symlink.sock")))
	for _, got := range discoverEndpoints() {
		require.NotEqual(t, filepath.Join(dir, "symlink.sock"), got.path)
	}
}

func TestUncertainDeliveryDisablesFurtherInput(t *testing.T) {
	var sends int
	e := socket(t, "herdr", func(method string, _ map[string]any) any {
		if method == "pane.list" {
			return map[string]any{"panes": []any{map[string]any{"pane_id": "p", "terminal_id": "t", "agent": "pi", "agent_status": "idle", "agent_session": map[string]string{"kind": "id", "value": "s"}}}}
		}
		sends++
		return nil // lost/invalid delivery acknowledgement
	})
	client := &Client{discover: func() []endpoint { return []endpoint{e} }}
	a, _ := client.Agents(context.Background())
	body, _ := json.Marshal(map[string]string{"server_id": a[0].ServerID, "pane_id": "p", "generation": a[0].Generation, "text": "one input"})
	httpServer := echo.New()
	pendingPath := filepath.Join(t.TempDir(), "pending")
	register(httpServer, client, pendingPath)
	for _, want := range []int{http.StatusServiceUnavailable, http.StatusConflict} {
		r := httptest.NewRequest(http.MethodPost, "/herdr/input", strings.NewReader(string(body)))
		r.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		httpServer.ServeHTTP(w, r)
		require.Equal(t, want, w.Code)
	}
	require.Equal(t, 1, sends)
	info, err := os.Stat(pendingPath)
	require.NoError(t, err)
	require.Equal(t, os.FileMode(0600), info.Mode().Perm())
	// Simulated bridge restart retains the write-ahead latch.
	restarted := echo.New()
	register(restarted, client, pendingPath)
	retry := httptest.NewRecorder()
	restarted.ServeHTTP(retry, httptest.NewRequest(http.MethodPost, "/herdr/input", strings.NewReader(string(body))))
	require.Equal(t, http.StatusConflict, retry.Code)
	w := httptest.NewRecorder()
	httpServer.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/herdr/agents", nil))
	require.Contains(t, w.Body.String(), `"input_disabled":true`)
	require.Contains(t, w.Body.String(), `"can_input":false`)
}

func TestCoreReadOnlyForDoneRecoveryAndWrappers(t *testing.T) {
	for _, tc := range []struct {
		name, status, argv string
		keep               bool
	}{
		{"done", "done", "claude", false},
		{"kept", "idle", "claude", true},
		{"shell", "idle", "bash", false},
		{"kb wrapper", "idle", "kb", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			e := socket(t, "core", func(method string, _ map[string]any) any {
				require.Equal(t, "terminal.list", method)
				return map[string]any{"terminals": []any{map[string]any{"address": "t1", "state": "live", "created_ms": 1, "pid": 10, "keep": tc.keep, "argv": []string{tc.argv}, "foreground": map[string]any{"pid": 10, "name": "claude"}, "status": map[string]string{"app": "claude", "state": tc.status}}}}
			})
			c := &Client{discover: func() []endpoint { return []endpoint{e} }}
			a, _ := c.Agents(context.Background())
			require.Len(t, a, 1)
			require.False(t, a[0].CanInput)
			require.ErrorIs(t, c.Send(context.Background(), Target{a[0].ServerID, a[0].PaneID, a[0].Generation}, "no"), ErrChanged)
		})
	}
}
