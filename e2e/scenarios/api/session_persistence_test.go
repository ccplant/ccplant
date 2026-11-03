//go:build e2e

package api_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ccplant/ccplant/e2e/pkg/apitest"
)

func TestSessionPersistenceSuspendResume(t *testing.T) {
	if !strings.EqualFold(os.Getenv("E2E_PARAM_LIVE_SESSION"), "true") {
		t.Skip("set E2E_PARAM_LIVE_SESSION=true to run the resource-consuming persistence scenario")
	}
	c := client(t)
	agentType := strings.TrimSpace(os.Getenv("E2E_PARAM_AGENT_TYPE"))
	if agentType == "" {
		agentType = "claude-acp"
	}
	marker := "ccplant-persistence-e2e-" + apitest.UniqueName()
	start := map[string]any{
		"scope": "user",
		"tags":  map[string]string{"e2e_run": marker},
		"params": map[string]any{
			"agent_type": agentType,
			"message":    "Reply with exactly this token and nothing else: " + marker,
		},
	}
	if connectionID := strings.TrimSpace(os.Getenv("E2E_PARAM_CONNECTION_ID")); connectionID != "" {
		start["params"].(map[string]any)["connection_id"] = connectionID
	}
	var created struct {
		SessionID string `json:"session_id"`
	}
	request(t, c, http.MethodPost, "/start", start, &created, http.StatusOK, http.StatusCreated, http.StatusAccepted)
	if created.SessionID == "" {
		t.Fatal("start response did not contain session_id")
	}
	sessionPath := "/sessions/" + url.PathEscape(created.SessionID)
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		if err := c.Do(ctx, http.MethodDelete, sessionPath, nil, nil, http.StatusOK, http.StatusNoContent, http.StatusNotFound); err != nil {
			t.Errorf("delete persistence test session: %v", err)
		}
	})

	waitForConversationMarker(t, c, created.SessionID, marker, 4*time.Minute)
	if err := suspendConcurrently(c, sessionPath); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	err := c.Do(ctx, http.MethodPost, sessionPath+"/resume", nil, nil, http.StatusOK, http.StatusAccepted)
	cancel()
	if err != nil {
		t.Fatalf("resume session: %v", err)
	}
	waitForConversationMarker(t, c, created.SessionID, marker, 3*time.Minute)
}

func suspendConcurrently(c *apitest.Client, sessionPath string) error {
	start := make(chan struct{})
	errs := make(chan error, 2)
	var wg sync.WaitGroup
	for range 2 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
			defer cancel()
			errs <- c.Do(ctx, http.MethodPost, sessionPath+"/suspend", nil, nil, http.StatusOK)
		}()
	}
	close(start)
	wg.Wait()
	close(errs)
	var joined error
	for err := range errs {
		joined = errors.Join(joined, err)
	}
	if joined != nil {
		return fmt.Errorf("concurrent suspend failed: %w", joined)
	}
	return nil
}

func waitForConversationMarker(t *testing.T, c *apitest.Client, sessionID, marker string, timeout time.Duration) []byte {
	t.Helper()
	deadline := time.Now().Add(timeout)
	path := "/" + url.PathEscape(sessionID) + "/messages"
	var lastErr error
	for time.Now().Before(deadline) {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		var body json.RawMessage
		err := c.Do(ctx, http.MethodGet, path, nil, &body, http.StatusOK)
		cancel()
		if err == nil && strings.Contains(string(body), marker) {
			return append([]byte(nil), body...)
		}
		lastErr = err
		time.Sleep(2 * time.Second)
	}
	if lastErr != nil {
		t.Fatalf("conversation marker %q was not restored: last request: %v", marker, lastErr)
	}
	t.Fatalf("conversation marker %q was not restored before timeout", marker)
	return nil
}
