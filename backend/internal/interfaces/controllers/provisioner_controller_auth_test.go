package controllers

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/labstack/echo/v4"
	"github.com/takutakahashi/agentapi-proxy/internal/infrastructure/services"
	"github.com/takutakahashi/agentapi-proxy/pkg/sessionsettings"
)

type scopedProvisionerManagerStub struct{}

func (scopedProvisionerManagerStub) ValidateProvisionerToken(token string) bool {
	return token == "global-token"
}

func (scopedProvisionerManagerStub) ValidateSessionControlToken(sessionID, token string) bool {
	return sessionID == "session-a" && token == "session-a-token"
}

func (scopedProvisionerManagerStub) ConnectProvisioner(context.Context, services.ProvisionerConnectRequest) error {
	return nil
}

func (scopedProvisionerManagerStub) ClaimProvisionRequest(context.Context, string, string) (*services.ProvisionRequest, bool, error) {
	return nil, false, nil
}

func (scopedProvisionerManagerStub) UpdateProvisionRequestStatus(context.Context, string, string, services.ProvisionRequestStatusUpdate) error {
	return nil
}

type routedProvisionerManagerStub struct{ scopedProvisionerManagerStub }

func (routedProvisionerManagerStub) CurrentSessionSettings(context.Context, string) (*sessionsettings.SessionSettings, error) {
	return &sessionsettings.SessionSettings{ParentRuntime: &sessionsettings.ParentRuntimeConfig{
		Enabled: true, SessionID: "public-session",
	}}, nil
}

type recordingSessionStateStore struct{ savedID string }

func (s *recordingSessionStateStore) Save(_ context.Context, id string, _ io.Reader) error {
	s.savedID = id
	return nil
}

func (*recordingSessionStateStore) Load(context.Context, string) (io.ReadCloser, error) {
	return io.NopCloser(strings.NewReader("")), nil
}

func TestProvisionerSessionAuthorizationIsBoundToSession(t *testing.T) {
	controller := NewProvisionerController(scopedProvisionerManagerStub{}, nil, nil, nil)
	e := echo.New()

	for _, tc := range []struct {
		name, sessionID, token string
		want                   bool
	}{
		{name: "matching session", sessionID: "session-a", token: "session-a-token", want: true},
		{name: "other session", sessionID: "session-b", token: "session-a-token", want: false},
		{name: "global credential", sessionID: "session-a", token: "global-token", want: false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/", nil)
			req.Header.Set(echo.HeaderAuthorization, "Bearer "+tc.token)
			ctx := e.NewContext(req, httptest.NewRecorder())
			if got := controller.authorizedSession(ctx, tc.sessionID); got != tc.want {
				t.Fatalf("authorizedSession() = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestSaveSessionStateAuthenticatesRuntimeIDAndStoresByPublicID(t *testing.T) {
	store := &recordingSessionStateStore{}
	controller := NewProvisionerController(routedProvisionerManagerStub{}, nil, nil, nil, store)
	e := echo.New()
	req := httptest.NewRequest(http.MethodPut, "/internal/session-state/session-a", strings.NewReader("state"))
	req.Header.Set(echo.HeaderAuthorization, "Bearer session-a-token")
	rec := httptest.NewRecorder()
	ctx := e.NewContext(req, rec)
	ctx.SetPath("/internal/session-state/:sessionId")
	ctx.SetParamNames("sessionId")
	ctx.SetParamValues("session-a")

	if err := controller.SaveSessionState(ctx); err != nil {
		t.Fatal(err)
	}
	if rec.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusNoContent)
	}
	if store.savedID != "public-session" {
		t.Fatalf("saved session ID = %q, want public-session", store.savedID)
	}
}

func TestSaveSessionStateAllowsProvisionerCredentialOnlyForMappedRuntime(t *testing.T) {
	for _, tc := range []struct {
		name       string
		manager    ProvisionerManager
		wantStatus int
		wantID     string
	}{
		{name: "mapped private runtime", manager: routedProvisionerManagerStub{}, wantStatus: http.StatusNoContent, wantID: "public-session"},
		{name: "unmapped public ID", manager: scopedProvisionerManagerStub{}, wantStatus: http.StatusUnauthorized},
	} {
		t.Run(tc.name, func(t *testing.T) {
			store := &recordingSessionStateStore{}
			controller := NewProvisionerController(tc.manager, nil, nil, nil, store)
			e := echo.New()
			req := httptest.NewRequest(http.MethodPut, "/internal/session-state/session-a", strings.NewReader("state"))
			req.Header.Set(echo.HeaderAuthorization, "Bearer global-token")
			rec := httptest.NewRecorder()
			ctx := e.NewContext(req, rec)
			ctx.SetPath("/internal/session-state/:sessionId")
			ctx.SetParamNames("sessionId")
			ctx.SetParamValues("session-a")

			if err := controller.SaveSessionState(ctx); err != nil {
				t.Fatal(err)
			}
			if rec.Code != tc.wantStatus {
				t.Fatalf("status = %d, want %d", rec.Code, tc.wantStatus)
			}
			if store.savedID != tc.wantID {
				t.Fatalf("saved session ID = %q, want %q", store.savedID, tc.wantID)
			}
		})
	}
}
