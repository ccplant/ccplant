//go:build e2e

package api_test

import (
	"context"
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
	SessionID string `json:"session_id"`
	Status    string `json:"status"`
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

	suspendSession(t, c, sessionID, 2*time.Minute)
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

func suspendSession(t *testing.T, c *apitest.Client, sessionID string, timeout time.Duration) {
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
			t.Fatalf("suspend failed: %s", response.Message)
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
	last := "not found"
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
				last = strings.ToLower(session.Status)
				if want[last] {
					return
				}
			}
		}
		if time.Now().Add(2 * time.Second).After(deadline) {
			t.Fatalf("session %s did not reach status %v; last status %q", sessionID, wanted, last)
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
