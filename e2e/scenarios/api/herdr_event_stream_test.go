//go:build e2e

package api_test

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/ccplant/ccplant/e2e/pkg/apitest"
)

// TestHerdrEventStream starts a real ACP agent, opens the Herdr event stream,
// and asks the agent to create a terminal. The terminal must arrive through
// SSE without periodically polling terminal.list.
func TestHerdrEventStream(t *testing.T) {
	if !strings.EqualFold(os.Getenv("E2E_PARAM_LIVE_SESSION"), "true") {
		t.Skip("set E2E_PARAM_LIVE_SESSION=true to run the resource-consuming Herdr stream scenario")
	}

	pool := envOrDefault("E2E_PARAM_POOL", "fly-dev")
	agentType := envOrDefault("E2E_PARAM_AGENT_TYPE", "codex-acp")
	// hc terminal names are limited to 32 characters. UniqueName already adds
	// an e2e prefix, so keep this scenario prefix short enough for the command
	// to be accepted by herdr-core.
	marker := "herdr-" + apitest.UniqueName()
	prompt := "Use the shell to run exactly `hc run --bg --name " + marker + " sleep 120`. " +
		"After the command succeeds, reply with exactly HERDR_STREAM_E2E_OK."
	c := client(t)
	params := map[string]any{
		"pool":        pool,
		"agent_type":  agentType,
		"session_ttl": "10m",
		"message":     prompt,
		"docker":      map[string]any{"enabled": false},
	}
	if managerID := os.Getenv("E2E_PARAM_MANAGER_ID"); managerID != "" {
		params["manager_id"] = managerID
	}

	var created struct {
		SessionID string `json:"session_id"`
	}
	request(t, c, http.MethodPost, "/start", map[string]any{
		"scope":  "user",
		"tags":   map[string]string{"e2e_run": marker},
		"params": params,
	}, &created, http.StatusOK, http.StatusCreated, http.StatusAccepted)
	if created.SessionID == "" {
		t.Fatal("start response did not contain session_id")
	}
	sessionPath := "/sessions/" + url.PathEscape(created.SessionID)
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		if err := c.Do(ctx, http.MethodDelete, sessionPath, nil, nil,
			http.StatusOK, http.StatusAccepted, http.StatusNoContent, http.StatusNotFound); err != nil {
			t.Errorf("delete Herdr stream test session: %v", err)
		}
	})

	streamCtx, cancelStream := context.WithTimeout(context.Background(), 6*time.Minute)
	defer cancelStream()
	if err := waitForHerdrTerminal(streamCtx, c.BaseURL, created.SessionID, marker); err != nil {
		t.Fatal(err)
	}
}

func openHerdrEventStream(ctx context.Context, baseURL, sessionID string) (*http.Response, error) {
	endpoint := strings.TrimRight(baseURL, "/") + "/" + url.PathEscape(sessionID) + "/herdr/events"
	deadline := time.Now().Add(4 * time.Minute)
	for {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
		if err != nil {
			return nil, fmt.Errorf("build Herdr stream request")
		}
		req.Header.Set("Accept", "text/event-stream")
		switch os.Getenv("E2E_AUTH_TYPE") {
		case "bearer":
			req.Header.Set("Authorization", "Bearer "+os.Getenv("E2E_AUTH_VALUE"))
		case "api-key":
			req.Header.Set("X-API-Key", os.Getenv("E2E_AUTH_VALUE"))
		}
		res, err := (&http.Client{}).Do(req)
		if err == nil && res.StatusCode == http.StatusOK && strings.HasPrefix(res.Header.Get("Content-Type"), "text/event-stream") {
			return res, nil
		}
		if res != nil {
			_ = res.Body.Close()
		}
		if time.Now().Add(2 * time.Second).After(deadline) {
			return nil, fmt.Errorf("Herdr event stream did not become ready")
		}
		select {
		case <-ctx.Done():
			return nil, fmt.Errorf("Herdr event stream context ended before readiness")
		case <-time.After(2 * time.Second):
		}
	}
}

func waitForHerdrTerminal(ctx context.Context, baseURL, sessionID, marker string) error {
	for ctx.Err() == nil {
		stream, err := openHerdrEventStream(ctx, baseURL, sessionID)
		if err != nil {
			return err
		}
		scanner := bufio.NewScanner(stream.Body)
		scanner.Buffer(make([]byte, 64*1024), 8<<20)
		for scanner.Scan() {
			line := scanner.Text()
			if !strings.HasPrefix(line, "data: ") {
				continue
			}
			payload := strings.TrimPrefix(line, "data: ")
			if json.Valid([]byte(payload)) && strings.Contains(payload, marker) {
				_ = stream.Body.Close()
				return nil
			}
		}
		_ = stream.Body.Close()
		if err := scanner.Err(); err != nil && ctx.Err() == nil {
			return fmt.Errorf("read Herdr event stream: %w", err)
		}
		select {
		case <-ctx.Done():
			break
		case <-time.After(time.Second):
		}
	}
	return fmt.Errorf("Herdr terminal %q did not appear before the stream deadline", marker)
}
