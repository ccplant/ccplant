package controllers_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"github.com/labstack/echo/v4"
	"github.com/stretchr/testify/require"
	"github.com/takutakahashi/agentapi-proxy/internal/domain/entities"
	"github.com/takutakahashi/agentapi-proxy/internal/interfaces/controllers"
	"github.com/takutakahashi/agentapi-proxy/internal/usecases/ports/repositories"
	"github.com/takutakahashi/agentapi-proxy/pkg/auth"
)

func (r *filteringListRouteRepo) Delete(context.Context, string) error { return nil }

// stubSessionProfileRepo resolves profiles by ID for session list rendering.
type stubSessionProfileRepo struct {
	repositories.SessionProfileRepository
	profiles map[string]*entities.SessionProfile
}

func (r *stubSessionProfileRepo) Get(_ context.Context, id string) (*entities.SessionProfile, error) {
	return r.profiles[id], nil
}

func (r *listRouteRepo) Save(context.Context, *repositories.SessionRoute) error { return nil }
func (r *listRouteRepo) Get(context.Context, string) (*repositories.SessionRoute, error) {
	return nil, nil
}
func (r *listRouteRepo) List(context.Context, string) ([]*repositories.SessionRoute, error) {
	return r.routes, nil
}
func (r *listRouteRepo) Delete(context.Context, string) error { return nil }

func (r *deletionRouteRepo) Save(_ context.Context, route *repositories.SessionRoute) error {
	r.route = route
	r.saved = true
	return nil
}
func (r *deletionRouteRepo) Get(_ context.Context, sessionID string) (*repositories.SessionRoute, error) {
	if r.route != nil && r.route.SessionID == sessionID {
		return r.route, nil
	}
	return nil, nil
}
func (r *deletionRouteRepo) List(context.Context, string) ([]*repositories.SessionRoute, error) {
	if r.route == nil {
		return nil, nil
	}
	return []*repositories.SessionRoute{r.route}, nil
}
func (r *deletionRouteRepo) Delete(context.Context, string) error {
	r.deleted = true
	r.route = nil
	return nil
}

func TestUpdateSessionAnnotationsPersistsRoutedSession(t *testing.T) {
	manager := &fakeSessionManager{sessions: map[string]*fakeSession{}}
	routes := &deletionRouteRepo{route: &repositories.SessionRoute{
		SessionID: "public-id", RemoteSessionID: "remote-id", ManagerID: "manager-a",
		UserID: "user-1", Scope: string(entities.ScopeUser),
		Annotations: entities.SessionAnnotations{PRURL: "https://example.test/old"},
	}}
	controller := controllers.NewSessionController(
		&routeSessionManagerProvider{manager: manager}, nil,
		controllers.WithSessionRouteRepository(routes),
	)
	req := httptest.NewRequest(http.MethodPatch, "/sessions/public-id/annotations", strings.NewReader(`{"description":"updated","running_task":"testing"}`))
	req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
	rec := httptest.NewRecorder()
	ctx := echo.New().NewContext(req, rec)
	ctx.SetPath("/sessions/:sessionId/annotations")
	ctx.SetParamNames("sessionId")
	ctx.SetParamValues("public-id")
	ctx.Set("authz_context", &auth.AuthorizationContext{
		User:          entities.NewUser("user-1", entities.UserTypeRegular, "user-1"),
		PersonalScope: auth.PersonalScopeAuth{UserID: "user-1", CanCreate: true, CanRead: true, CanUpdate: true},
	})

	require.NoError(t, controller.UpdateSessionAnnotations(ctx))
	require.Equal(t, http.StatusOK, rec.Code)
	require.True(t, routes.saved)
	require.Equal(t, "updated", routes.route.Annotations.Description)
	require.Equal(t, "testing", routes.route.Annotations.RunningTask)
	require.Equal(t, "https://example.test/old", routes.route.Annotations.PRURL)
	require.JSONEq(t, `{"session_id":"public-id","annotations":{"pr_url":"https://example.test/old","description":"updated","running_task":"testing"},"metadata":{"description":"updated"}}`, rec.Body.String())
}

func TestUpdateSessionAnnotationsRejectsUnauthorizedRoutedSession(t *testing.T) {
	manager := &fakeSessionManager{sessions: map[string]*fakeSession{}}
	routes := &deletionRouteRepo{route: &repositories.SessionRoute{
		SessionID: "public-id", UserID: "owner", Scope: string(entities.ScopeUser),
	}}
	controller := controllers.NewSessionController(
		&routeSessionManagerProvider{manager: manager}, nil,
		controllers.WithSessionRouteRepository(routes),
	)
	req := httptest.NewRequest(http.MethodPatch, "/sessions/public-id/annotations", strings.NewReader(`{"description":"updated"}`))
	req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
	ctx := echo.New().NewContext(req, httptest.NewRecorder())
	ctx.SetParamNames("sessionId")
	ctx.SetParamValues("public-id")
	ctx.Set("authz_context", &auth.AuthorizationContext{
		User:          entities.NewUser("other", entities.UserTypeRegular, "other"),
		PersonalScope: auth.PersonalScopeAuth{UserID: "other", CanUpdate: true},
	})

	err := controller.UpdateSessionAnnotations(ctx)
	httpErr, ok := err.(*echo.HTTPError)
	require.True(t, ok)
	require.Equal(t, http.StatusForbidden, httpErr.Code)
	require.False(t, routes.saved)
}

func (t *directRuntimeTunnel) IsConnected(_ context.Context, managerID string) bool {
	return managerID == "public-id"
}

func (t *directRuntimeTunnel) Do(_ context.Context, managerID, _, _ string, req *http.Request) (*http.Response, error) {
	t.managerID = managerID
	t.path = req.URL.Path
	return &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": {"application/json"}},
		Body:       io.NopCloser(strings.NewReader(`{"status":"stable"}`)),
	}, nil
}

func (p *routeSessionManagerProvider) GetSessionManager() repositories.SessionManager {
	return p.manager
}

func routeContext(e *echo.Echo, method, path, sessionID string) (echo.Context, *httptest.ResponseRecorder) {
	req := httptest.NewRequest(method, path, nil)
	rec := httptest.NewRecorder()
	ctx := e.NewContext(req, rec)
	ctx.SetParamNames("sessionId", "*")
	ctx.SetParamValues(sessionID, strings.TrimPrefix(path, "/"+sessionID+"/"))
	ctx.Set("authz_context", &auth.AuthorizationContext{
		PersonalScope: auth.PersonalScopeAuth{UserID: "user-1", CanRead: true},
	})
	return ctx, rec
}

func TestSearchSessionsIsolatesRequestedTeamAcrossLocalAndRoutedSessions(t *testing.T) {
	manager := &fakeSessionManager{sessions: map[string]*fakeSession{
		"local-a":  {id: "local-a", userID: "user-1", scope: entities.ScopeTeam, teamID: "acme/a", status: "active"},
		"local-b":  {id: "local-b", userID: "user-1", scope: entities.ScopeTeam, teamID: "acme/b", status: "active"},
		"personal": {id: "personal", userID: "user-1", scope: entities.ScopeUser, status: "active"},
	}}
	routeRepo := &listRouteRepo{routes: []*repositories.SessionRoute{
		{SessionID: "routed-a", UserID: "user-1", Scope: string(entities.ScopeTeam), TeamID: "acme/a"},
		{SessionID: "routed-b", UserID: "user-1", Scope: string(entities.ScopeTeam), TeamID: "acme/b"},
		{SessionID: "routed-personal", UserID: "user-1", Scope: string(entities.ScopeUser)},
	}}
	controller := controllers.NewSessionController(
		&routeSessionManagerProvider{manager: manager}, nil,
		controllers.WithSessionRouteRepository(routeRepo),
	)

	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/search?scope=team&team_id=acme%2Fa", nil)
	rec := httptest.NewRecorder()
	ctx := e.NewContext(req, rec)
	ctx.Set("authz_context", &auth.AuthorizationContext{
		PersonalScope: auth.PersonalScopeAuth{UserID: "user-1", CanRead: true},
		TeamScope:     auth.TeamScopeAuth{Teams: []string{"acme/a", "acme/b"}},
	})

	if err := controller.SearchSessions(ctx); err != nil {
		t.Fatal(err)
	}
	var response struct {
		Sessions []struct {
			SessionID string `json:"session_id"`
		} `json:"sessions"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	got := make(map[string]bool, len(response.Sessions))
	for _, session := range response.Sessions {
		got[session.SessionID] = true
	}
	if len(got) != 2 || !got["local-a"] || !got["routed-a"] {
		t.Fatalf("session IDs = %v, want only local-a and routed-a", got)
	}
}

func TestSearchSessionsIncludesTeamRouteOwnedByAnotherMember(t *testing.T) {
	routeRepo := &filteringListRouteRepo{routes: []*repositories.SessionRoute{
		{SessionID: "webhook-pool-session", UserID: "webhook-owner", Scope: string(entities.ScopeTeam), TeamID: "acme/a"},
	}}
	controller := controllers.NewSessionController(
		&routeSessionManagerProvider{manager: &fakeSessionManager{sessions: map[string]*fakeSession{}}}, nil,
		controllers.WithSessionRouteRepository(routeRepo),
	)

	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/search?scope=team&team_id=acme%2Fa", nil)
	rec := httptest.NewRecorder()
	ctx := e.NewContext(req, rec)
	ctx.Set("authz_context", &auth.AuthorizationContext{
		PersonalScope: auth.PersonalScopeAuth{UserID: "team-member", CanRead: true},
		TeamScope:     auth.TeamScopeAuth{Teams: []string{"acme/a"}},
	})

	if err := controller.SearchSessions(ctx); err != nil {
		t.Fatal(err)
	}
	var response struct {
		Sessions []struct {
			SessionID string `json:"session_id"`
		} `json:"sessions"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if len(response.Sessions) != 1 || response.Sessions[0].SessionID != "webhook-pool-session" {
		t.Fatalf("sessions = %#v, want webhook-pool-session", response.Sessions)
	}
	if routeRepo.lastFilter.UserID != "" || routeRepo.lastFilter.Scope != "team" || routeRepo.lastFilter.TeamID != "acme/a" {
		t.Fatalf("route filter = %#v, want team acme/a without user filter", routeRepo.lastFilter)
	}
}

func TestSearchSessionsExposesModelOptionsFromSessionProfileForRoutedSessions(t *testing.T) {
	profile := entities.NewSessionProfile("profile-1", "codex", "user-1")
	cfg := entities.NewSessionProfileConfig()
	cfg.SetParams(&entities.SessionParams{ModelOptions: []string{"sonnet", "opus"}})
	profile.SetConfig(cfg)

	routeRepo := &listRouteRepo{routes: []*repositories.SessionRoute{
		{
			SessionID: "routed-model", UserID: "user-1", Scope: string(entities.ScopeUser),
			Tags: map[string]string{"session_profile_id": "profile-1"},
		},
	}}
	controller := controllers.NewSessionController(
		&routeSessionManagerProvider{manager: &fakeSessionManager{sessions: map[string]*fakeSession{}}},
		nil,
		controllers.WithSessionRouteRepository(routeRepo),
		controllers.WithSessionProfileRepository(&stubSessionProfileRepo{
			profiles: map[string]*entities.SessionProfile{"profile-1": profile},
		}),
	)

	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/search", nil)
	rec := httptest.NewRecorder()
	ctx := e.NewContext(req, rec)
	ctx.Set("authz_context", &auth.AuthorizationContext{
		PersonalScope: auth.PersonalScopeAuth{UserID: "user-1", CanRead: true},
	})

	if err := controller.SearchSessions(ctx); err != nil {
		t.Fatal(err)
	}
	var response struct {
		Sessions []struct {
			SessionID    string   `json:"session_id"`
			ModelOptions []string `json:"model_options"`
		} `json:"sessions"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if len(response.Sessions) != 1 {
		t.Fatalf("sessions = %+v, want exactly the routed session", response.Sessions)
	}
	if got := response.Sessions[0].ModelOptions; !reflect.DeepEqual(got, []string{"sonnet", "opus"}) {
		t.Fatalf("model_options = %v, want [sonnet opus]", got)
	}
}

func TestRouteToSessionEnsuresLocalAliasWorkloadOnGet(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/status" {
			t.Errorf("upstream path = %q, want /status", r.URL.Path)
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"status":"stable"}`))
	}))
	defer upstream.Close()

	manager := &ensuringSessionManager{fakeSessionManager: &fakeSessionManager{sessions: map[string]*fakeSession{
		"remote-id": {id: "remote-id", addr: strings.TrimPrefix(upstream.URL, "http://"), userID: "user-1", scope: entities.ScopeUser},
	}}}
	controller := controllers.NewSessionController(
		&routeSessionManagerProvider{manager: manager},
		nil,
		controllers.WithSessionRouteRepository(&fakeACPRouteRepo{route: &repositories.SessionRoute{
			SessionID: "public-id", RemoteSessionID: "remote-id",
		}}),
	)
	ctx, rec := routeContext(echo.New(), http.MethodGet, "/public-id/status", "public-id")

	if err := controller.RouteToSession(ctx); err != nil {
		t.Fatal(err)
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("response status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	if len(manager.ensuredIDs) != 1 || manager.ensuredIDs[0] != "remote-id" {
		t.Fatalf("ensured IDs = %v, want [remote-id]", manager.ensuredIDs)
	}
}

func TestRouteToSessionEnsuresRegularLocalWorkloadOnGet(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer upstream.Close()

	manager := &ensuringSessionManager{fakeSessionManager: &fakeSessionManager{sessions: map[string]*fakeSession{
		"local-id": {id: "local-id", addr: strings.TrimPrefix(upstream.URL, "http://"), userID: "user-1", scope: entities.ScopeUser},
	}}}
	controller := controllers.NewSessionController(&routeSessionManagerProvider{manager: manager}, nil)
	ctx, rec := routeContext(echo.New(), http.MethodGet, "/local-id/status", "local-id")

	if err := controller.RouteToSession(ctx); err != nil {
		t.Fatal(err)
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("response status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	if len(manager.ensuredIDs) != 1 || manager.ensuredIDs[0] != "local-id" {
		t.Fatalf("ensured IDs = %v, want [local-id]", manager.ensuredIDs)
	}
}

func TestRouteToSessionReturnsStructuredResumingResponse(t *testing.T) {
	manager := &ensuringSessionManager{
		fakeSessionManager: &fakeSessionManager{sessions: map[string]*fakeSession{
			"local-id": {id: "local-id", userID: "user-1", scope: entities.ScopeUser},
		}},
		restoring: true,
	}
	controller := controllers.NewSessionController(&routeSessionManagerProvider{manager: manager}, nil)
	ctx, rec := routeContext(echo.New(), http.MethodGet, "/local-id/status", "local-id")

	if err := controller.RouteToSession(ctx); err != nil {
		t.Fatal(err)
	}
	if rec.Code != http.StatusServiceUnavailable || rec.Header().Get("Retry-After") != "2" {
		t.Fatalf("status=%d retry-after=%q body=%s", rec.Code, rec.Header().Get("Retry-After"), rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"code":"session_resuming"`) {
		t.Fatalf("body = %s", rec.Body.String())
	}
}
