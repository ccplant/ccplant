package schedule

import (
	"log"
	"net/http"
	"time"

	"github.com/labstack/echo/v4"
	"github.com/takutakahashi/agentapi-proxy/internal/domain/entities"
	sessionuc "github.com/takutakahashi/agentapi-proxy/internal/usecases/session"
	"github.com/takutakahashi/agentapi-proxy/pkg/auth"
)

func (h *Handlers) UpdateSchedule(c echo.Context) error {
	h.setCORSHeaders(c)

	id := c.Param("id")
	if id == "" {
		return echo.NewHTTPError(http.StatusBadRequest, "Schedule ID is required")
	}

	schedule, err := h.manager.Get(c.Request().Context(), id)
	if err != nil {
		if _, ok := err.(ErrScheduleNotFound); ok {
			return echo.NewHTTPError(http.StatusNotFound, "Schedule not found")
		}
		return echo.NewHTTPError(http.StatusInternalServerError, "Failed to get schedule")
	}

	// Check authorization
	if !h.userCanAccessSchedule(c, schedule) {
		return echo.NewHTTPError(http.StatusForbidden, "You don't have permission to update this schedule")
	}

	var req UpdateScheduleRequest
	if err := c.Bind(&req); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "Invalid request body")
	}

	// Apply updates
	if req.Name != nil {
		schedule.Name = *req.Name
	}
	if req.Status != nil {
		schedule.Status = *req.Status
	}
	if req.ScheduledAt != nil {
		schedule.ScheduledAt = req.ScheduledAt
	}
	if req.CronExpr != nil {
		// Validate new cron expression
		if *req.CronExpr != "" {
			parser := NewCronParser()
			if err := parser.Validate(*req.CronExpr); err != nil {
				return echo.NewHTTPError(http.StatusBadRequest, "invalid cron expression: "+err.Error())
			}
		}
		schedule.CronExpr = *req.CronExpr
	}
	if req.Timezone != nil {
		if *req.Timezone != "" {
			if _, err := time.LoadLocation(*req.Timezone); err != nil {
				return echo.NewHTTPError(http.StatusBadRequest, "invalid timezone: "+*req.Timezone)
			}
		}
		schedule.Timezone = *req.Timezone
	}
	if req.SessionConfig != nil {
		schedule.SessionConfig = *req.SessionConfig
	}

	// Refresh UserTeams from the current auth context so that team membership
	// changes since schedule creation are picked up.
	if schedule.GetScope() != entities.ScopeTeam {
		if azCtx := auth.GetAuthorizationContext(c); azCtx != nil {
			schedule.UserTeams = azCtx.TeamScope.Teams
		}
	}

	// Recalculate next execution if schedule changed
	if req.ScheduledAt != nil || req.CronExpr != nil || req.Status != nil {
		nextAt, err := CalculateNextExecution(schedule, h.clock.Now())
		if err != nil {
			return echo.NewHTTPError(http.StatusBadRequest, "failed to calculate next execution: "+err.Error())
		}
		schedule.NextExecutionAt = nextAt
	}

	if err := h.manager.Update(c.Request().Context(), schedule); err != nil {
		log.Printf("Failed to update schedule %s: %v", id, err)
		return echo.NewHTTPError(http.StatusInternalServerError, "Failed to update schedule")
	}

	log.Printf("Updated schedule %s", id)

	return c.JSON(http.StatusOK, h.toResponse(schedule))
}

// DeleteSchedule handles DELETE /schedules/:id
func (h *Handlers) DeleteSchedule(c echo.Context) error {
	h.setCORSHeaders(c)

	id := c.Param("id")
	if id == "" {
		return echo.NewHTTPError(http.StatusBadRequest, "Schedule ID is required")
	}

	schedule, err := h.manager.Get(c.Request().Context(), id)
	if err != nil {
		if _, ok := err.(ErrScheduleNotFound); ok {
			return echo.NewHTTPError(http.StatusNotFound, "Schedule not found")
		}
		return echo.NewHTTPError(http.StatusInternalServerError, "Failed to get schedule")
	}

	// Check authorization
	if !h.userCanAccessSchedule(c, schedule) {
		return echo.NewHTTPError(http.StatusForbidden, "You don't have permission to delete this schedule")
	}

	if err := h.manager.Delete(c.Request().Context(), id); err != nil {
		log.Printf("Failed to delete schedule %s: %v", id, err)
		return echo.NewHTTPError(http.StatusInternalServerError, "Failed to delete schedule")
	}

	log.Printf("Deleted schedule %s", id)

	return c.JSON(http.StatusOK, map[string]interface{}{
		"message": "Schedule deleted successfully",
		"id":      id,
	})
}

// TriggerSchedule handles POST /schedules/:id/trigger
func (h *Handlers) TriggerSchedule(c echo.Context) error {
	h.setCORSHeaders(c)

	id := c.Param("id")
	if id == "" {
		return echo.NewHTTPError(http.StatusBadRequest, "Schedule ID is required")
	}

	schedule, err := h.manager.Get(c.Request().Context(), id)
	if err != nil {
		if _, ok := err.(ErrScheduleNotFound); ok {
			return echo.NewHTTPError(http.StatusNotFound, "Schedule not found")
		}
		return echo.NewHTTPError(http.StatusInternalServerError, "Failed to get schedule")
	}

	// Check authorization
	if !h.userCanAccessSchedule(c, schedule) {
		return echo.NewHTTPError(http.StatusForbidden, "You don't have permission to trigger this schedule")
	}

	// Build the launch request for this manual trigger.
	// For the Teams field we prefer the live auth context (most up-to-date team
	// memberships), falling back to the memberships captured at schedule creation time.
	scheduleScope := schedule.GetScope() // Use GetScope() to handle default value
	sessionID := h.ids.New()
	executedAt := h.clock.Now()

	// Resolve user teams: prefer live auth context, fall back to saved UserTeams
	var userTeams []string
	authzCtx := auth.GetAuthorizationContext(c)
	if authzCtx != nil {
		userTeams = authzCtx.TeamScope.Teams
	} else {
		userTeams = schedule.UserTeams
	}
	teams := sessionuc.ResolveTeams(scheduleScope, schedule.TeamID, userTeams)

	// /start owns reuse. The execution-time tag makes this invocation unique
	// when schedule session reuse is disabled.
	tags := schedule.SessionConfig.SessionTags(schedule.ID, schedule.Name, executedAt)

	var initialMessage, githubToken, agentType, model, pool, managerID string
	var slackParams *entities.SlackParams
	var sandbox *entities.SandboxParams
	var docker *entities.DockerParams
	var authProxy *bool
	var initialMessageWaitSecond *int
	var cycleMessage, sessionTTL string
	var cycleMaxCount int
	if schedule.SessionConfig.Params != nil {
		initialMessage = schedule.SessionConfig.Params.Message
		// For team-scoped schedules, do not use the creator's github_token
		if scheduleScope != entities.ScopeTeam {
			githubToken = schedule.SessionConfig.Params.GithubToken
		}
		agentType = schedule.SessionConfig.Params.AgentType
		model = schedule.SessionConfig.Params.Model
		pool = schedule.SessionConfig.Params.Pool
		managerID = schedule.SessionConfig.Params.ManagerID
		slackParams = schedule.SessionConfig.Params.Slack
		sandbox = schedule.SessionConfig.Params.Sandbox
		docker = schedule.SessionConfig.Params.Docker
		authProxy = schedule.SessionConfig.Params.AuthProxy
		initialMessageWaitSecond = schedule.SessionConfig.Params.InitialMessageWaitSecond
		cycleMessage = schedule.SessionConfig.Params.CycleMessage
		cycleMaxCount = schedule.SessionConfig.Params.CycleMaxCount
		sessionTTL = sessionuc.ResolveSessionTTL(schedule.SessionConfig.Params)
	}

	result, err := h.launcher.Launch(c.Request().Context(), sessionID, sessionuc.LaunchRequest{
		UserID:                   schedule.UserID,
		Scope:                    scheduleScope,
		TeamID:                   schedule.TeamID,
		Teams:                    teams,
		Environment:              schedule.SessionConfig.Environment,
		Tags:                     tags,
		InitialMessage:           initialMessage,
		GithubToken:              githubToken,
		AgentType:                agentType,
		Model:                    model,
		Pool:                     pool,
		ManagerID:                managerID,
		SlackParams:              slackParams,
		Sandbox:                  sandbox,
		Docker:                   docker,
		AuthProxy:                authProxy,
		InitialMessageWaitSecond: initialMessageWaitSecond,
		CycleMessage:             cycleMessage,
		CycleMaxCount:            cycleMaxCount,
		SessionTTL:               sessionTTL,
		RepoInfo:                 extractRepositoryInfo(tags, sessionID),
		SessionProfileID:         schedule.SessionConfig.SessionProfileID,
		ReuseMatchTags:           tags,
		ReuseMessage:             schedule.SessionConfig.EffectiveReuseMessage(),
		ReuseSession:             schedule.SessionConfig.ReuseSession,
		DeferReuseToStart:        true,
	})
	if err != nil {
		log.Printf("Failed to trigger schedule %s: %v", id, err)

		// Record failed execution
		record := ExecutionRecord{
			ExecutedAt: executedAt,
			Status:     "failed",
			Error:      err.Error(),
		}
		_ = h.manager.RecordExecution(c.Request().Context(), id, record)

		return c.JSON(http.StatusServiceUnavailable, map[string]interface{}{
			"message": "Failed to create session: " + err.Error(),
		})
	}

	// Record successful execution
	record := ExecutionRecord{
		ExecutedAt:    executedAt,
		SessionID:     result.SessionID,
		Status:        "success",
		SessionReused: result.SessionReused,
	}
	if err := h.manager.RecordExecution(c.Request().Context(), id, record); err != nil {
		log.Printf("Failed to record execution for schedule %s: %v", id, err)
	}

	if result.SessionReused {
		log.Printf("Manually triggered schedule %s, reused session %s", id, result.SessionID)
	} else {
		log.Printf("Manually triggered schedule %s, created session %s", id, result.SessionID)
	}

	return c.JSON(http.StatusOK, map[string]interface{}{
		"session_id":     result.SessionID,
		"triggered_at":   record.ExecutedAt,
		"session_reused": result.SessionReused,
	})
}

// toResponse converts a Schedule to ScheduleResponse
func (h *Handlers) toResponse(s *Schedule) ScheduleResponse {
	return ScheduleResponse{
		ID:               s.ID,
		Name:             s.Name,
		UserID:           s.UserID,
		Scope:            s.GetScope(), // Use GetScope() to handle default value
		TeamID:           s.TeamID,
		Status:           s.Status,
		ScheduledAt:      s.ScheduledAt,
		CronExpr:         s.CronExpr,
		Timezone:         s.Timezone,
		SessionConfig:    s.SessionConfig,
		NextExecutionAt:  s.NextExecutionAt,
		ExecutionCount:   s.ExecutionCount,
		LastExecution:    s.LastExecution,
		ExecutionHistory: s.ExecutionHistory,
		CreatedAt:        s.CreatedAt,
		UpdatedAt:        s.UpdatedAt,
	}
}

// userCanAccessSchedule checks if the current user can access the schedule
// For team-scoped schedules, all team members have access
// For user-scoped schedules, only the owner has access
func (h *Handlers) userCanAccessSchedule(c echo.Context, schedule *Schedule) bool {
	user := auth.GetUserFromContext(c)
	if user == nil {
		return false
	}
	return user.CanAccessResource(
		entities.UserID(schedule.UserID),
		string(schedule.GetScope()), // Use GetScope() to handle default value
		schedule.TeamID,
	)
}

// setCORSHeaders sets CORS headers for all schedule endpoints
func (h *Handlers) setCORSHeaders(c echo.Context) {
	c.Response().Header().Set("Access-Control-Allow-Origin", "*")
	c.Response().Header().Set("Access-Control-Allow-Methods", "GET, HEAD, PUT, PATCH, POST, DELETE, OPTIONS")
	c.Response().Header().Set("Access-Control-Allow-Headers", "Origin, Content-Type, Accept, Authorization, X-Requested-With, X-Forwarded-For, X-Forwarded-Proto, X-Forwarded-Host, X-API-Key")
	c.Response().Header().Set("Access-Control-Allow-Credentials", "true")
	c.Response().Header().Set("Access-Control-Max-Age", "86400")
}
