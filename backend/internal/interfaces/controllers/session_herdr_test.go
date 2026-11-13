package controllers

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/labstack/echo/v4"
	"github.com/stretchr/testify/require"
	"github.com/takutakahashi/agentapi-proxy/internal/domain/entities"
	portrepos "github.com/takutakahashi/agentapi-proxy/internal/usecases/ports/repositories"
	"github.com/takutakahashi/agentapi-proxy/pkg/auth"
)

func TestHerdrProxyAuthorization(t *testing.T) {
	for _, remote := range []bool{false, true} {
		for _, tc := range []struct {
			name, method, user string
			permissions        []entities.Permission
			want               int
		}{
			{"read owner", http.MethodGet, "owner", []entities.Permission{entities.PermissionSessionRead}, 0},
			{"write owner", http.MethodPost, "owner", []entities.Permission{entities.PermissionSessionCreate}, 0},
			{"read only cannot type", http.MethodPost, "owner", []entities.Permission{entities.PermissionSessionRead}, http.StatusForbidden},
			{"other user cannot read", http.MethodGet, "other", []entities.Permission{entities.PermissionSessionRead}, http.StatusForbidden},
			{"other user cannot type", http.MethodPost, "other", []entities.Permission{entities.PermissionSessionCreate}, http.StatusForbidden},
			{"unauthenticated", http.MethodGet, "", nil, http.StatusUnauthorized},
		} {
			t.Run(tc.name+map[bool]string{true: " remote", false: " local"}[remote], func(t *testing.T) {
				mgr := newMockWaitSessionManager(&mockWaitSession{id: "s", userID: "owner"})
				controller := NewSessionController(&mockWaitProvider{manager: mgr}, nil)
				if remote {
					mgr.session = nil
					controller.sessionRouteRepo = &mockWaitRouteRepo{route: &portrepos.SessionRoute{SessionID: "s", UserID: "owner", Scope: "user", ManagerID: "manager"}}
				}
				e := echo.New()
				c := e.NewContext(httptest.NewRequest(tc.method, "/s/herdr/input", nil), httptest.NewRecorder())
				c.SetParamNames("sessionId")
				c.SetParamValues("s")
				if tc.user != "" {
					u := entities.NewUser(entities.UserID(tc.user), entities.UserTypeRegular, tc.user)
					u.SetPermissions(tc.permissions)
					c.Set("authz_context", &auth.AuthorizationContext{User: u, PersonalScope: auth.PersonalScopeAuth{UserID: tc.user}})
				}
				err := controller.authorizeHerdrRequest(c)
				if tc.want == 0 {
					require.NoError(t, err)
				} else {
					var he *echo.HTTPError
					require.ErrorAs(t, err, &he)
					require.Equal(t, tc.want, he.Code)
				}
			})
		}
	}
}

func TestSharedLinkRejectsHerdrBeforeSessionLookup(t *testing.T) {
	for _, url := range []string{"/s/token/herdr/agents", "/s/token/%68erdr/agents", "/s/token/herdr%2foutput"} {
		e := echo.New()
		e.Any("/s/:shareToken/*", (&ShareController{}).RouteToSharedSession)
		w := httptest.NewRecorder()
		e.ServeHTTP(w, httptest.NewRequest(http.MethodGet, url, nil))
		require.Equal(t, http.StatusForbidden, w.Code, url)
	}
}
