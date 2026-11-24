package controllers

import (
	"context"
	"log"
	"sort"
	"strings"

	"github.com/labstack/echo/v4"
	"github.com/takutakahashi/agentapi-proxy/internal/domain/entities"
	"github.com/takutakahashi/agentapi-proxy/internal/infrastructure/services"
	"github.com/takutakahashi/agentapi-proxy/internal/usecases/ports/repositories"
	"github.com/takutakahashi/agentapi-proxy/pkg/auth"
	"github.com/takutakahashi/agentapi-proxy/pkg/telemetry"
)

type sessionSearchQuery struct {
	ctx        echo.Context
	authz      *auth.AuthorizationContext
	status     string
	scope      string
	teamID     string
	userID     string
	teamIDs    []string
	tagFilters map[string]string
}

func newSessionSearchQuery(ctx echo.Context) sessionSearchQuery {
	authzCtx := auth.GetAuthorizationContext(ctx)
	scopeFilter, teamIDFilter := ctx.QueryParam("scope"), ctx.QueryParam("team_id")
	resolvedScope, resolvedTeamID := authzCtx.ResolveScope(scopeFilter, teamIDFilter)
	if resolvedScope != scopeFilter || resolvedTeamID != teamIDFilter {
		log.Printf("[SESSION_DEBUG] Service account %s search: routing scope %q → %q (team %q)", authzCtx.PersonalScope.UserID, scopeFilter, resolvedScope, resolvedTeamID)
	}
	query := sessionSearchQuery{
		ctx: ctx, authz: authzCtx, status: ctx.QueryParam("status"), scope: resolvedScope,
		teamID: resolvedTeamID, userID: authzCtx.PersonalScope.UserID, teamIDs: authzCtx.TeamScope.Teams,
		tagFilters: searchTagFilters(ctx),
	}
	telemetry.SetAttributes(ctx.Request().Context(), telemetry.Int64("search.tag_filter_count", int64(len(query.tagFilters))))
	return query
}

func searchTagFilters(ctx echo.Context) map[string]string {
	filters := make(map[string]string)
	for name, values := range ctx.QueryParams() {
		if strings.HasPrefix(name, "tag.") && len(values) > 0 {
			filters[strings.TrimPrefix(name, "tag.")] = values[0]
		}
	}
	return filters
}

func (query sessionSearchQuery) sessionFilter() entities.SessionFilter {
	filter := entities.SessionFilter{
		Status: query.status, Tags: query.tagFilters, Scope: entities.ResourceScope(query.scope),
		TeamID: query.teamID, TeamIDs: query.teamIDs,
	}
	if query.scope != "team" && query.teamID == "" {
		filter.UserID = query.userID
	}
	return filter
}

func (c *SessionController) listMatchingSessions(query sessionSearchQuery) []entities.Session {
	requestCtx := query.ctx.Request().Context()
	sessions, err := telemetry.Operation(requestCtx, "controllers.SessionController.SearchSessions.ListLocalSessions", func(context.Context) ([]entities.Session, error) {
		return c.getSessionManager().ListSessions(query.sessionFilter()), nil
	})
	if err != nil {
		log.Printf("[SEARCH] Failed to list local sessions: %v", err)
		return nil
	}
	telemetry.SetAttributes(requestCtx, telemetry.Int64("search.local_session_count", int64(len(sessions))))
	matching, err := telemetry.Operation(requestCtx, "controllers.SessionController.SearchSessions.FilterLocalSessions", func(context.Context) ([]entities.Session, error) {
		return query.authorizedSessions(sessions), nil
	})
	if err != nil {
		log.Printf("[SEARCH] Failed to filter local sessions: %v", err)
		return nil
	}
	if _, requested := query.tagFilters["hidden"]; !requested {
		matching = filterHiddenSessions(matching)
	}
	telemetry.SetAttributes(requestCtx, telemetry.Int64("search.matching_local_session_count", int64(len(matching))))
	return matching
}

func (query sessionSearchQuery) authorizedSessions(sessions []entities.Session) []entities.Session {
	matching := make([]entities.Session, 0, len(sessions))
	for _, session := range sessions {
		if query.authorizedSession(session) {
			matching = append(matching, session)
		}
	}
	return matching
}

func (query sessionSearchQuery) authorizedSession(session entities.Session) bool {
	scope := session.Scope()
	if query.scope == string(entities.ScopeTeam) {
		if scope != entities.ScopeTeam || query.teamID != "" && session.TeamID() != query.teamID {
			return false
		}
	} else if scope == entities.ScopeTeam {
		return false
	}
	return query.authz.CanAccessResource(session.UserID(), string(scope), session.TeamID())
}

func (c *SessionController) listSearchRoutes(query sessionSearchQuery, sessions []entities.Session) ([]*repositories.SessionRoute, []entities.Session, map[string]entities.Session) {
	allocated := make(map[string]entities.Session)
	if c.sessionRouteRepo == nil {
		return nil, sessions, allocated
	}
	routes, err := telemetry.Operation(query.ctx.Request().Context(), "controllers.SessionController.SearchSessions.ListRoutes", func(routeCtx context.Context) ([]*repositories.SessionRoute, error) {
		return c.querySessionRoutes(routeCtx, query)
	})
	if err != nil {
		log.Printf("[SEARCH] Failed to list session routes: %v", err)
		return nil, sessions, allocated
	}
	telemetry.SetAttributes(query.ctx.Request().Context(), telemetry.Int64("search.route_count", int64(len(routes))))
	telemetry.OperationVoid(query.ctx.Request().Context(), "controllers.SessionController.SearchSessions.ReconcileRoutes", func(reconcileCtx context.Context) {
		sessions = excludeTemplatedSessions(sessions, routes)
		routes = c.activeSearchRoutes(reconcileCtx, routes)
		allocated = indexAllocatedSessions(sessions, routes)
		sessions = excludeAllocatedSessions(sessions, routes)
	})
	return routes, sessions, allocated
}

func (c *SessionController) querySessionRoutes(ctx context.Context, query sessionSearchQuery) ([]*repositories.SessionRoute, error) {
	filtered, supportsFilter := c.sessionRouteRepo.(repositories.FilteredSessionRouteRepository)
	if query.scope == string(entities.ScopeTeam) {
		if !supportsFilter {
			return c.sessionRouteRepo.List(ctx, "")
		}
		filter := repositories.SessionRouteFilter{Scope: string(entities.ScopeTeam), TeamID: query.teamID}
		if query.teamID == "" && !query.authz.TeamScope.IsAdmin {
			filter.TeamIDs = query.teamIDs
		}
		return filtered.ListFiltered(ctx, filter)
	}
	if supportsFilter {
		return filtered.ListFiltered(ctx, repositories.SessionRouteFilter{UserID: query.userID, Scope: string(entities.ScopeUser)})
	}
	return c.sessionRouteRepo.List(ctx, query.userID)
}

func (c *SessionController) activeSearchRoutes(ctx context.Context, routes []*repositories.SessionRoute) []*repositories.SessionRoute {
	active := routes[:0]
	for _, route := range routes {
		if route.Status != "templated" && !c.reconcileQueuedDeletion(ctx, route) {
			active = append(active, route)
		}
	}
	return active
}

func (c *SessionController) buildSearchResponse(query sessionSearchQuery, sessions []entities.Session, routes []*repositories.SessionRoute, allocated map[string]entities.Session) []map[string]interface{} {
	result := make([]map[string]interface{}, 0, len(sessions)+len(routes))
	modelOptions := make(map[string][]string)
	localIDs := make(map[string]struct{}, len(sessions))
	telemetry.OperationVoid(query.ctx.Request().Context(), "controllers.SessionController.SearchSessions.BuildLocalResponse", func(ctx context.Context) {
		sort.Slice(sessions, func(i, j int) bool { return sessions[i].StartedAt().After(sessions[j].StartedAt()) })
		for _, session := range sessions {
			localIDs[session.ID()] = struct{}{}
			result = append(result, c.localSessionData(ctx, session, modelOptions))
		}
	})
	telemetry.OperationVoid(query.ctx.Request().Context(), "controllers.SessionController.SearchSessions.BuildRouteResponse", func(ctx context.Context) {
		for _, route := range routes {
			if query.routeMatches(route, localIDs) {
				result = append(result, c.routeSessionData(ctx, route, allocated, modelOptions))
			}
		}
	})
	return result
}

func (c *SessionController) localSessionData(ctx context.Context, session entities.Session, modelCache map[string][]string) map[string]interface{} {
	annotations := getSessionAnnotations(session)
	description := session.Description()
	if annotations.Description != "" {
		description = annotations.Description
	}
	data := map[string]interface{}{
		"session_id": session.ID(), "user_id": session.UserID(), "scope": session.Scope(), "team_id": session.TeamID(),
		"status": session.Status(), "started_at": session.StartedAt(), "updated_at": session.UpdatedAt(),
		"last_message_at": session.LastMessageAt(), "addr": session.Addr(), "tags": session.Tags(), "annotations": annotations,
		"metadata": map[string]interface{}{"description": description},
	}
	addSessionErrorData(data, session)
	addSessionSandboxData(data, session)
	if options := c.resolveSessionModelOptions(ctx, session, modelCache); len(options) > 0 {
		data["model_options"] = options
	}
	return data
}

func addSessionErrorData(data map[string]interface{}, session entities.Session) {
	if provider, ok := session.(sessionStatusMessageProvider); ok && provider.StatusMessage() != "" {
		data["error_message"] = provider.StatusMessage()
	}
	if provider, ok := session.(sessionErrorCodeProvider); ok && provider.ErrorCode() != "" {
		data["error_code"] = provider.ErrorCode()
	}
}

func addSessionSandboxData(data map[string]interface{}, session entities.Session) {
	if provider, ok := session.(sessionSandboxPolicyProvider); ok {
		if policyID := provider.SandboxPolicyID(); policyID != "" {
			data["sandbox_policy_id"] = policyID
		}
		return
	}
	if kubernetesSession, ok := session.(*services.KubernetesSession); ok {
		if request := kubernetesSession.Request(); request != nil && request.Sandbox != nil {
			data["sandbox_policy_id"] = request.Sandbox.PolicyID
		}
	}
}

func (query sessionSearchQuery) routeMatches(route *repositories.SessionRoute, localIDs map[string]struct{}) bool {
	if _, exists := localIDs[route.SessionID]; exists {
		return false
	}
	teamScope := string(entities.ScopeTeam)
	if query.scope == teamScope && route.Scope != teamScope || query.scope != teamScope && route.Scope == teamScope {
		return false
	}
	if query.teamID != "" && route.TeamID != query.teamID {
		return false
	}
	if !query.authz.CanAccessResource(route.UserID, route.Scope, route.TeamID) {
		return false
	}
	return tagsMatch(route.Tags, query.tagFilters)
}

func tagsMatch(tags, filters map[string]string) bool {
	for key, value := range filters {
		if tags[key] != value {
			return false
		}
	}
	return true
}

func (c *SessionController) routeSessionData(ctx context.Context, route *repositories.SessionRoute, allocated map[string]entities.Session, modelCache map[string][]string) map[string]interface{} {
	tags := route.Tags
	if tags == nil {
		tags = map[string]string{}
	}
	description := route.InitialMessage
	if route.Annotations.Description != "" {
		description = route.Annotations.Description
	}
	data := map[string]interface{}{
		"session_id": route.SessionID, "allocated_session_id": route.RemoteSessionID,
		"manager_id": route.ManagerID, "remote_session_id": route.RemoteSessionID, "user_id": route.UserID,
		"scope": route.Scope, "team_id": route.TeamID, "status": routedSessionStatus(route, allocated),
		"started_at": route.StartedAt, "updated_at": route.StartedAt, "last_message_at": route.StartedAt,
		"addr": "", "tags": tags, "annotations": route.Annotations, "metadata": map[string]interface{}{"description": description},
	}
	if options := c.sessionModelOptionsFromTags(ctx, tags, modelCache); len(options) > 0 {
		data["model_options"] = options
	}
	return data
}
