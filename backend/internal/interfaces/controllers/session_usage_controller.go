package controllers

import (
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/labstack/echo/v4"
	"github.com/takutakahashi/agentapi-proxy/internal/domain/entities"
	portrepos "github.com/takutakahashi/agentapi-proxy/internal/usecases/ports/repositories"
	"github.com/takutakahashi/agentapi-proxy/pkg/auth"
)

const maxSessionRuntimeRange = 90 * 24 * time.Hour

type SessionUsageController struct {
	repository portrepos.SessionRuntimeRepository
	teams      portrepos.TeamConfigRepository
	now        func() time.Time
}

func NewSessionUsageController(repository portrepos.SessionRuntimeRepository, teams portrepos.TeamConfigRepository) *SessionUsageController {
	return &SessionUsageController{repository: repository, teams: teams, now: func() time.Time { return time.Now().UTC() }}
}

func (c *SessionUsageController) GetDashboard(ctx echo.Context) error {
	from, err := time.Parse(time.RFC3339, ctx.QueryParam("from"))
	if err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "from must be RFC3339")
	}
	to, err := time.Parse(time.RFC3339, ctx.QueryParam("to"))
	if err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "to must be RFC3339")
	}
	if !from.Before(to) || to.Sub(from) > maxSessionRuntimeRange {
		return echo.NewHTTPError(http.StatusBadRequest, "usage range must be positive and not exceed 90 days")
	}
	timezone := ctx.QueryParam("timezone")
	if timezone == "" {
		timezone = "UTC"
	}
	location, err := time.LoadLocation(timezone)
	if err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "timezone must be a valid IANA timezone")
	}
	limit := 10
	if value := ctx.QueryParam("breakdown_limit"); value != "" {
		limit, err = strconv.Atoi(value)
		if err != nil || limit < 1 || limit > 50 {
			return echo.NewHTTPError(http.StatusBadRequest, "breakdown_limit must be between 1 and 50")
		}
	}
	principalID, err := c.resolvePrincipal(ctx)
	if err != nil {
		return err
	}
	now := c.now().UTC()
	effectiveTo := to.UTC()
	partial := effectiveTo.After(now)
	if partial {
		effectiveTo = now
	}
	events, err := c.repository.ListRuntimeEvents(ctx.Request().Context(), entities.SessionRuntimeQuery{
		PrincipalID: principalID, Pool: ctx.QueryParam("pool"), From: from.UTC(), To: effectiveTo,
	})
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "failed to query session runtime")
	}
	coverage, err := c.repository.RuntimeCoverageStart(ctx.Request().Context(), principalID)
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "failed to query session runtime coverage")
	}
	dashboard := aggregateSessionRuntime(events, from.UTC(), to.UTC(), effectiveTo, location, limit)
	dashboard.AsOf = now
	dashboard.IsPartial = partial
	dashboard.Timezone = timezone
	dashboard.CoverageStartedAt = coverage
	return ctx.JSON(http.StatusOK, dashboard)
}

func (c *SessionUsageController) resolvePrincipal(ctx echo.Context) (string, error) {
	authz := auth.GetAuthorizationContext(ctx)
	if authz == nil {
		return "", echo.NewHTTPError(http.StatusUnauthorized, "authentication required")
	}
	teamID := ctx.QueryParam("team_id")
	if teamID == "" {
		return authz.PersonalScope.UserID, nil
	}
	if !authz.CanAccessTeam(teamID) {
		return "", echo.NewHTTPError(http.StatusForbidden, "not authorized for team")
	}
	team, err := c.teams.FindByTeamID(ctx.Request().Context(), teamID)
	if err != nil {
		return "", echo.NewHTTPError(http.StatusInternalServerError, "failed to resolve team")
	}
	if team == nil || team.PrincipalID() == "" {
		return "", echo.NewHTTPError(http.StatusNotFound, "team not found")
	}
	return team.PrincipalID(), nil
}

type runtimeInterval struct {
	sessionID string
	status    string
	start     time.Time
	end       time.Time
}

func aggregateSessionRuntime(events []entities.SessionStatusUsageEvent, from, to, effectiveTo time.Time, location *time.Location, limit int) entities.SessionRuntimeDashboard {
	dashboard := entities.SessionRuntimeDashboard{From: from, To: to, Trend: []entities.SessionRuntimeBucket{}, BySession: []entities.SessionRuntimeBreakdown{}, AvailablePools: []string{}}
	if !from.Before(effectiveTo) {
		return dashboard
	}
	byID := map[string][]entities.SessionStatusUsageEvent{}
	pools := map[string]struct{}{}
	for _, event := range events {
		byID[event.SessionID] = append(byID[event.SessionID], event)
		if event.Pool != "" {
			pools[event.Pool] = struct{}{}
		}
	}
	intervals := []runtimeInterval{}
	breakdowns := map[string]*entities.SessionRuntimeBreakdown{}
	for sessionID, sessionEvents := range byID {
		sort.SliceStable(sessionEvents, func(i, j int) bool { return sessionEvents[i].OccurredAt.Before(sessionEvents[j].OccurredAt) })
		for index, event := range sessionEvents {
			start := event.OccurredAt
			if start.Before(from) {
				start = from
			}
			end := effectiveTo
			if index+1 < len(sessionEvents) && sessionEvents[index+1].OccurredAt.Before(end) {
				end = sessionEvents[index+1].OccurredAt
			}
			if !start.Before(end) {
				continue
			}
			interval := runtimeInterval{sessionID: sessionID, status: strings.ToLower(event.Status), start: start, end: end}
			intervals = append(intervals, interval)
			item := breakdowns[sessionID]
			if item == nil {
				item = &entities.SessionRuntimeBreakdown{SessionID: sessionID}
				breakdowns[sessionID] = item
			}
			addInterval(item, interval.status, secondsBetween(start, end))
		}
		if len(sessionEvents) > 0 {
			item := breakdowns[sessionID]
			if item == nil {
				item = &entities.SessionRuntimeBreakdown{SessionID: sessionID}
				breakdowns[sessionID] = item
			}
			item.CurrentStatus = sessionEvents[len(sessionEvents)-1].Status
		}
	}

	for _, item := range breakdowns {
		dashboard.Summary.RuntimeSeconds += item.RuntimeSeconds
		dashboard.Summary.RunningSeconds += item.RunningSeconds
		dashboard.Summary.SuspendedSeconds += item.SuspendedSeconds
		if item.RuntimeSeconds > 0 {
			dashboard.Summary.Sessions++
		}
		dashboard.BySession = append(dashboard.BySession, *item)
	}
	sort.Slice(dashboard.BySession, func(i, j int) bool {
		if dashboard.BySession[i].RuntimeSeconds == dashboard.BySession[j].RuntimeSeconds {
			return dashboard.BySession[i].SessionID < dashboard.BySession[j].SessionID
		}
		return dashboard.BySession[i].RuntimeSeconds > dashboard.BySession[j].RuntimeSeconds
	})
	if len(dashboard.BySession) > limit {
		dashboard.BySession = dashboard.BySession[:limit]
	}
	dashboard.Summary.PeakConcurrent = peakConcurrent(intervals, from, effectiveTo)
	dashboard.Trend = runtimeTrend(intervals, from, effectiveTo, location)
	for pool := range pools {
		dashboard.AvailablePools = append(dashboard.AvailablePools, pool)
	}
	sort.Strings(dashboard.AvailablePools)
	return dashboard
}

func isRuntimeStatus(status string) bool {
	switch status {
	case "creating", "starting", "active", "stable", "running", "resuming", "restoring", "suspending":
		return true
	default:
		return false
	}
}

func addInterval(item *entities.SessionRuntimeBreakdown, status string, seconds int64) {
	if isRuntimeStatus(status) {
		item.RuntimeSeconds += seconds
	}
	if status == "running" {
		item.RunningSeconds += seconds
	}
	if status == "suspended" {
		item.SuspendedSeconds += seconds
	}
}

func secondsBetween(start, end time.Time) int64 { return int64(end.Sub(start) / time.Second) }

type concurrencyPoint struct {
	at    time.Time
	delta int
}

func peakConcurrent(intervals []runtimeInterval, from, to time.Time) int {
	points := []concurrencyPoint{}
	for _, interval := range intervals {
		if !isRuntimeStatus(interval.status) || !interval.start.Before(to) || !from.Before(interval.end) {
			continue
		}
		start, end := interval.start, interval.end
		if start.Before(from) {
			start = from
		}
		if end.After(to) {
			end = to
		}
		points = append(points, concurrencyPoint{start, 1}, concurrencyPoint{end, -1})
	}
	sort.Slice(points, func(i, j int) bool {
		if points[i].at.Equal(points[j].at) {
			return points[i].delta < points[j].delta
		}
		return points[i].at.Before(points[j].at)
	})
	current, peak := 0, 0
	for _, point := range points {
		current += point.delta
		if current > peak {
			peak = current
		}
	}
	return peak
}

func runtimeTrend(intervals []runtimeInterval, from, to time.Time, location *time.Location) []entities.SessionRuntimeBucket {
	result := []entities.SessionRuntimeBucket{}
	cursor := from.In(location)
	for cursor.Before(to.In(location)) {
		next := time.Date(cursor.Year(), cursor.Month(), cursor.Day()+1, 0, 0, 0, 0, location)
		bucketStart, bucketEnd := cursor.UTC(), next.UTC()
		if bucketEnd.After(to) {
			bucketEnd = to
		}
		bucket := entities.SessionRuntimeBucket{Start: bucketStart}
		for _, interval := range intervals {
			start, end := interval.start, interval.end
			if start.Before(bucketStart) {
				start = bucketStart
			}
			if end.After(bucketEnd) {
				end = bucketEnd
			}
			if !start.Before(end) {
				continue
			}
			seconds := secondsBetween(start, end)
			if isRuntimeStatus(interval.status) {
				bucket.RuntimeSeconds += seconds
			}
			if interval.status == "running" {
				bucket.RunningSeconds += seconds
			}
			if interval.status == "suspended" {
				bucket.SuspendedSeconds += seconds
			}
		}
		bucket.PeakConcurrent = peakConcurrent(intervals, bucketStart, bucketEnd)
		result = append(result, bucket)
		cursor = next
	}
	return result
}
