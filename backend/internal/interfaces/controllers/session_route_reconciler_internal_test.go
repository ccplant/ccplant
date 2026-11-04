package controllers

import (
	"testing"
	"time"

	"github.com/takutakahashi/agentapi-proxy/internal/usecases/ports/repositories"
)

func TestClassifyRouteForReconcile(t *testing.T) {
	now := time.Date(2026, 11, 4, 12, 0, 0, 0, time.UTC)
	opts := SessionRouteReconcileOptions{}.withDefaults()
	old := now.Add(-2 * time.Hour)
	alive := routeReconcileFacts{}
	ttl := map[string]string{"session_ttl": "1m"}

	route := func(status string, last time.Time, tags map[string]string) *repositories.SessionRoute {
		return &repositories.SessionRoute{SessionID: "s", ManagerID: "m", RemoteSessionID: "r", Status: status, StatusUpdatedAt: last, Tags: tags}
	}

	cases := []struct {
		name      string
		route     *repositories.SessionRoute
		facts     routeReconcileFacts
		want      string
		wantProbe bool
	}{
		{"local route is never reconciled", &repositories.SessionRoute{SessionID: "s", RemoteSessionID: "r", StatusUpdatedAt: old}, routeReconcileFacts{managerMissing: true}, "", false},
		{"templated route is retained", route("templated", old, nil), routeReconcileFacts{managerMissing: true}, "", false},
		{"recent change is protected by grace", route("running", now.Add(-time.Minute), ttl), routeReconcileFacts{managerMissing: true}, "", false},
		{"deleted manager", route("running", old, nil), routeReconcileFacts{managerMissing: true}, RouteReconcileManagerDeleted, false},
		{"deleted manager also ends suspended session", route("suspended", old, nil), routeReconcileFacts{managerMissing: true}, RouteReconcileManagerDeleted, false},
		{"deleted runner", route("running", old, nil), routeReconcileFacts{runnerMissing: true}, RouteReconcileRunnerDeleted, false},
		{"deleted runner of idle session", route("active", old, nil), routeReconcileFacts{runnerMissing: true}, RouteReconcileRunnerDeleted, false},
		{"suspended session keeps released runner", route("suspended", old, nil), routeReconcileFacts{runnerMissing: true}, "", false},
		{"resuming session keeps released runner", route("resuming", old, nil), routeReconcileFacts{runnerMissing: true}, "", false},
		{"stuck deletion", route("terminating", old, nil), alive, RouteReconcileStuckDeletion, false},
		{"pending deletion is left to the manager", route("terminating", now.Add(-30*time.Minute), nil), alive, "", false},
		{"failed deletion is retried until stuck", route("delete_failed", now.Add(-30*time.Minute), nil), alive, "", false},
		// Frozen-looking routes are never ended by age: their runtime is asked.
		{"running ttl session is probed, not ended", route("running", now.Add(-48*time.Hour), ttl), alive, "", true},
		{"running interactive session is probed", route("running", old, nil), alive, "", true},
		{"starting session is probed", route("starting", old, ttl), alive, "", true},
		{"idle session is left to TTL cleanup", route("active", old, ttl), alive, "", false},
		{"suspended ttl session is left to TTL cleanup", route("suspended", now.Add(-48*time.Hour), ttl), alive, "", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, probe := classifyRouteForReconcile(tc.route, tc.facts, now, opts)
			if got != tc.want || probe != tc.wantProbe {
				t.Fatalf("classifyRouteForReconcile() = (%q, %t), want (%q, %t)", got, probe, tc.want, tc.wantProbe)
			}
		})
	}
}
