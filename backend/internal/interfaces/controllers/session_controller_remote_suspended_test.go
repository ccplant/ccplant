package controllers_test

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/labstack/echo/v4"
	"github.com/takutakahashi/agentapi-proxy/internal/domain/entities"
	"github.com/takutakahashi/agentapi-proxy/internal/interfaces/controllers"
	"github.com/takutakahashi/agentapi-proxy/internal/usecases/ports/repositories"
	"github.com/takutakahashi/agentapi-proxy/pkg/auth"
)

type scriptedTunnelCall struct {
	method string
	path   string
	body   string
}

type scriptedTunnelReply struct {
	status int
	body   string
}

type scriptedTunnel struct {
	calls   []scriptedTunnelCall
	replies []scriptedTunnelReply
}

func (t *scriptedTunnel) IsConnected(_ context.Context, managerID string) bool {
	return managerID == "manager-a"
}

func (t *scriptedTunnel) Do(_ context.Context, _, _, _ string, req *http.Request) (*http.Response, error) {
	var body []byte
	if req.Body != nil {
		body, _ = io.ReadAll(req.Body)
	}
	t.calls = append(t.calls, scriptedTunnelCall{method: req.Method, path: req.URL.Path, body: string(body)})
	reply := scriptedTunnelReply{status: http.StatusOK, body: `{}`}
	if len(t.replies) > 0 {
		reply = t.replies[0]
		t.replies = t.replies[1:]
	}
	return &http.Response{StatusCode: reply.status, Header: http.Header{"Content-Type": []string{"application/json"}}, Body: io.NopCloser(strings.NewReader(reply.body))}, nil
}

const remoteSuspendedReply = `{"error":{"code":"session_suspended","message":"Session workload is suspended","session_id":"remote-id","status":"suspended"}}`

func activeRemoteRoute() *deletionRouteRepo {
	return &deletionRouteRepo{route: &repositories.SessionRoute{
		SessionID: "public-id", RemoteSessionID: "remote-id", ManagerID: "manager-a",
		UserID: "user-1", Scope: string(entities.ScopeUser), Status: "active",
	}}
}

func remoteRouteContext(method, path, body string) (echo.Context, *httptest.ResponseRecorder) {
	req := httptest.NewRequest(method, path, bytes.NewReader([]byte(body)))
	rec := httptest.NewRecorder()
	ctx := echo.New().NewContext(req, rec)
	ctx.SetParamNames("sessionId", "*")
	ctx.SetParamValues("public-id", strings.TrimPrefix(path, "/public-id/"))
	ctx.Set("authz_context", &auth.AuthorizationContext{
		PersonalScope: auth.PersonalScopeAuth{UserID: "user-1", CanRead: true},
	})
	return ctx, rec
}

func TestRouteToRemoteSessionResumesWhenManagerReportsSuspendedWorkload(t *testing.T) {
	tunnel := &scriptedTunnel{replies: []scriptedTunnelReply{
		{status: http.StatusConflict, body: remoteSuspendedReply},
		{status: http.StatusOK, body: `{"session_id":"remote-id","status":"active"}`},
		{status: http.StatusOK, body: `{"ok":true}`},
	}}
	routeRepo := activeRemoteRoute()
	controller := controllers.NewSessionController(
		&routeSessionManagerProvider{manager: &fakeSessionManager{sessions: map[string]*fakeSession{}}}, nil,
		controllers.WithSessionRouteRepository(routeRepo),
		controllers.WithESMControlTunnel(tunnel),
	)
	ctx, rec := remoteRouteContext(http.MethodPost, "/public-id/message", `{"content":"hi","type":"user"}`)

	if err := controller.RouteToSession(ctx); err != nil {
		t.Fatal(err)
	}
	if len(tunnel.calls) != 3 {
		t.Fatalf("tunnel calls=%+v, want relay, resume, relay", tunnel.calls)
	}
	if tunnel.calls[1].path != "/api/v1/sessions/remote-id/resume" {
		t.Fatalf("second call path=%q, want parent resume", tunnel.calls[1].path)
	}
	if tunnel.calls[2].path != "/remote-id/message" || tunnel.calls[2].body != `{"content":"hi","type":"user"}` {
		t.Fatalf("retried call=%+v, want original request", tunnel.calls[2])
	}
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"ok":true`) {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
}

func TestRouteToRemoteSessionReportsResumingAfterAcceptedResume(t *testing.T) {
	tunnel := &scriptedTunnel{replies: []scriptedTunnelReply{
		{status: http.StatusConflict, body: remoteSuspendedReply},
		{status: http.StatusAccepted, body: `{"session_id":"remote-id","status":"resuming"}`},
		{status: http.StatusConflict, body: remoteSuspendedReply},
	}}
	controller := controllers.NewSessionController(
		&routeSessionManagerProvider{manager: &fakeSessionManager{sessions: map[string]*fakeSession{}}}, nil,
		controllers.WithSessionRouteRepository(activeRemoteRoute()),
		controllers.WithESMControlTunnel(tunnel),
	)
	ctx, rec := remoteRouteContext(http.MethodGet, "/public-id/messages", "")

	if err := controller.RouteToSession(ctx); err != nil {
		t.Fatal(err)
	}
	if len(tunnel.calls) != 3 || tunnel.calls[1].path != "/api/v1/sessions/remote-id/resume" {
		t.Fatalf("tunnel calls=%+v", tunnel.calls)
	}
	if rec.Code != http.StatusServiceUnavailable || rec.Header().Get("Retry-After") != "2" || !strings.Contains(rec.Body.String(), `"code":"session_resuming"`) {
		t.Fatalf("status=%d retry-after=%q body=%s", rec.Code, rec.Header().Get("Retry-After"), rec.Body.String())
	}
}

func TestRouteToRemoteSessionStatusOfSuspendedWorkloadDoesNotResume(t *testing.T) {
	tunnel := &scriptedTunnel{replies: []scriptedTunnelReply{{status: http.StatusConflict, body: remoteSuspendedReply}}}
	routeRepo := activeRemoteRoute()
	controller := controllers.NewSessionController(
		&routeSessionManagerProvider{manager: &fakeSessionManager{sessions: map[string]*fakeSession{}}}, nil,
		controllers.WithSessionRouteRepository(routeRepo),
		controllers.WithESMControlTunnel(tunnel),
	)
	ctx, rec := remoteRouteContext(http.MethodGet, "/public-id/status", "")

	if err := controller.RouteToSession(ctx); err != nil {
		t.Fatal(err)
	}
	if len(tunnel.calls) != 1 {
		t.Fatalf("status polling sent %+v, want only the relay", tunnel.calls)
	}
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"status":"suspended"`) {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	if routeRepo.route.Status != "suspended" {
		t.Fatalf("route status=%q, want suspended", routeRepo.route.Status)
	}
}
