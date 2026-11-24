package controllers_test

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"github.com/labstack/echo/v4"
	"github.com/stretchr/testify/require"
	sessionrunnercore "github.com/takutakahashi/agentapi-proxy/internal/core/sessionrunner"
	"github.com/takutakahashi/agentapi-proxy/internal/domain/entities"
	"github.com/takutakahashi/agentapi-proxy/internal/interfaces/controllers"
	"github.com/takutakahashi/agentapi-proxy/internal/usecases/ports/repositories"
	"github.com/takutakahashi/agentapi-proxy/pkg/auth"
	"github.com/takutakahashi/agentapi-proxy/pkg/sessionsettings"
)

type ensuringSessionManager struct {
	*fakeSessionManager
	ensuredIDs []string
	restoring  bool
}

func (m *ensuringSessionManager) EnsureSessionWorkload(_ context.Context, id string) (entities.Session, bool, error) {
	m.ensuredIDs = append(m.ensuredIDs, id)
	return m.GetSession(id), m.restoring, nil
}

type routeSessionManagerProvider struct {
	manager repositories.SessionManager
}

type statusWatchingSessionManager struct {
	*fakeSessionManager
	events chan repositories.SessionStatusEvent
}

func (m *statusWatchingSessionManager) SubscribeStatusEvents() (<-chan repositories.SessionStatusEvent, func()) {
	return m.events, func() {}
}

type directRuntimeTunnel struct {
	managerID string
	path      string
}

type lifecycleTunnel struct {
	path     string
	body     []byte
	enqueued bool
	done     bool
	status   int
	onDo     func()
}

type statusUsageRecorder struct {
	dimensions repositories.SessionUsageDimensions
	events     []repositories.SessionStatusEvent
}

func (r *statusUsageRecorder) RecordStatus(_ context.Context, event repositories.SessionStatusEvent) error {
	r.events = append(r.events, event)
	return nil
}

func (r *statusUsageRecorder) ResolveDimensions(_ context.Context, sessionID string) (repositories.SessionUsageDimensions, error) {
	dimensions := r.dimensions
	dimensions.SessionID = sessionID
	return dimensions, nil
}

func (r *statusUsageRecorder) RecordResolvedStatus(_ context.Context, _ repositories.SessionUsageDimensions, event repositories.SessionStatusEvent) error {
	r.events = append(r.events, event)
	return nil
}

func (t *lifecycleTunnel) IsConnected(_ context.Context, managerID string) bool {
	return managerID == "manager-a"
}

func (t *lifecycleTunnel) Do(_ context.Context, _, _, _ string, req *http.Request) (*http.Response, error) {
	if t.onDo != nil {
		t.onDo()
	}
	t.path = req.URL.Path
	if req.Body != nil {
		t.body, _ = io.ReadAll(req.Body)
	}
	status := t.status
	if status == 0 {
		status = http.StatusOK
	}
	return &http.Response{StatusCode: status, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(`{}`))}, nil
}

type allocationReader struct {
	allocation           *sessionrunnercore.Allocation
	err                  error
	deletedConfiguration string
}

type resumeConfigurationStore struct {
	*allocationReader
	configuration *sessionrunnercore.Configuration
}

func (s *resumeConfigurationStore) CreateConfiguration(context.Context, *sessionrunnercore.Configuration) error {
	return nil
}
func (s *resumeConfigurationStore) GetConfiguration(context.Context, string) (*sessionrunnercore.Configuration, error) {
	return s.configuration, nil
}
func (s *resumeConfigurationStore) SaveConfiguration(context.Context, *sessionrunnercore.Configuration) error {
	return nil
}
func (s *resumeConfigurationStore) UpdateProvisionSettings(context.Context, string, []byte) error {
	return nil
}

type resumeSettingsCreator struct{ controllers.SessionCreator }

func (resumeSettingsCreator) ResolveRestartSettings(_ context.Context, id string, start entities.StartRequest, _ string, _ []string) (*sessionsettings.SessionSettings, error) {
	token := ""
	if start.Params != nil {
		token = start.Params.GithubToken
	}
	return &sessionsettings.SessionSettings{
		Session: sessionsettings.SessionMeta{ID: id, UserID: "user-1", Scope: string(entities.ScopeUser)},
		Env:     map[string]string{"GITHUB_TOKEN": token},
	}, nil
}

type resumeSettingsRepo struct{ settings *entities.Settings }

func (r *resumeSettingsRepo) Save(context.Context, *entities.Settings) error { return nil }
func (r *resumeSettingsRepo) FindByName(context.Context, string) (*entities.Settings, error) {
	return r.settings, nil
}
func (r *resumeSettingsRepo) Delete(context.Context, string) error         { return nil }
func (r *resumeSettingsRepo) Exists(context.Context, string) (bool, error) { return true, nil }
func (r *resumeSettingsRepo) List(context.Context) ([]*entities.Settings, error) {
	return []*entities.Settings{r.settings}, nil
}

func (s *allocationReader) GetAllocation(context.Context, string) (*sessionrunnercore.Allocation, error) {
	return s.allocation, s.err
}

func (s *allocationReader) DeleteConfiguration(_ context.Context, sessionID string) error {
	s.deletedConfiguration = sessionID
	return nil
}

func (t *lifecycleTunnel) Enqueue(_ context.Context, _, _, _ string, req *http.Request) (string, error) {
	t.path = req.URL.Path
	if req.Body != nil {
		t.body, _ = io.ReadAll(req.Body)
	}
	t.enqueued = true
	return "request-id", nil
}

func (t *lifecycleTunnel) CommandResult(_ context.Context, _ string) (bool, int, error) {
	return t.done, t.status, nil
}

func TestStartSessionReusesMatchingDirectRuntimeThroughPromptCommand(t *testing.T) {
	manager := &fakeSessionManager{sessions: map[string]*fakeSession{
		"existing": {id: "existing", userID: "user-1", scope: entities.ScopeUser, status: "active", tags: map[string]string{"slack_thread_ts": "123", "slackbot_id": "bot-1"}},
	}}
	routes := &deletionRouteRepo{route: &repositories.SessionRoute{
		SessionID: "existing", RemoteSessionID: "runner-1", ManagerID: "manager-a",
		Transport: repositories.SessionRouteTransportDirectRuntime, Status: "active",
		UserID: "user-1", Scope: string(entities.ScopeUser),
		Tags: map[string]string{"slack_thread_ts": "123", "slackbot_id": "bot-1"},
	}}
	tunnel := &lifecycleTunnel{}
	controller := controllers.NewSessionController(
		&routeSessionManagerProvider{manager: manager}, nil,
		controllers.WithSessionRouteRepository(routes), controllers.WithESMControlTunnel(tunnel),
	)
	body := `{"reuse_match_tags":{"slack_thread_ts":"123","slackbot_id":"bot-1"},"reuse_message":"follow up"}`
	req := httptest.NewRequest(http.MethodPost, "/start", strings.NewReader(body))
	req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
	rec := httptest.NewRecorder()
	ctx := echo.New().NewContext(req, rec)
	ctx.Set("authz_context", &auth.AuthorizationContext{
		User:          entities.NewUser("user-1", entities.UserTypeRegular, "user-1"),
		PersonalScope: auth.PersonalScopeAuth{UserID: "user-1", CanCreate: true, CanRead: true},
	})

	if err := controller.StartSession(ctx); err != nil {
		t.Fatal(err)
	}
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"session_id":"existing"`) || !strings.Contains(rec.Body.String(), `"session_reused":true`) {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	if !tunnel.enqueued || tunnel.path != "/internal/session-prompt" {
		t.Fatalf("enqueued=%t path=%q", tunnel.enqueued, tunnel.path)
	}
	if got := string(tunnel.body); got != `{"content":"follow up","type":"user"}` {
		t.Fatalf("queued body = %s", got)
	}
}

func TestStartSessionInterruptsRunningDirectRuntimeBeforePrompt(t *testing.T) {
	manager := &fakeSessionManager{sessions: map[string]*fakeSession{
		"existing": {id: "existing", userID: "user-1", scope: entities.ScopeUser, status: "running", tags: map[string]string{"slack_thread_ts": "123", "slackbot_id": "bot-1"}},
	}}
	routes := &deletionRouteRepo{route: &repositories.SessionRoute{
		SessionID: "existing", RemoteSessionID: "runner-1", ManagerID: "manager-a",
		Transport: repositories.SessionRouteTransportDirectRuntime, Status: "running",
		UserID: "user-1", Scope: string(entities.ScopeUser),
		Tags: map[string]string{"slack_thread_ts": "123", "slackbot_id": "bot-1"},
	}}
	tunnel := &lifecycleTunnel{}
	controller := controllers.NewSessionController(
		&routeSessionManagerProvider{manager: manager}, nil,
		controllers.WithSessionRouteRepository(routes), controllers.WithESMControlTunnel(tunnel),
	)
	body := `{"reuse_match_tags":{"slack_thread_ts":"123","slackbot_id":"bot-1"},"reuse_message":"follow up","stop_before_reuse":true}`
	req := httptest.NewRequest(http.MethodPost, "/start", strings.NewReader(body))
	req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
	rec := httptest.NewRecorder()
	ctx := echo.New().NewContext(req, rec)
	ctx.Set("authz_context", &auth.AuthorizationContext{
		User:          entities.NewUser("user-1", entities.UserTypeRegular, "user-1"),
		PersonalScope: auth.PersonalScopeAuth{UserID: "user-1", CanCreate: true, CanRead: true},
	})

	if err := controller.StartSession(ctx); err != nil {
		t.Fatal(err)
	}
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"session_reused":true`) {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	if !tunnel.enqueued || tunnel.path != "/internal/session-interrupt-prompt" {
		t.Fatalf("enqueued=%t path=%q", tunnel.enqueued, tunnel.path)
	}
}

func TestStartSessionStopsRunningManagedSessionBeforePrompt(t *testing.T) {
	manager := &fakeSessionManager{sessions: map[string]*fakeSession{
		"existing": {id: "existing", userID: "user-1", scope: entities.ScopeUser, status: "running", tags: map[string]string{"slack_thread_ts": "123"}},
	}}
	controller := controllers.NewSessionController(&routeSessionManagerProvider{manager: manager}, nil)
	body := `{"reuse_match_tags":{"slack_thread_ts":"123"},"reuse_message":"follow up","stop_before_reuse":true}`
	req := httptest.NewRequest(http.MethodPost, "/start", strings.NewReader(body))
	req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
	rec := httptest.NewRecorder()
	ctx := echo.New().NewContext(req, rec)
	ctx.Set("authz_context", &auth.AuthorizationContext{
		User:          entities.NewUser("user-1", entities.UserTypeRegular, "user-1"),
		PersonalScope: auth.PersonalScopeAuth{UserID: "user-1", CanCreate: true, CanRead: true},
	})

	if err := controller.StartSession(ctx); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(manager.calls, []string{"stop", "send"}) {
		t.Fatalf("reuse calls = %#v", manager.calls)
	}
}

type deletionRouteRepo struct {
	route   *repositories.SessionRoute
	saved   bool
	deleted bool
}

type listRouteRepo struct {
	routes []*repositories.SessionRoute
}

type filteringListRouteRepo struct {
	routes     []*repositories.SessionRoute
	lastUserID string
	lastFilter repositories.SessionRouteFilter
}

func (r *filteringListRouteRepo) Save(context.Context, *repositories.SessionRoute) error { return nil }
func (r *filteringListRouteRepo) Get(context.Context, string) (*repositories.SessionRoute, error) {
	return nil, nil
}
func (r *filteringListRouteRepo) List(_ context.Context, userID string) ([]*repositories.SessionRoute, error) {
	r.lastUserID = userID
	if userID == "" {
		return r.routes, nil
	}
	var filtered []*repositories.SessionRoute
	for _, route := range r.routes {
		if route.UserID == userID {
			filtered = append(filtered, route)
		}
	}
	return filtered, nil
}
func (r *filteringListRouteRepo) ListFiltered(_ context.Context, filter repositories.SessionRouteFilter) ([]*repositories.SessionRoute, error) {
	r.lastFilter = filter
	var filtered []*repositories.SessionRoute
	for _, route := range r.routes {
		if filter.Scope != "" && route.Scope != filter.Scope {
			continue
		}
		if filter.TeamID != "" && route.TeamID != filter.TeamID {
			continue
		}
		if len(filter.TeamIDs) > 0 {
			matched := false
			for _, teamID := range filter.TeamIDs {
				if route.TeamID == teamID {
					matched = true
					break
				}
			}
			if !matched {
				continue
			}
		}
		if filter.UserID != "" && route.UserID != filter.UserID {
			continue
		}
		filtered = append(filtered, route)
	}
	return filtered, nil
}

func TestSearchSessionsPushesPersonalOwnerIntoRouteQuery(t *testing.T) {
	routeRepo := &filteringListRouteRepo{routes: []*repositories.SessionRoute{
		{SessionID: "mine", UserID: "user-1", Scope: string(entities.ScopeUser), InitialMessage: "original", Annotations: entities.SessionAnnotations{Description: "annotated", RunningTask: "testing"}},
		{SessionID: "not-mine", UserID: "user-2", Scope: string(entities.ScopeUser)},
	}}
	controller := controllers.NewSessionController(
		&routeSessionManagerProvider{manager: &fakeSessionManager{sessions: map[string]*fakeSession{}}}, nil,
		controllers.WithSessionRouteRepository(routeRepo),
	)
	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/search", nil)
	rec := httptest.NewRecorder()
	ctx := e.NewContext(req, rec)
	ctx.Set("authz_context", &auth.AuthorizationContext{PersonalScope: auth.PersonalScopeAuth{UserID: "user-1", CanRead: true}})

	require.NoError(t, controller.SearchSessions(ctx))
	require.Equal(t, "user-1", routeRepo.lastFilter.UserID)
	require.Equal(t, string(entities.ScopeUser), routeRepo.lastFilter.Scope)
	require.Contains(t, rec.Body.String(), `"annotations":{"description":"annotated","running_task":"testing"}`)
	require.Contains(t, rec.Body.String(), `"metadata":{"description":"annotated"}`)
}

func TestSearchSessionsPushesAuthorizedTeamsIntoRouteQuery(t *testing.T) {
	routeRepo := &filteringListRouteRepo{routes: []*repositories.SessionRoute{
		{SessionID: "mine", UserID: "user-1", Scope: string(entities.ScopeTeam), TeamID: "acme/a"},
		{SessionID: "outside", UserID: "user-2", Scope: string(entities.ScopeTeam), TeamID: "other/team"},
	}}
	controller := controllers.NewSessionController(
		&routeSessionManagerProvider{manager: &fakeSessionManager{sessions: map[string]*fakeSession{}}}, nil,
		controllers.WithSessionRouteRepository(routeRepo),
	)
	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/search?scope=team", nil)
	rec := httptest.NewRecorder()
	ctx := e.NewContext(req, rec)
	ctx.Set("authz_context", &auth.AuthorizationContext{
		PersonalScope: auth.PersonalScopeAuth{UserID: "user-1", CanRead: true},
		TeamScope:     auth.TeamScopeAuth{Teams: []string{"acme/a", "acme/b"}},
	})

	require.NoError(t, controller.SearchSessions(ctx))
	require.ElementsMatch(t, []string{"acme/a", "acme/b"}, routeRepo.lastFilter.TeamIDs)
}
