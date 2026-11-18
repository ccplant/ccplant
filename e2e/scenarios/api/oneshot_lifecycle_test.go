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

// TestOneshotLifecycle owns only the resource-consuming lifecycle contract.
// Health, authentication, search semantics, and generic dry-run behavior are
// separate catalog scenarios and are intentionally not repeated here.
func TestOneshotLifecycle(t *testing.T) {
	if !strings.EqualFold(os.Getenv("E2E_PARAM_LIVE_SESSION"), "true") {
		t.Skip("set E2E_PARAM_LIVE_SESSION=true to run the resource-consuming oneshot scenario")
	}

	profileID := strings.TrimSpace(os.Getenv("E2E_PARAM_SESSION_PROFILE_ID"))
	if profileID == "" {
		t.Fatal("E2E_PARAM_SESSION_PROFILE_ID must select an isolated E2E profile")
	}
	pool := strings.TrimSpace(os.Getenv("E2E_PARAM_POOL"))
	if pool == "" {
		pool = "fly-dev"
	}

	c := client(t)
	tag := apitest.UniqueName()
	expected := "CCPLANT_ONESHOT_E2E_OK"
	var created struct {
		SessionID string `json:"session_id"`
	}
	request(t, c, http.MethodPost, "/start", map[string]any{
		"scope":              "user",
		"session_profile_id": profileID,
		"tags":               map[string]string{"e2e_run": tag},
		"params": map[string]any{
			"pool":        pool,
			"oneshot":     true,
			"session_ttl": "1m",
			"message":     "Return exactly " + expected,
		},
	}, &created, http.StatusOK)
	if created.SessionID == "" {
		t.Fatal("start response must contain session_id")
	}

	cleaned := false
	t.Cleanup(func() {
		if cleaned {
			return
		}
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		if err := c.Do(ctx, http.MethodDelete, "/sessions/"+url.PathEscape(created.SessionID), nil, nil,
			http.StatusOK, http.StatusAccepted, http.StatusNotFound); err != nil {
			t.Errorf("cleanup session %s failed: %v", created.SessionID, err)
		}
	})

	waitForExpectedMessage(t, c, created.SessionID, expected, 2*time.Minute)
	waitForAutomaticDeletion(t, c, tag, created.SessionID, 3*time.Minute)
	cleaned = true
}

func waitForExpectedMessage(t *testing.T, c *apitest.Client, sessionID, expected string, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		var wait json.RawMessage
		_ = c.Do(ctx, http.MethodGet, "/sessions/"+url.PathEscape(sessionID)+"/messages/wait?timeout=10", nil, &wait,
			http.StatusOK, http.StatusNotFound, http.StatusNotImplemented)
		cancel()

		ctx, cancel = context.WithTimeout(context.Background(), 20*time.Second)
		var messages json.RawMessage
		err := c.Do(ctx, http.MethodGet, "/"+url.PathEscape(sessionID)+"/messages", nil, &messages, http.StatusOK)
		cancel()
		if err == nil && strings.Contains(string(messages), expected) {
			return
		}
		if time.Now().Add(2 * time.Second).After(deadline) {
			t.Fatalf("session %s did not return the expected response", sessionID)
		}
		time.Sleep(2 * time.Second)
	}
}

func waitForAutomaticDeletion(t *testing.T, c *apitest.Client, tag, sessionID string, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
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
		found := false
		for _, session := range out.Sessions {
			found = found || session.SessionID == sessionID
		}
		if !found {
			return
		}
		if time.Now().Add(5 * time.Second).After(deadline) {
			t.Fatalf("oneshot session %s was not automatically deleted", sessionID)
		}
		time.Sleep(5 * time.Second)
	}
}
