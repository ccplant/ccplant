package controllers_test

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/labstack/echo/v4"
	testassert "github.com/stretchr/testify/assert"
	"github.com/takutakahashi/agentapi-proxy/internal/domain/entities"
	"github.com/takutakahashi/agentapi-proxy/internal/interfaces/controllers"
	"github.com/takutakahashi/agentapi-proxy/internal/usecases/ports/repositories"
	"github.com/takutakahashi/agentapi-proxy/pkg/auth"
	"github.com/takutakahashi/agentapi-proxy/pkg/hmacutil"
)

func TestACPController_SessionResume_Running(t *testing.T) {
	s := &fakeSession{id: "sess-run", userID: "user1", scope: entities.ScopeUser, status: "running"}
	e, ctrl, _ := newEchoWithACP(map[string]*fakeSession{"sess-run": s})
	c, rec := setupEchoContext(e, http.MethodPost, "/acp",
		rpcBody("session/resume", map[string]string{"sessionId": "sess-run"}), "user1")

	if err := ctrl.HandleRPC(c); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	resp := parseRPCResponse(t, rec)
	if resp["error"] != nil {
		t.Errorf("unexpected error: %v", resp["error"])
	}
}

func TestACPController_SessionResume_Stable(t *testing.T) {
	s := &fakeSession{id: "sess-stable", userID: "user1", scope: entities.ScopeUser, status: "stable"}
	e, ctrl, _ := newEchoWithACP(map[string]*fakeSession{"sess-stable": s})
	c, rec := setupEchoContext(e, http.MethodPost, "/acp",
		rpcBody("session/resume", map[string]string{"sessionId": "sess-stable"}), "user1")

	if err := ctrl.HandleRPC(c); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	resp := parseRPCResponse(t, rec)
	if resp["error"] != nil {
		t.Errorf("unexpected error: %v", resp["error"])
	}
}

func TestACPController_SessionResume_Stopped(t *testing.T) {
	s := &fakeSession{id: "sess-stopped", userID: "user1", scope: entities.ScopeUser, status: "stopped"}
	e, ctrl, _ := newEchoWithACP(map[string]*fakeSession{"sess-stopped": s})
	c, rec := setupEchoContext(e, http.MethodPost, "/acp",
		rpcBody("session/resume", map[string]string{"sessionId": "sess-stopped"}), "user1")

	if err := ctrl.HandleRPC(c); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	resp := parseRPCResponse(t, rec)
	if resp["error"] == nil {
		t.Error("expected error for stopped session")
	}
}

func TestACPController_SessionResume_NotFound(t *testing.T) {
	e, ctrl, _ := newEchoWithACP(nil)
	c, rec := setupEchoContext(e, http.MethodPost, "/acp",
		rpcBody("session/resume", map[string]string{"sessionId": "ghost"}), "user1")

	if err := ctrl.HandleRPC(c); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	resp := parseRPCResponse(t, rec)
	rpcErr := resp["error"].(map[string]interface{})
	if rpcErr["code"].(float64) != -32602 {
		t.Errorf("expected -32602, got %v", rpcErr["code"])
	}
}

// ----------------------------------------------------------------------------
// Tests: session/load
// ----------------------------------------------------------------------------

func TestACPController_SessionLoad_OK(t *testing.T) {
	s := &fakeSession{id: "sess-load", userID: "user1", scope: entities.ScopeUser, status: "running"}
	e, ctrl, _ := newEchoWithACP(map[string]*fakeSession{"sess-load": s})
	c, rec := setupEchoContext(e, http.MethodPost, "/acp",
		rpcBody("session/load", map[string]string{"sessionId": "sess-load"}), "user1")

	if err := ctrl.HandleRPC(c); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	resp := parseRPCResponse(t, rec)
	if resp["error"] != nil {
		t.Errorf("unexpected error: %v", resp["error"])
	}
}

func TestACPController_SessionLoad_NotFound(t *testing.T) {
	e, ctrl, _ := newEchoWithACP(nil)
	c, rec := setupEchoContext(e, http.MethodPost, "/acp",
		rpcBody("session/load", map[string]string{"sessionId": "ghost"}), "user1")

	if err := ctrl.HandleRPC(c); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	resp := parseRPCResponse(t, rec)
	if resp["error"] == nil {
		t.Error("expected error for nonexistent session")
	}
}

// ----------------------------------------------------------------------------
// Tests: error cases
// ----------------------------------------------------------------------------

func TestACPController_UnknownMethod(t *testing.T) {
	e, ctrl, _ := newEchoWithACP(nil)
	c, rec := setupEchoContext(e, http.MethodPost, "/acp",
		rpcBody("unknown/method", nil), "user1")

	if err := ctrl.HandleRPC(c); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	resp := parseRPCResponse(t, rec)
	rpcErr := resp["error"].(map[string]interface{})
	if rpcErr["code"].(float64) != -32601 {
		t.Errorf("expected -32601, got %v", rpcErr["code"])
	}
}

func TestACPController_InvalidJsonrpcVersion(t *testing.T) {
	e, ctrl, _ := newEchoWithACP(nil)
	body := `{"jsonrpc":"1.0","id":1,"method":"initialize","params":{}}`
	c, rec := setupEchoContext(e, http.MethodPost, "/acp", body, "user1")

	if err := ctrl.HandleRPC(c); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	resp := parseRPCResponse(t, rec)
	rpcErr := resp["error"].(map[string]interface{})
	if rpcErr["code"].(float64) != -32600 {
		t.Errorf("expected -32600, got %v", rpcErr["code"])
	}
}

type outboundACPTestTunnel struct{ target *url.URL }

func (t outboundACPTestTunnel) IsConnected(context.Context, string) bool { return true }
func (t outboundACPTestTunnel) Do(_ context.Context, _, _, _ string, req *http.Request) (*http.Response, error) {
	req.URL.Scheme = t.target.Scheme
	req.URL.Host = t.target.Host
	return http.DefaultClient.Do(req)
}

func TestACPController_BuiltInPromptUsesOutboundRuntime(t *testing.T) {
	var rpcSessionID string
	runtime := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method + " " + r.URL.Path {
		case http.MethodGet + " /session":
			testassert.NoError(t, json.NewEncoder(w).Encode(map[string]string{"sessionId": "bridge-session"}))
		case http.MethodPost + " /rpc":
			var request struct {
				Params struct {
					SessionID string `json:"sessionId"`
				} `json:"params"`
			}
			_ = json.NewDecoder(r.Body).Decode(&request)
			rpcSessionID = request.Params.SessionID
			w.WriteHeader(http.StatusOK)
		default:
			http.NotFound(w, r)
		}
	}))
	defer runtime.Close()
	runtimeURL, err := url.Parse(runtime.URL)
	if err != nil {
		t.Fatal(err)
	}
	ctrl := controllers.NewACPController(
		&testSessionManagerProvider{mgr: &fakeSessionManager{sessions: map[string]*fakeSession{}}},
		&fakeSessionCreator{},
		&fakeACPRouteRepo{route: &repositories.SessionRoute{
			SessionID: "built-in", RemoteSessionID: "built-in", Transport: repositories.SessionRouteTransportDirectRuntime,
			UserID: "user1", Scope: string(entities.ScopeUser),
		}},
	)
	ctrl.SetESMControlTunnel(outboundACPTestTunnel{target: runtimeURL})
	e := echo.New()
	body := `{"jsonrpc":"2.0","id":1,"method":"session/prompt","params":{"sessionId":"built-in","prompt":[]}}`
	c, rec := setupEchoContext(e, http.MethodPost, "/acp", body, "user1")
	if err := ctrl.HandleRPC(c); err != nil {
		t.Fatal(err)
	}
	if rec.Code != http.StatusOK || rpcSessionID != "bridge-session" {
		t.Fatalf("status=%d rpc session=%q body=%s", rec.Code, rpcSessionID, rec.Body.String())
	}
}

func TestACPController_RemoteSessionSSEUsesOutboundTunnel(t *testing.T) {
	const secret = "remote-sse-secret"
	requestSeen := make(chan struct{}, 1)
	remote := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/remote-session/sse" {
			t.Errorf("remote path = %q", r.URL.Path)
			http.NotFound(w, r)
			return
		}
		ts := r.Header.Get(hmacutil.TimestampHeader)
		msg := hmacutil.BuildMessage(http.MethodGet, r.URL.RequestURI(), ts, nil)
		if err := hmacutil.ValidateTimestamp(ts); err != nil || !hmacutil.Verify([]byte(secret), msg, r.Header.Get("X-Hub-Signature-256")) {
			t.Error("remote SSE request did not contain a valid HMAC signature")
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		if r.Header.Get("Last-Event-ID") != "4" {
			t.Errorf("Last-Event-ID = %q", r.Header.Get("Last-Event-ID"))
		}
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		_, _ = fmt.Fprint(w, "id: 5\nevent: message\ndata: {\"stream\":true}\n\n")
		w.(http.Flusher).Flush()
		requestSeen <- struct{}{}
		<-r.Context().Done()
	}))
	defer remote.Close()

	mgr := &fakeSessionManager{sessions: map[string]*fakeSession{}}
	creator := &fakeSessionCreator{}
	ctrl := controllers.NewACPController(&testSessionManagerProvider{mgr: mgr}, creator, &fakeACPRouteRepo{route: &repositories.SessionRoute{
		SessionID: "proxy-session", RemoteSessionID: "remote-session", ManagerID: "manager-a",
		HMACSecret: secret, UserID: "user1", Scope: string(entities.ScopeUser),
	}})
	remoteURL, err := url.Parse(remote.URL)
	if err != nil {
		t.Fatal(err)
	}
	ctrl.SetESMControlTunnel(outboundACPTestTunnel{target: remoteURL})
	e := echo.New()
	e.GET("/acp", func(c echo.Context) error {
		c.Set("authz_context", &auth.AuthorizationContext{PersonalScope: auth.PersonalScopeAuth{UserID: "user1", CanRead: true}})
		return ctrl.HandleSessionSSE(c)
	})
	proxy := httptest.NewServer(e)
	defer proxy.Close()

	req, err := http.NewRequest(http.MethodGet, proxy.URL+"/acp", nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Acp-Session-Id", "proxy-session")
	req.Header.Set("Last-Event-ID", "4")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK || resp.Header.Get("Content-Type") != "text/event-stream" {
		t.Fatalf("SSE response = %d %q", resp.StatusCode, resp.Header.Get("Content-Type"))
	}

	scanner := bufio.NewScanner(resp.Body)
	lines := make([]string, 0, 3)
	for scanner.Scan() {
		if scanner.Text() != "" {
			lines = append(lines, scanner.Text())
		}
		if len(lines) == 3 {
			break
		}
	}
	if got := strings.Join(lines, "|"); got != `id: 5|event: message|data: {"stream":true}` {
		t.Fatalf("streamed event = %q", got)
	}
	select {
	case <-requestSeen:
	case <-time.After(time.Second):
		t.Fatal("remote SSE request was not observed")
	}
}
