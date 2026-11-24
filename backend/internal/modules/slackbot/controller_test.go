package slackbot

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/labstack/echo/v4"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/takutakahashi/agentapi-proxy/internal/domain/entities"
	portrepos "github.com/takutakahashi/agentapi-proxy/internal/usecases/ports/repositories"
	"github.com/takutakahashi/agentapi-proxy/pkg/auth"
)

// --- Mock SlackBot repository ---

type mockSlackBotRepository struct {
	bots         map[string]*entities.SlackBot
	errOn        string // if set, returns error for this method
	listAllCalls int
}

func newMockSlackBotRepository() *mockSlackBotRepository {
	return &mockSlackBotRepository{bots: make(map[string]*entities.SlackBot)}
}

func (r *mockSlackBotRepository) Create(_ context.Context, bot *entities.SlackBot) error {
	if r.errOn == "Create" {
		return errors.New("storage error")
	}
	r.bots[bot.ID()] = bot
	return nil
}

func (r *mockSlackBotRepository) Get(_ context.Context, id string) (*entities.SlackBot, error) {
	if r.errOn == "Get" {
		return nil, errors.New("storage error")
	}
	bot, ok := r.bots[id]
	if !ok {
		return nil, entities.ErrSlackBotNotFound{ID: id}
	}
	return bot, nil
}

func (r *mockSlackBotRepository) List(_ context.Context, filter portrepos.SlackBotFilter) ([]*entities.SlackBot, error) {
	if r.errOn == "List" {
		return nil, errors.New("storage error")
	}
	var result []*entities.SlackBot
	listAll := filter.UserID == "" &&
		len(filter.TeamIDs) == 0 &&
		filter.Status == "" &&
		filter.Scope == "" &&
		filter.TeamID == ""
	for _, bot := range r.bots {
		if listAll {
			result = append(result, bot)
			continue
		}

		accessible := false
		if bot.Scope() == entities.ScopeTeam && len(filter.TeamIDs) > 0 {
			for _, teamID := range filter.TeamIDs {
				if bot.TeamID() == teamID {
					accessible = true
					break
				}
			}
			if !accessible && filter.UserID != "" && bot.UserID() == filter.UserID {
				accessible = true
			}
		} else {
			if filter.UserID == "" || bot.UserID() == filter.UserID {
				accessible = true
			}
		}
		if !accessible {
			continue
		}
		// Apply additional explicit filters (mirrors KubernetesSlackBotRepository.List)
		if filter.Status != "" && bot.Status() != filter.Status {
			continue
		}
		if filter.Scope != "" && bot.Scope() != filter.Scope {
			continue
		}
		if filter.TeamID != "" && bot.TeamID() != filter.TeamID {
			continue
		}
		result = append(result, bot)
	}
	return result, nil
}

func (r *mockSlackBotRepository) ListAll(_ context.Context) ([]*entities.SlackBot, error) {
	r.listAllCalls++
	if r.errOn == "ListAll" {
		return nil, errors.New("storage error")
	}
	result := make([]*entities.SlackBot, 0, len(r.bots))
	for _, bot := range r.bots {
		result = append(result, bot)
	}
	return result, nil
}

func (r *mockSlackBotRepository) Update(_ context.Context, bot *entities.SlackBot) error {
	if r.errOn == "Update" {
		return errors.New("storage error")
	}
	if _, ok := r.bots[bot.ID()]; !ok {
		return entities.ErrSlackBotNotFound{ID: bot.ID()}
	}
	r.bots[bot.ID()] = bot
	return nil
}

func (r *mockSlackBotRepository) Delete(_ context.Context, id string) error {
	if r.errOn == "Delete" {
		return errors.New("storage error")
	}
	if _, ok := r.bots[id]; !ok {
		return entities.ErrSlackBotNotFound{ID: id}
	}
	delete(r.bots, id)
	return nil
}

// --- Test helpers ---

func makeSlackBotEchoContext(t *testing.T, method, path string, body interface{}, userID string) (echo.Context, *httptest.ResponseRecorder) {
	t.Helper()
	return makeSlackBotEchoContextWithTeams(t, method, path, body, userID, nil)
}

// makeSlackBotEchoContextWithTeams creates an echo context with user ID and team IDs set in auth context.
func makeSlackBotEchoContextWithTeams(t *testing.T, method, path string, body interface{}, userID string, teamIDs []string) (echo.Context, *httptest.ResponseRecorder) {
	t.Helper()
	e := echo.New()
	var reqBody *bytes.Reader
	if body != nil {
		b, err := json.Marshal(body)
		require.NoError(t, err)
		reqBody = bytes.NewReader(b)
	} else {
		reqBody = bytes.NewReader([]byte("{}"))
	}
	req := httptest.NewRequest(method, path, reqBody)
	req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	if userID != "" {
		user := entities.NewUser(entities.UserID(userID), entities.UserTypeAPIKey, userID)
		c.Set("internal_user", user)

		authzCtx := &auth.AuthorizationContext{
			User: user,
			PersonalScope: auth.PersonalScopeAuth{
				UserID:    userID,
				CanCreate: true,
				CanRead:   true,
				CanUpdate: true,
				CanDelete: true,
			},
			TeamScope: auth.TeamScopeAuth{
				Teams:           teamIDs,
				TeamPermissions: make(map[string]auth.TeamPermissions),
				IsAdmin:         false,
			},
		}
		for _, tid := range teamIDs {
			authzCtx.TeamScope.TeamPermissions[tid] = auth.TeamPermissions{
				TeamID:    tid,
				CanCreate: true,
				CanRead:   true,
				CanUpdate: true,
				CanDelete: true,
			}
		}
		c.Set("authz_context", authzCtx)
	}
	return c, rec
}

func TestSlackBotControllerSimulate(t *testing.T) {
	repo := newMockSlackBotRepository()
	bot := entities.NewSlackBot("bot-1", "debug", "owner")
	bot.SetAllowedChannelNames([]string{"dev"})
	require.NoError(t, repo.Create(context.Background(), bot))
	controller := NewSlackBotController(repo)
	body := SlackBotSimulationRequest{ChannelName: "dev", Event: SlackEvent{Type: "app_mention", User: "U1", Channel: "C1", Text: "hello", Ts: "1.0"}}
	ctx, rec := makeSlackBotEchoContext(t, http.MethodPost, "/slackbots/bot-1/simulate", body, "owner")
	ctx.SetParamNames("id")
	ctx.SetParamValues("bot-1")

	require.NoError(t, controller.SimulateSlackBot(ctx))
	assert.Equal(t, http.StatusOK, rec.Code)
	var response SlackBotSimulationResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &response))
	assert.Equal(t, simulationDecisionCreateOrReuse, response.Decision)
	assert.True(t, response.DryRun)
}

func TestSlackBotControllerSimulateRejectsOtherOwner(t *testing.T) {
	repo := newMockSlackBotRepository()
	bot := entities.NewSlackBot("bot-1", "debug", "owner")
	require.NoError(t, repo.Create(context.Background(), bot))
	controller := NewSlackBotController(repo)
	body := SlackBotSimulationRequest{Event: SlackEvent{Type: "message", User: "U1", Channel: "C1", Ts: "1.0"}}
	ctx, _ := makeSlackBotEchoContext(t, http.MethodPost, "/slackbots/bot-1/simulate", body, "other")
	ctx.SetParamNames("id")
	ctx.SetParamValues("bot-1")

	err := controller.SimulateSlackBot(ctx)
	var httpErr *echo.HTTPError
	require.ErrorAs(t, err, &httpErr)
	assert.Equal(t, http.StatusForbidden, httpErr.Code)
}

// --- CreateSlackBot tests ---

func TestCreateSlackBot_Success(t *testing.T) {
	repo := newMockSlackBotRepository()
	controller := NewSlackBotController(repo)

	c, rec := makeSlackBotEchoContext(t, http.MethodPost, "/slackbots", CreateSlackBotRequest{
		Name: "My Bot",
	}, "user-1")

	err := controller.CreateSlackBot(c)
	require.NoError(t, err)
	assert.Equal(t, http.StatusCreated, rec.Code)

	var resp SlackBotResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	assert.Equal(t, "My Bot", resp.Name)
	assert.Equal(t, "user-1", resp.UserID)
	assert.NotEmpty(t, resp.ID)
	assert.Equal(t, entities.SlackBotStatusActive, resp.Status)
}

func TestCreateSlackBot_Paused(t *testing.T) {
	repo := newMockSlackBotRepository()
	controller := NewSlackBotController(repo)

	c, rec := makeSlackBotEchoContext(t, http.MethodPost, "/slackbots", CreateSlackBotRequest{
		Name:   "Paused Bot",
		Status: entities.SlackBotStatusPaused,
	}, "user-1")

	err := controller.CreateSlackBot(c)
	require.NoError(t, err)
	assert.Equal(t, http.StatusCreated, rec.Code)

	var resp SlackBotResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	assert.Equal(t, entities.SlackBotStatusPaused, resp.Status)
}

func TestCreateSlackBot_InvalidStatus(t *testing.T) {
	repo := newMockSlackBotRepository()
	controller := NewSlackBotController(repo)

	c, _ := makeSlackBotEchoContext(t, http.MethodPost, "/slackbots", CreateSlackBotRequest{
		Name:   "Invalid Bot",
		Status: entities.SlackBotStatus("invalid"),
	}, "user-1")

	err := controller.CreateSlackBot(c)
	assertHTTPError(t, err, http.StatusBadRequest)
}

func TestCreateSlackBot_MissingName(t *testing.T) {
	repo := newMockSlackBotRepository()
	controller := NewSlackBotController(repo)

	c, _ := makeSlackBotEchoContext(t, http.MethodPost, "/slackbots", CreateSlackBotRequest{
		// Name is missing
	}, "user-1")

	err := controller.CreateSlackBot(c)
	assertHTTPError(t, err, http.StatusBadRequest)
}

func TestCreateSlackBot_Unauthenticated(t *testing.T) {
	repo := newMockSlackBotRepository()
	controller := NewSlackBotController(repo)

	c, _ := makeSlackBotEchoContext(t, http.MethodPost, "/slackbots", CreateSlackBotRequest{
		Name: "My Bot",
	}, "") // no user_id

	err := controller.CreateSlackBot(c)
	assertHTTPError(t, err, http.StatusUnauthorized)
}

func TestCreateSlackBot_WithAllOptionalFields(t *testing.T) {
	repo := newMockSlackBotRepository()
	controller := NewSlackBotController(repo)

	c, rec := makeSlackBotEchoContext(t, http.MethodPost, "/slackbots", CreateSlackBotRequest{
		Name:                "Full Bot",
		BotTokenSecretName:  "my-k8s-secret",
		BotTokenSecretKey:   "xoxb-token",
		AllowedEventTypes:   []string{"message", "app_mention"},
		AllowedChannelNames: []string{"dev-alerts"},
		MaxSessions:         5,
		SessionConfig: &SlackBotSessionConfig{
			InitialMessageTemplate: "Hello from Slack: {{.event.text}}",
			ReuseMessageTemplate:   "Continue: {{.event.text}}",
			Tags:                   map[string]string{"team": "engineering"},
			Environment:            map[string]string{"LOG_LEVEL": "debug"},
			SessionProfileID:       "profile-slack",
			Params:                 &SlackBotSessionParams{Pool: "pool-a", ManagerID: "manager-a"},
		},
	}, "user-1")

	err := controller.CreateSlackBot(c)
	require.NoError(t, err)
	assert.Equal(t, http.StatusCreated, rec.Code)

	var resp SlackBotResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	assert.Equal(t, "Full Bot", resp.Name)
	assert.Equal(t, "my-k8s-secret", resp.BotTokenSecretName)
	assert.Equal(t, "xoxb-token", resp.BotTokenSecretKey)
	assert.Equal(t, []string{"message", "app_mention"}, resp.AllowedEventTypes)
	assert.Equal(t, []string{"dev-alerts"}, resp.AllowedChannelNames)
	assert.Equal(t, 5, resp.MaxSessions)
	require.NotNil(t, resp.SessionConfig)
	assert.Equal(t, "Hello from Slack: {{.event.text}}", resp.SessionConfig.InitialMessageTemplate)
	assert.Equal(t, "Continue: {{.event.text}}", resp.SessionConfig.ReuseMessageTemplate)
	assert.Equal(t, "engineering", resp.SessionConfig.Tags["team"])
	assert.Equal(t, "debug", resp.SessionConfig.Environment["LOG_LEVEL"])
	assert.Equal(t, "profile-slack", resp.SessionConfig.SessionProfileID)
	require.NotNil(t, resp.SessionConfig.Params)
	assert.Equal(t, "pool-a", resp.SessionConfig.Params.Pool)
	assert.Equal(t, "manager-a", resp.SessionConfig.Params.ManagerID)
}

func TestCreateSlackBot_WithTeamScope(t *testing.T) {
	repo := newMockSlackBotRepository()
	controller := NewSlackBotController(repo)

	c, rec := makeSlackBotEchoContext(t, http.MethodPost, "/slackbots", CreateSlackBotRequest{
		Name:   "Team Bot",
		Scope:  entities.ScopeTeam,
		TeamID: "myorg/backend",
	}, "user-1")

	err := controller.CreateSlackBot(c)
	require.NoError(t, err)
	assert.Equal(t, http.StatusCreated, rec.Code)

	var resp SlackBotResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	assert.Equal(t, string(entities.ScopeTeam), string(resp.Scope))
	assert.Equal(t, "myorg/backend", resp.TeamID)
}
