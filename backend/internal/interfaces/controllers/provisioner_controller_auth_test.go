package controllers

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/labstack/echo/v4"
	"github.com/takutakahashi/agentapi-proxy/internal/infrastructure/services"
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
