package controllers

import (
	"net/http"
	"strings"
	"time"

	"github.com/labstack/echo/v4"
	"github.com/takutakahashi/agentapi-proxy/internal/domain/entities"
	portrepos "github.com/takutakahashi/agentapi-proxy/internal/usecases/ports/repositories"
	"github.com/takutakahashi/agentapi-proxy/pkg/auth"
)

const maxUsageEventsPerRequest = 1000

type UsageController struct {
	repo     portrepos.UsageRepository
	sessions portrepos.SessionManager
}

func (c *UsageController) Get(ctx echo.Context) error {
	authz := auth.GetAuthorizationContext(ctx)
	if authz == nil {
		return echo.NewHTTPError(http.StatusUnauthorized, "authentication required")
	}
	query := entities.UsageQuery{}
	if teamID := ctx.QueryParam("team_id"); teamID != "" {
		if !authz.CanAccessTeam(teamID) {
			return echo.NewHTTPError(http.StatusForbidden, "not authorized for team")
		}
		query.TeamID = teamID
	} else {
		query.UserID = authz.PersonalScope.UserID
	}
	if err := bindUsageRange(ctx, &query); err != nil {
		return err
	}
	return c.aggregate(ctx, query)
}

func (c *UsageController) GetSession(ctx echo.Context) error {
	session := c.sessions.GetSession(ctx.Param("sessionId"))
	if session == nil {
		return echo.NewHTTPError(http.StatusNotFound, "session not found")
	}
	authz := auth.GetAuthorizationContext(ctx)
	if authz == nil || !authz.CanAccessResource(session.UserID(), string(session.Scope()), session.TeamID()) {
		return echo.NewHTTPError(http.StatusForbidden, "not authorized for session")
	}
	query := entities.UsageQuery{SessionID: session.ID()}
	if err := bindUsageRange(ctx, &query); err != nil {
		return err
	}
	return c.aggregate(ctx, query)
}

func bindUsageRange(ctx echo.Context, query *entities.UsageQuery) error {
	for name, target := range map[string]**time.Time{"from": &query.From, "to": &query.To} {
		value := ctx.QueryParam(name)
		if value == "" {
			continue
		}
		parsed, err := time.Parse(time.RFC3339, value)
		if err != nil {
			return echo.NewHTTPError(http.StatusBadRequest, name+" must be RFC3339")
		}
		*target = &parsed
	}
	if query.From != nil && query.To != nil && !query.From.Before(*query.To) {
		return echo.NewHTTPError(http.StatusBadRequest, "from must be before to")
	}
	return nil
}

func (c *UsageController) aggregate(ctx echo.Context, query entities.UsageQuery) error {
	summary, err := c.repo.Aggregate(ctx.Request().Context(), query)
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "failed to aggregate usage")
	}
	return ctx.JSON(http.StatusOK, summary)
}

func NewUsageController(repo portrepos.UsageRepository, sessions portrepos.SessionManager) *UsageController {
	return &UsageController{repo: repo, sessions: sessions}
}

func (c *UsageController) Create(ctx echo.Context) error {
	var batch entities.UsageEventBatch
	if err := ctx.Bind(&batch); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "invalid usage payload")
	}
	if batch.SessionID == "" || len(batch.Events) == 0 || len(batch.Events) > maxUsageEventsPerRequest {
		return echo.NewHTTPError(http.StatusBadRequest, "session_id and 1-1000 events are required")
	}
	session := c.sessions.GetSession(batch.SessionID)
	if session == nil {
		return echo.NewHTTPError(http.StatusNotFound, "session not found")
	}
	authz := auth.GetAuthorizationContext(ctx)
	if authz == nil || !authz.CanModifyResource(session.UserID(), string(session.Scope()), session.TeamID()) {
		return echo.NewHTTPError(http.StatusForbidden, "not authorized for session")
	}
	for i := range batch.Events {
		event := &batch.Events[i]
		if strings.TrimSpace(event.EventID) == "" || strings.TrimSpace(event.Model) == "" ||
			event.InputTokens < 0 || event.OutputTokens < 0 || event.CachedInputTokens < 0 ||
			event.CacheCreationTokens < 0 || event.ReasoningTokens < 0 || event.OccurredAt.IsZero() {
			return echo.NewHTTPError(http.StatusBadRequest, "invalid usage event")
		}
		event.SessionID = batch.SessionID
		event.UserID = session.UserID()
		event.Scope = string(session.Scope())
		event.TeamID = session.TeamID()
	}
	result, err := c.repo.InsertEvents(ctx.Request().Context(), batch.Events)
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "failed to store usage events")
	}
	return ctx.JSON(http.StatusOK, result)
}
