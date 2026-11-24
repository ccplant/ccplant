package schedule

import (
	"fmt"
	"log"
	"net/http"
	"time"

	"github.com/labstack/echo/v4"
	"github.com/takutakahashi/agentapi-proxy/internal/domain/entities"
	portrepos "github.com/takutakahashi/agentapi-proxy/internal/usecases/ports/repositories"
	sessionuc "github.com/takutakahashi/agentapi-proxy/internal/usecases/session"
	"github.com/takutakahashi/agentapi-proxy/pkg/auth"
)

// Handlers handles schedule management endpoints
type Handlers struct {
	manager         Manager
	sessionManager  portrepos.SessionManager
	launcher        *sessionuc.LaunchUseCase
	defaultTimezone string
	clock           Clock
	ids             IDGenerator
}

// NewHandlers creates a new Handlers instance
func NewHandlers(manager Manager, sessionManager portrepos.SessionManager, sessionProfileRepo portrepos.SessionProfileRepository) *Handlers {
	return &Handlers{
		manager:         manager,
		sessionManager:  sessionManager,
		launcher:        sessionuc.NewLaunchUseCase(sessionManager).WithSessionProfileRepository(sessionProfileRepo),
		defaultTimezone: "Asia/Tokyo",
		clock:           realClock{},
		ids:             uuidGenerator{},
	}
}

// WithSettingsRepository enables personal team-profile opt-in resolution.
func (h *Handlers) WithSettingsRepository(repo portrepos.SettingsRepository) *Handlers {
	h.launcher.WithSettingsRepository(repo)
	return h
}

// NewHandlersWithTimezone creates a new Handlers instance with a custom default timezone
func NewHandlersWithTimezone(manager Manager, sessionManager portrepos.SessionManager, sessionProfileRepo portrepos.SessionProfileRepository, defaultTimezone string) *Handlers {
	return &Handlers{
		manager:         manager,
		sessionManager:  sessionManager,
		launcher:        sessionuc.NewLaunchUseCase(sessionManager).WithSessionProfileRepository(sessionProfileRepo),
		defaultTimezone: defaultTimezone,
		clock:           realClock{},
		ids:             uuidGenerator{},
	}
}

// WithRuntime replaces nondeterministic runtime dependencies. It is intended
// for tests and other controlled execution environments.
func (h *Handlers) WithRuntime(clock Clock, ids IDGenerator) *Handlers {
	if clock != nil {
		h.clock = clock
	}
	if ids != nil {
		h.ids = ids
	}
	return h
}

// GetName returns the name of this handler for logging
func (h *Handlers) GetName() string {
	return "ScheduleHandlers"
}

// RegisterRoutes registers schedule management routes
func (h *Handlers) RegisterRoutes(e *echo.Echo) error {
	g := e.Group("/schedules")

	g.POST("", h.CreateSchedule)
	g.GET("", h.ListSchedules)
	g.GET("/:id", h.GetSchedule)
	g.PUT("/:id", h.UpdateSchedule)
	g.DELETE("/:id", h.DeleteSchedule)
	g.POST("/:id/trigger", h.TriggerSchedule)

	log.Printf("Registered schedule management routes")
	return nil
}

// CreateScheduleRequest represents the request body for creating a schedule
type CreateScheduleRequest struct {
	Name          string                 `json:"name"`
	Scope         entities.ResourceScope `json:"scope,omitempty"`
	TeamID        string                 `json:"team_id,omitempty"`
	Status        *ScheduleStatus        `json:"status,omitempty"`
	ScheduledAt   *time.Time             `json:"scheduled_at,omitempty"`
	CronExpr      string                 `json:"cron_expr,omitempty"`
	Timezone      string                 `json:"timezone,omitempty"`
	SessionConfig SessionConfig          `json:"session_config"`
}

// UpdateScheduleRequest represents the request body for updating a schedule
type UpdateScheduleRequest struct {
	Name          *string         `json:"name,omitempty"`
	Status        *ScheduleStatus `json:"status,omitempty"`
	ScheduledAt   *time.Time      `json:"scheduled_at,omitempty"`
	CronExpr      *string         `json:"cron_expr,omitempty"`
	Timezone      *string         `json:"timezone,omitempty"`
	SessionConfig *SessionConfig  `json:"session_config,omitempty"`
}

// ScheduleResponse represents the response for a schedule
type ScheduleResponse struct {
	ID               string                 `json:"id"`
	Name             string                 `json:"name"`
	UserID           string                 `json:"user_id"`
	Scope            entities.ResourceScope `json:"scope,omitempty"`
	TeamID           string                 `json:"team_id,omitempty"`
	Status           ScheduleStatus         `json:"status"`
	ScheduledAt      *time.Time             `json:"scheduled_at,omitempty"`
	CronExpr         string                 `json:"cron_expr,omitempty"`
	Timezone         string                 `json:"timezone,omitempty"`
	SessionConfig    SessionConfig          `json:"session_config"`
	NextExecutionAt  *time.Time             `json:"next_execution_at,omitempty"`
	ExecutionCount   int                    `json:"execution_count"`
	LastExecution    *ExecutionRecord       `json:"last_execution,omitempty"`
	ExecutionHistory []ExecutionRecord      `json:"execution_history,omitempty"`
	CreatedAt        time.Time              `json:"created_at"`
	UpdatedAt        time.Time              `json:"updated_at"`
}

// CreateSchedule handles POST /schedules
func (h *Handlers) CreateSchedule(c echo.Context) error {
	h.setCORSHeaders(c)

	var req CreateScheduleRequest
	if err := c.Bind(&req); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "Invalid request body")
	}

	// Validate request
	if req.Name == "" {
		return echo.NewHTTPError(http.StatusBadRequest, "name is required")
	}
	if req.ScheduledAt == nil && req.CronExpr == "" {
		return echo.NewHTTPError(http.StatusBadRequest, "either scheduled_at or cron_expr must be set")
	}
	if req.Status != nil && *req.Status != ScheduleStatusActive && *req.Status != ScheduleStatusPaused {
		return echo.NewHTTPError(http.StatusBadRequest, "status must be active or paused")
	}

	// Validate cron expression if provided
	if req.CronExpr != "" {
		parser := NewCronParser()
		if err := parser.Validate(req.CronExpr); err != nil {
			return echo.NewHTTPError(http.StatusBadRequest, "invalid cron expression: "+err.Error())
		}
	}

	// Use default timezone if not provided
	timezone := req.Timezone
	if timezone == "" {
		timezone = h.defaultTimezone
	}

	// Validate timezone
	if _, err := time.LoadLocation(timezone); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "invalid timezone: "+timezone)
	}

	// Get user from context
	user := auth.GetUserFromContext(c)
	var userID string
	if user != nil {
		userID = string(user.ID())
	} else {
		userID = "anonymous"
	}

	// Resolve scope for service accounts: service accounts are transparently routed
	// to team scope so that their resources are owned by the team, not a personal user.
	resolvedScope, resolvedTeamID := auth.ResolveUserScope(user, string(req.Scope), req.TeamID)
	req.Scope = entities.ResourceScope(resolvedScope)
	req.TeamID = resolvedTeamID

	// Validate team scope: user must be a member of the team.
	// Use AuthorizationContext.CanCreateInTeam which handles service accounts correctly
	// (service accounts have their team populated in TeamScope.TeamPermissions).
	if req.Scope == entities.ScopeTeam {
		if req.TeamID == "" {
			return echo.NewHTTPError(http.StatusBadRequest, "team_id is required when scope is 'team'")
		}
		if user == nil {
			return echo.NewHTTPError(http.StatusUnauthorized, "Authentication required for team-scoped schedules")
		}
		if authzCtx := auth.GetAuthorizationContext(c); authzCtx == nil || !authzCtx.CanCreateInTeam(req.TeamID) {
			log.Printf("User %s is not a member of team %s", userID, req.TeamID)
			return echo.NewHTTPError(http.StatusForbidden, "You are not a member of this team")
		}
	}

	// Never persist the request credential in a schedule. Runtime credentials
	// are resolved from managed settings when the worker asks the control API to
	// create a session; bearer tokens are authentication material, not schedule
	// configuration.
	sessionConfig := req.SessionConfig

	// For user-scoped schedules, capture the creator's team memberships so that
	// the background worker can inject team-level settings (Bedrock, MCP, etc.)
	// when executing the schedule without an HTTP auth context.
	var userTeams []string
	if req.Scope != entities.ScopeTeam {
		if azCtx := auth.GetAuthorizationContext(c); azCtx != nil {
			userTeams = azCtx.TeamScope.Teams
		}
	}

	status := ScheduleStatusActive
	if req.Status != nil {
		status = *req.Status
	}

	// Create schedule
	schedule := &Schedule{
		ID:            h.ids.New(),
		Name:          req.Name,
		UserID:        userID,
		Scope:         req.Scope,
		TeamID:        req.TeamID,
		UserTeams:     userTeams,
		Status:        status,
		ScheduledAt:   req.ScheduledAt,
		CronExpr:      req.CronExpr,
		Timezone:      timezone,
		SessionConfig: sessionConfig,
	}

	// Calculate next execution time
	nextAt, err := CalculateNextExecution(schedule, h.clock.Now())
	if err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "failed to calculate next execution: "+err.Error())
	}
	schedule.NextExecutionAt = nextAt

	if err := h.manager.Create(c.Request().Context(), schedule); err != nil {
		log.Printf("Failed to create schedule: %v", err)
		return echo.NewHTTPError(http.StatusInternalServerError, "Failed to create schedule")
	}

	log.Printf("Created schedule %s (%s) for user %s", schedule.ID, schedule.Name, userID)

	return c.JSON(http.StatusCreated, h.toResponse(schedule))
}

// ListSchedules handles GET /schedules
func (h *Handlers) ListSchedules(c echo.Context) error {
	h.setCORSHeaders(c)

	user := auth.GetUserFromContext(c)
	scopeFilter := c.QueryParam("scope")
	teamIDFilter := c.QueryParam("team_id")

	// Resolve scope for service accounts: transparently route to team scope.
	scopeFilter, teamIDFilter = auth.ResolveUserScope(user, scopeFilter, teamIDFilter)

	var userID string
	var userTeamIDs []string
	if user != nil {
		userID = string(user.ID())
		// Extract user's team IDs for filtering team-scoped schedules
		if resolved, ok := user.ResolvedTeamIDs(); ok {
			userTeamIDs = resolved
		} else if githubInfo := user.GitHubInfo(); githubInfo != nil {
			for _, team := range githubInfo.Teams() {
				teamSlug := fmt.Sprintf("%s/%s", team.Organization, team.TeamSlug)
				userTeamIDs = append(userTeamIDs, teamSlug)
			}
		}
	}

	// Build filter (without UserID to get all schedules, we'll filter by authorization below)
	filter := ScheduleFilter{
		Scope:   entities.ResourceScope(scopeFilter),
		TeamID:  teamIDFilter,
		TeamIDs: userTeamIDs,
	}

	// Filter by status if provided
	if status := c.QueryParam("status"); status != "" {
		filter.Status = ScheduleStatus(status)
	}

	// For non-team scope, always filter by user ID - even admins should not see
	// other users' personal schedules. Admin privileges apply to team-scoped resources only.
	if user != nil && scopeFilter != "team" && teamIDFilter == "" {
		filter.UserID = userID
	}

	schedules, err := h.manager.List(c.Request().Context(), filter)
	if err != nil {
		log.Printf("Failed to list schedules: %v", err)
		return echo.NewHTTPError(http.StatusInternalServerError, "Failed to list schedules")
	}

	// Filter by user authorization (supports both user-scoped and team-scoped)
	// IMPORTANT: Resources are isolated by scope - team scope resources are only visible
	// when explicitly filtering by scope=team, and user scope resources are only visible
	// when filtering by scope=user or no scope filter (default to user scope)
	responses := make([]ScheduleResponse, 0, len(schedules))
	for _, s := range schedules {

		// Scope isolation: resources are only visible within their respective scope
		// - scope=team filter: only show team-scoped resources
		// - scope=user filter or no filter: only show user-scoped resources
		scheduleScope := s.GetScope() // Use GetScope() to handle default value
		if scopeFilter == string(entities.ScopeTeam) {
			// Only show team-scoped schedules
			if scheduleScope != entities.ScopeTeam {
				continue
			}
		} else {
			// Default to user scope: only show user-scoped schedules
			if scheduleScope == entities.ScopeTeam {
				continue
			}
		}

		// Admin can access all team-scoped schedules; user-scoped requires ownership.
		// Admin privileges do not extend to other users' personal resources.
		if scheduleScope == entities.ScopeTeam {
			// Team-scoped: admin or team member can see
			if user != nil && (user.IsAdmin() || user.IsMemberOfTeam(s.TeamID)) {
				responses = append(responses, h.toResponse(s))
			}
		} else {
			// User-scoped: only owner can see, regardless of admin status
			if user != nil && s.UserID == string(user.ID()) {
				responses = append(responses, h.toResponse(s))
			}
		}
	}

	return c.JSON(http.StatusOK, map[string]interface{}{
		"schedules": responses,
	})
}

// GetSchedule handles GET /schedules/:id
func (h *Handlers) GetSchedule(c echo.Context) error {
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
		log.Printf("Failed to get schedule %s: %v", id, err)
		return echo.NewHTTPError(http.StatusInternalServerError, "Failed to get schedule")
	}

	// Check authorization
	if !h.userCanAccessSchedule(c, schedule) {
		return echo.NewHTTPError(http.StatusForbidden, "You don't have permission to access this schedule")
	}

	return c.JSON(http.StatusOK, h.toResponse(schedule))
}

// UpdateSchedule handles PUT /schedules/:id
