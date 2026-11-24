//go:build e2e

package api_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/ccplant/ccplant/e2e/pkg/apitest"
)

type triggerResult struct {
	SessionID     string `json:"session_id"`
	SessionReused bool   `json:"session_reused"`
}

type searchSession struct {
	SessionID          string `json:"session_id"`
	AllocatedSessionID string `json:"allocated_session_id"`
	ManagerID          string `json:"manager_id"`
	RemoteSessionID    string `json:"remote_session_id"`
	Status             string `json:"status"`
	ErrorCode          string `json:"error_code"`
	ErrorMessage       string `json:"error_message"`
}

func TestScheduleSuspendResume(t *testing.T) {
	if !strings.EqualFold(os.Getenv("E2E_PARAM_LIVE_SESSION"), "true") {
		t.Skip("set E2E_PARAM_LIVE_SESSION=true to run the resource-consuming live session scenario")
	}

	c := client(t)
	name := apitest.UniqueName()
	profileID := strings.TrimSpace(os.Getenv("E2E_PARAM_SESSION_PROFILE_ID"))
	sessionConfig := map[string]any{
		"reuse_session": true,
		"reuse_message": "E2E resume probe; no response is required.",
		"tags":          map[string]string{"e2e_run": name},
	}
	if profileID != "" {
		sessionConfig["session_profile_id"] = profileID
	}

	var schedule resource
	request(t, c, http.MethodPost, "/schedules", map[string]any{
		"name":           name,
		"scope":          "user",
		"status":         "paused",
		"scheduled_at":   "2099-01-01T00:00:00Z",
		"timezone":       "UTC",
		"session_config": sessionConfig,
	}, &schedule, http.StatusCreated)
	if schedule.ID == "" {
		t.Fatal("create schedule response must contain id")
	}

	schedulePath := "/schedules/" + url.PathEscape(schedule.ID)
	var sessionID string
	t.Cleanup(func() { cleanupLiveResources(t, c, sessionID, schedulePath) })

	first := triggerSchedule(t, c, schedulePath)
	if first.SessionID == "" || first.SessionReused {
		t.Fatalf("first trigger must create a session: session_id=%q reused=%t", first.SessionID, first.SessionReused)
	}
	sessionID = first.SessionID
	waitForSessionStatus(t, c, name, sessionID, 4*time.Minute, "active", "running", "stable")
	waitForRunnerAssignment(t, c, name, sessionID, 2*time.Minute)
	waitForSessionMessagesEndpoint(t, c, sessionID, 2*time.Minute)
	t.Logf("public session route before suspend: %s", sessionRouteInventory(t, c, name, sessionID))
	t.Logf("session-pool inventory before suspend: %s", managerInventory(t, c))
	suspendSession(t, c, name, sessionID, 2*time.Minute)
	waitForSessionStatus(t, c, name, sessionID, 2*time.Minute, "suspended")

	second := triggerSchedule(t, c, schedulePath)
	if second.SessionID != sessionID || !second.SessionReused {
		t.Fatalf("second trigger must reuse session %q: session_id=%q reused=%t", sessionID, second.SessionID, second.SessionReused)
	}
	waitForSessionStatus(t, c, name, sessionID, 3*time.Minute, "active", "running", "stable")
}

func triggerSchedule(t *testing.T, c *apitest.Client, schedulePath string) triggerResult {
	t.Helper()
	var out triggerResult
	request(t, c, http.MethodPost, schedulePath+"/trigger", nil, &out, http.StatusOK)
	return out
}

func managerInventory(t *testing.T, c *apitest.Client) string {
	t.Helper()
	var out struct {
		SessionManagers []struct {
			Manager struct {
				ID   string `json:"id"`
				Name string `json:"name"`
			} `json:"manager"`
			Status struct {
				Namespace        string   `json:"namespace"`
				ServingInstance  string   `json:"serving_instance"`
				RunningRunnerIDs []string `json:"running_runner_ids"`
				UsedRunnerIDs    []string `json:"used_runner_ids"`
			} `json:"status"`
		} `json:"session_managers"`
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if err := c.Do(ctx, http.MethodGet, "/session-pools/status", nil, &out, http.StatusOK); err != nil {
		return "unavailable: " + err.Error()
	}
	raw, _ := json.Marshal(out.SessionManagers)
	return string(raw)
}

func waitForRunnerAssignment(t *testing.T, c *apitest.Client, tag, sessionID string, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	last := "public route or manager inventory unavailable"
	for time.Now().Before(deadline) {
		var search struct {
			Sessions []searchSession `json:"sessions"`
		}
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		err := c.Do(ctx, http.MethodGet, "/search?scope=user&tag.e2e_run="+url.QueryEscape(tag), nil, &search, http.StatusOK)
		cancel()
		if err == nil {
			for _, route := range search.Sessions {
				if route.SessionID != sessionID {
					continue
				}
				// Older API versions do not expose route diagnostics. Prove the
				// session is reachable through its public route before suspending;
				// when diagnostics exist, retain the stricter inventory check.
				if route.ManagerID == "" && route.RemoteSessionID == "" {
					t.Log("route diagnostics unavailable; checking public messages endpoint readiness")
					waitForSessionMessagesEndpoint(t, c, sessionID, time.Until(deadline))
					return
				}
				var inventory struct {
					SessionManagers []struct {
						Manager struct {
							ID string `json:"id"`
						} `json:"manager"`
						Status struct {
							Namespace        string   `json:"namespace"`
							ServingInstance  string   `json:"serving_instance"`
							RunningRunnerIDs []string `json:"running_runner_ids"`
							UsedRunnerIDs    []string `json:"used_runner_ids"`
						} `json:"status"`
					} `json:"session_managers"`
				}
				ctx, cancel = context.WithTimeout(context.Background(), 15*time.Second)
				err = c.Do(ctx, http.MethodGet, "/session-pools/status", nil, &inventory, http.StatusOK)
				cancel()
				if err != nil {
					last = "manager inventory unavailable: " + err.Error()
					break
				}
				last = "runner " + route.RemoteSessionID + " is not yet allocated to manager " + route.ManagerID
				for _, manager := range inventory.SessionManagers {
					if manager.Manager.ID != route.ManagerID {
						continue
					}
					used := false
					running := false
					for _, id := range manager.Status.UsedRunnerIDs {
						used = used || id == route.RemoteSessionID
					}
					for _, id := range manager.Status.RunningRunnerIDs {
						running = running || id == route.RemoteSessionID
					}
					if used && running {
						return
					}
				}
				routeJSON, _ := json.Marshal(route)
				inventoryJSON, _ := json.Marshal(inventory.SessionManagers)
				last += "; route=" + string(routeJSON) + "; manager inventory=" + string(inventoryJSON)
			}
		}
		time.Sleep(2 * time.Second)
	}
	t.Fatalf("session %s runner was not assigned before suspend: %s", sessionID, last)
}

func waitForSessionMessagesEndpoint(t *testing.T, c *apitest.Client, sessionID string, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	path := "/" + url.PathEscape(sessionID) + "/messages"
	var lastErr error
	for time.Now().Before(deadline) {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		var body json.RawMessage
		lastErr = c.Do(ctx, http.MethodGet, path, nil, &body, http.StatusOK)
		cancel()
		if lastErr == nil {
			return
		}
		time.Sleep(2 * time.Second)
	}
	t.Fatalf("session %s messages endpoint did not become ready: %v", sessionID, lastErr)
}

func sessionRouteInventory(t *testing.T, c *apitest.Client, tag, sessionID string) string {
	t.Helper()
	var out struct {
		Sessions []searchSession `json:"sessions"`
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	path := "/search?scope=user&tag.e2e_run=" + url.QueryEscape(tag)
	if err := c.Do(ctx, http.MethodGet, path, nil, &out, http.StatusOK); err != nil {
		return "unavailable: " + err.Error()
	}
	for _, session := range out.Sessions {
		if session.SessionID == sessionID {
			raw, _ := json.Marshal(session)
			return string(raw)
		}
	}
	return "not found"
}

func suspendSession(t *testing.T, c *apitest.Client, tag, sessionID string, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	path := "/sessions/" + url.PathEscape(sessionID) + "/suspend"
	for {
		var response struct {
			Message string `json:"message"`
		}
		ctx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
		err := c.Do(ctx, http.MethodPost, path, nil, &response, http.StatusOK, http.StatusServiceUnavailable)
		cancel()
		if err != nil {
			t.Fatal(err)
		}
		if response.Message == "" {
			return
		}
		if !strings.Contains(strings.ToLower(response.Message), "session is busy") {
			t.Fatalf("suspend session %s failed: %s; public session route: %s; manager inventory: %s", sessionID, response.Message, sessionRouteInventory(t, c, tag, sessionID), managerInventory(t, c))
		}
		if time.Now().Add(2 * time.Second).After(deadline) {
			t.Fatal("session remained busy while waiting to suspend")
		}
		time.Sleep(2 * time.Second)
	}
}

func waitForSessionStatus(t *testing.T, c *apitest.Client, tag, sessionID string, timeout time.Duration, wanted ...string) {
	t.Helper()
	want := make(map[string]bool, len(wanted))
	for _, status := range wanted {
		want[status] = true
	}
	deadline := time.Now().Add(timeout)
	last := searchSession{Status: "not found"}
	for {
		var out struct {
			Sessions []searchSession `json:"sessions"`
		}
		ctx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
		err := c.Do(ctx, http.MethodGet, "/search?scope=user&tag.e2e_run="+url.QueryEscape(tag), nil, &out, http.StatusOK)
		cancel()
		if err != nil {
			t.Fatal(err)
		}
		for _, session := range out.Sessions {
			if session.SessionID == sessionID {
				session.Status = strings.ToLower(session.Status)
				last = session
				if want[session.Status] {
					return
				}
			}
		}
		if time.Now().Add(2 * time.Second).After(deadline) {
			t.Fatalf("session %s did not reach status %v; last session state: %+v", sessionID, wanted, last)
		}
		time.Sleep(2 * time.Second)
	}
}

func cleanupLiveResources(t *testing.T, c *apitest.Client, sessionID, schedulePath string) {
	t.Helper()
	if sessionID != "" {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		err := c.Do(ctx, http.MethodDelete, "/sessions/"+url.PathEscape(sessionID), nil, nil, http.StatusOK, http.StatusAccepted, http.StatusNotFound)
		cancel()
		if err != nil {
			t.Errorf("cleanup session %s failed: %v", sessionID, err)
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	err := c.Do(ctx, http.MethodDelete, schedulePath, nil, nil, http.StatusOK, http.StatusNoContent, http.StatusNotFound)
	cancel()
	if err != nil {
		t.Errorf("cleanup schedule failed: %v", err)
	}
}
