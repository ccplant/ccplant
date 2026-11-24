package provisioner

import (
	"bytes"
	"context"
	"encoding/json"
	"log"
	"net/http"
	"time"

	closeutils "github.com/takutakahashi/agentapi-proxy/pkg/utils"
)

type agentStatusResponse struct {
	Status string `json:"status"`
}

// agentMessagesResponse is the minimal shape of agentapi's /messages response.
type agentMessagesResponse struct {
	Messages []struct {
		Role    string `json:"role"`
		Content string `json:"content"`
	} `json:"messages"`
}

// acpSessionResponse is the shape of the ACP bridge's GET /session response.
type acpSessionResponse struct {
	SessionId string `json:"sessionId"`
	Status    string `json:"status"`
}

// acpMessagesResponse is the shape of the ACP bridge's GET /messages response.
// Each message is a raw JSON-RPC 2.0 envelope; we only need to inspect
// the method and the sessionUpdate discriminant inside params.update.
type acpMessagesResponse struct {
	Messages []json.RawMessage `json:"messages"`
}

type initialMessageRequest struct {
	agentapiURL, message, agentType, markerPath string
	waitSec                                     int
	restored                                    bool
}

// sendInitialMessage sends an initial message to agentapi after it has
// started, replicating the logic of initialMessageSenderScript.
func sendInitialMessage(ctx context.Context, request initialMessageRequest) {
	// ACP sessions use a different transport (JSON-RPC 2.0 over POST /rpc).
	if request.agentType == "claude-acp" || request.agentType == "codex-acp" || request.agentType == "pi" || request.agentType == "cursor" || request.agentType == "antigravity" {
		sendACPInitialMessage(ctx, request)
		return
	}

	client := localAgentHTTPClient(request.agentapiURL)

	// Check for existing user messages (idempotency / Pod restart).
	if count := countUserMessages(client, request.agentapiURL); count > 0 {
		log.Printf("[PROVISIONER] User messages already exist (%d), skipping initial message", count)
		return
	}

	// Wait for Claude to be ready (strategy depends on agentType).
	if request.agentType == "" {
		// Default agentapi: wait for running→stable transition OR stable + non-empty message.
		waitForDefaultAgentReady(ctx, client, request.agentapiURL)
	} else {
		// Non-default agents: wait for stable.
		waitForStable(ctx, client, request.agentapiURL, 60)
	}

	// Configured delay before sending.
	log.Printf("[PROVISIONER] Waiting %d second(s) before sending initial message", request.waitSec)
	select {
	case <-ctx.Done():
		return
	case <-time.After(time.Duration(request.waitSec) * time.Second):
	}

	// Double-check race condition.
	if count := countUserMessages(client, request.agentapiURL); count > 0 {
		log.Printf("[PROVISIONER] User messages appeared during wait (%d), skipping", count)
		return
	}

	// Send.
	payload := map[string]string{"content": request.message, "type": "user"}
	body, err := json.Marshal(payload)
	if err != nil {
		log.Printf("[PROVISIONER] Failed to encode initial message: %v", err)
		return
	}
	sender := initialMessageSender{client: client, endpoint: request.agentapiURL + "/message", body: body, label: "Initial message", acceptedStatus: http.StatusOK}
	sender.send(ctx)
}

// sendACPInitialMessage sends an initial message to an ACP bridge session
// via POST /rpc (JSON-RPC 2.0 session/prompt).
func sendACPInitialMessage(ctx context.Context, request initialMessageRequest) {
	client := localAgentHTTPClient(request.agentapiURL)

	// Idempotency: check whether user messages already exist in ACP format.
	if acpInitialMessageDelivered(client, request.agentapiURL, request.message, request.restored, request.markerPath) {
		return
	}

	// Wait for the ACP bridge /status to report "stable".
	waitForStable(ctx, client, request.agentapiURL, 60)

	// Configured delay before sending.
	log.Printf("[PROVISIONER] Waiting %d second(s) before sending ACP initial message", request.waitSec)
	select {
	case <-ctx.Done():
		return
	case <-time.After(time.Duration(request.waitSec) * time.Second):
	}

	// Double-check idempotency after wait.
	if acpInitialMessageDelivered(client, request.agentapiURL, request.message, request.restored, request.markerPath) {
		return
	}

	// Fetch the ACP session ID from GET /session.
	acpSessionId, err := getACPSessionId(client, request.agentapiURL)
	if err != nil {
		log.Printf("[PROVISIONER] ERROR: failed to get ACP session ID: %v", err)
		return
	}
	log.Printf("[PROVISIONER] ACP session ID: %s", acpSessionId)

	// Build JSON-RPC 2.0 session/prompt payload.
	type contentBlock struct {
		Type string `json:"type"`
		Text string `json:"text"`
	}
	type promptParams struct {
		SessionId string         `json:"sessionId"`
		Prompt    []contentBlock `json:"prompt"`
	}
	type rpcRequest struct {
		JSONRPC string       `json:"jsonrpc"`
		ID      int          `json:"id"`
		Method  string       `json:"method"`
		Params  promptParams `json:"params"`
	}
	payload := rpcRequest{
		JSONRPC: "2.0",
		ID:      1,
		Method:  "session/prompt",
		Params: promptParams{
			SessionId: acpSessionId,
			Prompt:    []contentBlock{{Type: "text", Text: request.message}},
		},
	}
	body, err := json.Marshal(payload)
	if err != nil {
		log.Printf("[PROVISIONER] ERROR: failed to encode ACP initial message: %v", err)
		return
	}

	sender := initialMessageSender{
		client: client, endpoint: request.agentapiURL + "/rpc", body: body, label: "ACP initial message",
		acceptedStatus: http.StatusAccepted, alternateStatus: http.StatusOK,
		onSuccess: func() { writeInitialMessageMarker(request.markerPath) },
	}
	sender.send(ctx)
}

type initialMessageSender struct {
	client                          *http.Client
	endpoint, label                 string
	body                            []byte
	acceptedStatus, alternateStatus int
	onSuccess                       func()
}

func (s initialMessageSender) send(ctx context.Context) {
	for attempt := 1; attempt <= 5; attempt++ {
		log.Printf("[PROVISIONER] %s send attempt %d/5", s.label, attempt)
		status, err := s.sendOnce(ctx)
		if err != nil {
			log.Printf("[PROVISIONER] %s send error: %v, attempt %d/5", s.label, err, attempt)
		} else if status == s.acceptedStatus || status == s.alternateStatus {
			log.Printf("[PROVISIONER] %s sent successfully (HTTP %d)", s.label, status)
			if s.onSuccess != nil {
				s.onSuccess()
			}
			return
		} else {
			log.Printf("[PROVISIONER] %s send failed (HTTP %d), attempt %d/5", s.label, status, attempt)
		}
		if attempt < 5 && !waitForInitialMessageRetry(ctx) {
			return
		}
	}
	log.Printf("[PROVISIONER] ERROR: Failed to send %s after 5 attempts", s.label)
}

func (s initialMessageSender) sendOnce(ctx context.Context) (int, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.endpoint, bytes.NewReader(s.body))
	if err != nil {
		return 0, err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := s.client.Do(req)
	if err != nil {
		return 0, err
	}
	closeutils.SafeCloseResponseBody(resp.Body)
	return resp.StatusCode, nil
}

func waitForInitialMessageRetry(ctx context.Context) bool {
	select {
	case <-ctx.Done():
		return false
	case <-time.After(3 * time.Second):
		return true
	}
}
