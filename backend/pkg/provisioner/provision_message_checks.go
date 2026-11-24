package provisioner

import (
	"context"
	"encoding/json"
	"fmt"
	closeutils "github.com/takutakahashi/agentapi-proxy/pkg/utils"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

func localAgentHTTPClient(agentapiURL string) *http.Client {
	if agentapiURL == "http://unix" {
		return unixSocketHTTPClient(localAgentSocketPath())
	}
	return &http.Client{Timeout: 5 * time.Second}
}

const initialMessageMarkerPrefix = "initial-message-sent-"

func initialMessageMarkerPath(home, sessionID string) string {
	if sessionID == "" || sessionID == "." || sessionID == ".." || strings.ContainsAny(sessionID, `/\`) {
		return ""
	}
	return filepath.Join(home, ".session", initialMessageMarkerPrefix+sessionID)
}

func acpInitialMessageDelivered(client *http.Client, agentapiURL, message string, restored bool, markerPath string) bool {
	if initialMessageMarkerExists(markerPath) {
		log.Printf("[PROVISIONER] Initial message marker exists (%s), skipping initial message", markerPath)
		return true
	}
	if !restored {
		if count := countACPUserMessages(client, agentapiURL); count > 0 {
			log.Printf("[PROVISIONER] ACP user messages already exist (%d), skipping initial message", count)
			return true
		}
		return false
	}
	if anyInitialMessageMarkerExists(markerPath) {
		return false
	}
	if hasACPUserMessage(client, agentapiURL, message) {
		log.Printf("[PROVISIONER] Restored ACP history already contains the initial message, skipping")
		writeInitialMessageMarker(markerPath)
		return true
	}
	return false
}

func initialMessageMarkerExists(markerPath string) bool {
	if markerPath == "" {
		return false
	}
	_, err := os.Stat(markerPath)
	return err == nil
}

func anyInitialMessageMarkerExists(markerPath string) bool {
	if markerPath == "" {
		return false
	}
	matches, err := filepath.Glob(filepath.Join(filepath.Dir(markerPath), initialMessageMarkerPrefix+"*"))
	return err == nil && len(matches) > 0
}

func writeInitialMessageMarker(markerPath string) {
	if markerPath == "" {
		return
	}
	if err := os.MkdirAll(filepath.Dir(markerPath), 0o700); err != nil {
		log.Printf("[PROVISIONER] WARNING: failed to create initial message marker directory: %v", err)
		return
	}
	if err := os.WriteFile(markerPath, []byte(time.Now().UTC().Format(time.RFC3339)+"\n"), 0o600); err != nil {
		log.Printf("[PROVISIONER] WARNING: failed to write initial message marker: %v", err)
	}
}

// getACPSessionId fetches the ACP session ID from GET /session.
func getACPSessionId(client *http.Client, agentapiURL string) (string, error) {
	resp, err := client.Get(agentapiURL + "/session")
	if err != nil {
		return "", err
	}
	defer func() { closeutils.SafeCloseResponseBody(resp.Body) }()
	var sr acpSessionResponse
	if err := json.NewDecoder(resp.Body).Decode(&sr); err != nil {
		return "", err
	}
	if sr.SessionId == "" {
		return "", fmt.Errorf("empty sessionId in /session response")
	}
	return sr.SessionId, nil
}

// countACPUserMessages returns the number of user_message_chunk events in the
// ACP bridge's GET /messages history. Used for idempotency on Pod restarts.
func countACPUserMessages(client *http.Client, agentapiURL string) int {
	resp, err := client.Get(agentapiURL + "/messages")
	if err != nil {
		return 0
	}
	defer func() { closeutils.SafeCloseResponseBody(resp.Body) }()

	var mr acpMessagesResponse
	if err := json.NewDecoder(resp.Body).Decode(&mr); err != nil {
		return 0
	}

	// Each element is a raw JSON-RPC 2.0 message. We look for:
	//   { "method": "session/update", "params": { "update": { "sessionUpdate": "user_message_chunk" } } }
	type acpUpdateParams struct {
		Update struct {
			SessionUpdate string `json:"sessionUpdate"`
		} `json:"update"`
	}
	type acpMsg struct {
		Method string          `json:"method"`
		Params acpUpdateParams `json:"params"`
	}
	count := 0
	for _, raw := range mr.Messages {
		var m acpMsg
		if err := json.Unmarshal(raw, &m); err != nil {
			continue
		}
		if m.Method == "session/update" && m.Params.Update.SessionUpdate == "user_message_chunk" {
			count++
		}
	}
	return count
}

// hasACPUserMessage checks exact restored prompt content. A restored session
// legitimately has older user messages, so message count alone cannot decide
// whether the new launch prompt has already been delivered.
func hasACPUserMessage(client *http.Client, agentapiURL, message string) bool {
	latest, ok := fetchACPTurn(client, agentapiURL+"/messages")
	if !ok {
		return false
	}
	if acpTurnContainsUserMessage(latest.Messages, message) {
		return true
	}
	for i := 0; i < latest.UserPromptCount; i++ {
		if latest.UserPromptIndex != nil && *latest.UserPromptIndex == i {
			continue
		}
		turn, ok := fetchACPTurn(client, fmt.Sprintf("%s/messages?userPromptIndex=%d", agentapiURL, i))
		if ok && acpTurnContainsUserMessage(turn.Messages, message) {
			return true
		}
	}
	return false
}

type acpTurnResponse struct {
	Messages        []json.RawMessage `json:"messages"`
	UserPromptCount int               `json:"userPromptCount"`
	UserPromptIndex *int              `json:"userPromptIndex"`
}

func fetchACPTurn(client *http.Client, url string) (acpTurnResponse, bool) {
	var tr acpTurnResponse
	resp, err := client.Get(url)
	if err != nil {
		return tr, false
	}
	defer func() { closeutils.SafeCloseResponseBody(resp.Body) }()
	if resp.StatusCode != http.StatusOK {
		return tr, false
	}
	if json.NewDecoder(resp.Body).Decode(&tr) != nil {
		return tr, false
	}
	return tr, true
}

func acpTurnContainsUserMessage(messages []json.RawMessage, message string) bool {
	for _, raw := range messages {
		var event struct {
			Method string `json:"method"`
			Params struct {
				Update struct {
					SessionUpdate string `json:"sessionUpdate"`
					Content       struct {
						Text string `json:"text"`
					} `json:"content"`
				} `json:"update"`
			} `json:"params"`
		}
		if json.Unmarshal(raw, &event) == nil && event.Method == "session/update" && event.Params.Update.SessionUpdate == "user_message_chunk" && event.Params.Update.Content.Text == message {
			return true
		}
	}
	return false
}

// waitForDefaultAgentReady uses the two-phase strategy from the original shell
// script: first tries to catch "running→stable" within 10 s, then falls back
// to "stable + non-empty message" for up to 60 s.
func waitForDefaultAgentReady(ctx context.Context, client *http.Client, agentapiURL string) {
	log.Printf("[PROVISIONER] Waiting for agent ready (phase 1: running→stable, 10s timeout)")

	sawRunning := false
	for i := 0; i < 10; i++ {
		select {
		case <-ctx.Done():
			return
		case <-time.After(time.Second):
		}

		st := getAgentStatus(client, agentapiURL)
		if st == "running" {
			sawRunning = true
		}
		if sawRunning && st == "stable" {
			if countNonEmptyMessages(client, agentapiURL) > 0 {
				log.Printf("[PROVISIONER] Phase 1: running→stable transition detected")
				return
			}
		}
	}

	// Phase 2 fallback.
	log.Printf("[PROVISIONER] Phase 1 timed out (sawRunning=%v), falling back to stable+non-empty check", sawRunning)
	for i := 0; i < 60; i++ {
		select {
		case <-ctx.Done():
			return
		case <-time.After(time.Second):
		}

		if getAgentStatus(client, agentapiURL) == "stable" && countNonEmptyMessages(client, agentapiURL) > 0 {
			log.Printf("[PROVISIONER] Phase 2: agent stable with non-empty messages")
			return
		}
	}
	log.Printf("[PROVISIONER] WARNING: agent not fully ready after 70s, sending anyway")
}

// waitForStable polls until agentapi reports "stable" or the context is done.
func waitForStable(ctx context.Context, client *http.Client, agentapiURL string, maxRetries int) {
	for i := 0; i < maxRetries; i++ {
		select {
		case <-ctx.Done():
			return
		case <-time.After(time.Second):
		}
		if getAgentStatus(client, agentapiURL) == "stable" {
			return
		}
	}
	log.Printf("[PROVISIONER] WARNING: agent not stable after %d retries", maxRetries)
}

// getAgentStatus fetches /status and returns the "status" field value.
func getAgentStatus(client *http.Client, agentapiURL string) string {
	resp, err := client.Get(agentapiURL + "/status")
	if err != nil {
		return ""
	}
	defer func() { closeutils.SafeCloseResponseBody(resp.Body) }()

	var sr agentStatusResponse
	if err := json.NewDecoder(resp.Body).Decode(&sr); err != nil {
		return ""
	}
	return sr.Status
}

// countUserMessages returns the number of messages with role "user".
func countUserMessages(client *http.Client, agentapiURL string) int {
	resp, err := client.Get(agentapiURL + "/messages")
	if err != nil {
		return 0
	}
	defer func() { closeutils.SafeCloseResponseBody(resp.Body) }()

	var mr agentMessagesResponse
	if err := json.NewDecoder(resp.Body).Decode(&mr); err != nil {
		return 0
	}
	count := 0
	for _, m := range mr.Messages {
		if m.Role == "user" {
			count++
		}
	}
	return count
}

// countNonEmptyMessages returns the number of messages with non-empty content.
func countNonEmptyMessages(client *http.Client, agentapiURL string) int {
	resp, err := client.Get(agentapiURL + "/messages")
	if err != nil {
		return 0
	}
	defer func() { closeutils.SafeCloseResponseBody(resp.Body) }()

	var mr agentMessagesResponse
	if err := json.NewDecoder(resp.Body).Decode(&mr); err != nil {
		return 0
	}
	count := 0
	for _, m := range mr.Messages {
		if m.Content != "" {
			count++
		}
	}
	return count
}

// writeWebhookPayloadFile writes the webhook payload JSON string to
// webhookPayloadPath.  It is a no-op when the file already exists, which is
// the case for non-stock sessions where a read-only Kubernetes Secret volume
// provides the file.  For stock sessions the volume is absent, so the
// provisioner writes the file itself to ensure the agent always finds the
// payload at the same well-known path.
