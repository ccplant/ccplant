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

	route := func(status string, last time.Time, tags map[string]string) *repositories.SessionRoute {
		return &repositories.SessionRoute{SessionID: "s", ManagerID: "m", RemoteSessionID: "r", Status: status, StatusUpdatedAt: last, Tags: tags}
	}

	cases := []struct {
		name  string
		route *repositories.SessionRoute
		facts routeReconcileFacts
		want  string
	}{
		{"local route is never reconciled", &repositories.SessionRoute{SessionID: "s", RemoteSessionID: "r", StatusUpdatedAt: old}, routeReconcileFacts{managerMissing: true}, ""},
		{"templated route is retained", route("templated", old, nil), routeReconcileFacts{managerMissing: true}, ""},
		{"recent change is protected by grace", route("running", now.Add(-time.Minute), nil), routeReconcileFacts{managerMissing: true}, ""},
		{"deleted manager", route("running", old, nil), routeReconcileFacts{managerMissing: true}, RouteReconcileManagerDeleted},
		{"deleted manager also ends suspended session", route("suspended", old, nil), routeReconcileFacts{managerMissing: true}, RouteReconcileManagerDeleted},
		{"deleted runner", route("running", old, nil), routeReconcileFacts{runnerMissing: true}, RouteReconcileRunnerDeleted},
		{"deleted runner of idle session", route("active", old, nil), routeReconcileFacts{runnerMissing: true}, RouteReconcileRunnerDeleted},
		{"suspended session keeps released runner", route("suspended", old, nil), routeReconcileFacts{runnerMissing: true}, ""},
		{"resuming session keeps released runner", route("resuming", old, nil), routeReconcileFacts{runnerMissing: true}, ""},
		{"stuck deletion", route("terminating", old, nil), alive, RouteReconcileStuckDeletion},
		{"pending deletion is left to the manager", route("terminating", now.Add(-30*time.Minute), nil), alive, ""},
		{"failed deletion is retried until stuck", route("delete_failed", now.Add(-30*time.Minute), nil), alive, ""},
		{"ttl session frozen past ceiling", route("running", now.Add(-25*time.Hour), map[string]string{"session_ttl": "1m"}), alive, RouteReconcileTTLExceeded},
		{"ttl session within ceiling", route("running", now.Add(-30*time.Minute), map[string]string{"session_ttl": "1m"}), alive, ""},
		{"ttl session reclaimed within the hour", route("running", now.Add(-50*time.Minute), map[string]string{"session_ttl": "1m"}), alive, RouteReconcileTTLExceeded},
		{"suspended ttl session is left to TTL cleanup", route("suspended", now.Add(-48*time.Hour), map[string]string{"session_ttl": "1m"}), alive, ""},
		{"invalid ttl is ignored", route("running", now.Add(-48*time.Hour), map[string]string{"session_ttl": "forever"}), alive, ""},
		{"healthy long-running session is kept", route("running", now.Add(-72*time.Hour), nil), alive, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := classifyRouteForReconcile(tc.route, tc.facts, now, opts); got != tc.want {
				t.Fatalf("classifyRouteForReconcile() = %q, want %q", got, tc.want)
			}
		})
	}
}
