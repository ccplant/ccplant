package bridge

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"time"

	"github.com/takutakahashi/agentapi-proxy/pkg/acp"
)

func (b *Bridge) SessionID() string { return b.sessionId }

// SessionRuntimeInfo returns the runtime information reported by the ACP agent.
func (b *Bridge) SessionRuntimeInfo() acp.SessionRuntimeInfo {
	return b.acp.SessionRuntimeInfo()
}

// Status returns the current agent status ("running" or "stable").
func (b *Bridge) Status() string {
	b.statusMu.RLock()
	defer b.statusMu.RUnlock()
	return b.currentStatus
}

// SubscribeStatus returns a channel that receives status strings whenever the
// agent status changes, and a cancel function that must be called on disconnect.
func (b *Bridge) SubscribeStatus() (<-chan string, func()) {
	sub := &statusSubscriber{ch: make(chan string, 4)}
	b.statusSubsMu.Lock()
	b.statusSubs = append(b.statusSubs, sub)
	b.statusSubsMu.Unlock()

	cancel := func() {
		b.statusSubsMu.Lock()
		for i, s := range b.statusSubs {
			if s == sub {
				b.statusSubs = append(b.statusSubs[:i], b.statusSubs[i+1:]...)
				break
			}
		}
		b.statusSubsMu.Unlock()
		close(sub.ch)
	}
	return sub.ch, cancel
}

// setStatus updates the current status and notifies all subscribers.
func (b *Bridge) setStatus(status string) {
	b.statusMu.Lock()
	prev := b.currentStatus
	b.currentStatus = status
	b.statusMu.Unlock()

	if prev == status {
		return
	}
	if b.verbose {
		log.Printf("[bridge] status: %s → %s (session=%s)", prev, status, b.sessionId)
	}

	b.statusSubsMu.Lock()
	defer b.statusSubsMu.Unlock()
	for _, sub := range b.statusSubs {
		select {
		case sub.ch <- status:
		default:
		}
	}
}

// ----------------------------------------------------------------------------
// Event loop
// ----------------------------------------------------------------------------

// Run starts the event loop. It blocks until ctx is cancelled.
// Call this in a goroutine before starting the HTTP server.
func (b *Bridge) Run(ctx context.Context) {
	b.serverCtx = ctx
	updates := b.acp.Updates()
	perms := b.acp.PermissionRequests()

	for {
		select {
		case <-ctx.Done():
			return

		case update, ok := <-updates:
			if !ok {
				return
			}
			b.emitUpdate(update)

		case req, ok := <-perms:
			if !ok {
				return
			}
			b.handlePermissionRequest(req)
		}
	}
}

// isChunkKind reports whether kind is a streaming chunk that should be buffered.
func isChunkKind(kind acp.SessionUpdateKind) bool {
	return kind == acp.SessionUpdateKindAgentMessageChunk ||
		kind == acp.SessionUpdateKindUserMessageChunk ||
		kind == acp.SessionUpdateKindAgentThoughtChunk
}

// emitUpdate buffers consecutive chunk updates of the same kind and broadcasts
// them as a single batched session/update when the kind changes or a non-chunk
// event arrives.  Non-chunk events (tool_call, plan, …) are broadcast immediately
// after the pending chunk buffer is flushed.
func (b *Bridge) emitUpdate(update acp.SessionUpdate) {
	if update.Replayed && b.restoredHistory && isTranscriptKind(update.Kind) {
		return
	}
	if isChunkKind(update.Kind) {
		// Text chunks are coalesced to reduce UI churn. Binary content blocks
		// (notably image output) must be relayed intact instead of disappearing
		// into the text-only buffer.
		text := acp.ExtractTextContent(update.Content)
		if text == "" {
			b.flushChunkBuffer()
			b.broadcast(jsonRPCMsg{
				JSONRPC: "2.0",
				Method:  "session/update",
				Params: sessionUpdateParams{
					SessionId: b.sessionId,
					Update:    update,
					Time:      time.Now(),
				},
			})
			return
		}
		b.chunkMu.Lock()
		if b.chunkKind != "" && b.chunkKind != update.Kind {
			// Kind changed – flush the previous buffer before accumulating.
			b.flushChunkBufferLocked()
		}
		b.chunkKind = update.Kind
		b.chunkText.WriteString(text)
		b.chunkMu.Unlock()
		return
	}

	// Non-chunk event: flush pending chunks first, then broadcast immediately.
	b.flushChunkBuffer()

	msg := jsonRPCMsg{
		JSONRPC: "2.0",
		Method:  "session/update",
		Params: sessionUpdateParams{
			SessionId: b.sessionId,
			Update:    update,
			Time:      time.Now(),
		},
	}
	b.broadcast(msg)
}

// flushChunkBuffer flushes the accumulated chunk buffer as a single session/update.
// Safe to call from any goroutine.
func (b *Bridge) flushChunkBuffer() {
	b.chunkMu.Lock()
	defer b.chunkMu.Unlock()
	b.flushChunkBufferLocked()
}

// flushChunkBufferLocked flushes the buffer. Must be called with b.chunkMu held.
func (b *Bridge) flushChunkBufferLocked() {
	if b.chunkKind == "" || b.chunkText.Len() == 0 {
		return
	}
	text := b.chunkText.String()
	kind := b.chunkKind
	b.chunkKind = ""
	b.chunkText.Reset()

	contentRaw, err := json.Marshal(acp.ContentBlockText{Type: "text", Text: text})
	if err != nil {
		log.Printf("[bridge] failed to marshal buffered chunk: %v", err)
		return
	}
	now := time.Now()
	msg := jsonRPCMsg{
		JSONRPC: "2.0",
		Method:  "session/update",
		Params: sessionUpdateParams{
			SessionId: b.sessionId,
			Update: acp.SessionUpdate{
				Kind:    kind,
				Content: json.RawMessage(contentRaw),
			},
			Time: now,
		},
	}
	// broadcast without holding chunkMu to avoid lock-order inversion.
	b.chunkMu.Unlock()
	b.broadcast(msg)
	// Persist completed agent messages to the output file for acp-posts consumption.
	if kind == acp.SessionUpdateKindAgentMessageChunk {
		b.appendToOutputFile(text)
	}
	b.chunkMu.Lock()
}

// handlePermissionRequest emits a session/request_permission request via SSE
// and waits for the HTTP client to reply via POST /rpc.
// When autoApprove is set, the first available option is selected immediately
// without broadcasting to the UI.
func (b *Bridge) handlePermissionRequest(req acp.PermissionRequest) {
	if b.autoApprove {
		if len(req.Params.Options) > 0 {
			log.Printf("[bridge] auto-approving permission request (option=%s)", req.Params.Options[0].OptionId)
			replyPermission(req, req.Params.Options[0].OptionId)
		} else {
			replyPermission(req, "")
		}
		return
	}

	id := b.agentReqSeq.Add(1)
	idRaw, err := json.Marshal(id)
	if err != nil {
		log.Printf("[bridge] failed to marshal permission request id: %v", err)
		replyPermission(req, "")
		return
	}
	idRawMsg := json.RawMessage(idRaw)

	replyCh := make(chan json.RawMessage, 1)

	b.pendingReplyMu.Lock()
	b.pendingReplies[id] = replyCh
	b.pendingReplyMu.Unlock()

	msg := jsonRPCMsg{
		JSONRPC: "2.0",
		ID:      &idRawMsg,
		Method:  "session/request_permission",
		Params:  req.Params,
	}
	b.broadcast(msg)

	go func() {
		defer func() {
			b.pendingReplyMu.Lock()
			delete(b.pendingReplies, id)
			b.pendingReplyMu.Unlock()
		}()

		select {
		case <-b.serverCtx.Done():
			replyPermission(req, "")
		case raw := <-replyCh:
			var result acp.RequestPermissionResult
			if err := json.Unmarshal(raw, &result); err == nil {
				replyPermission(req, result.Outcome.OptionId)
			} else {
				replyPermission(req, "")
			}
		}
	}()
}

func replyPermission(request acp.PermissionRequest, optionID string) {
	if err := request.Reply(optionID); err != nil {
		log.Printf("[bridge] failed to reply to permission request: %v", err)
	}
}

// ----------------------------------------------------------------------------
// Public API (called by the HTTP server)
// ----------------------------------------------------------------------------

// SendPrompt sends a session/prompt request to the ACP agent.
// clientID is the raw JSON-RPC id from the HTTP client; the result (or error)
// is emitted via SSE with that same id so the client can correlate the response.
func (b *Bridge) SendPrompt(clientID json.RawMessage, prompt []acp.ContentBlock) error {
	promptCtx := b.serverCtx
	if promptCtx == nil {
		promptCtx = context.Background()
	}

	log.Printf("[bridge] SendPrompt (session=%s, clientID=%s, blocks=%d)", b.sessionId, clientID, len(prompt))

	// Broadcast the user's prompt blocks as synthetic session/update notifications.
	// The ACP server does not echo user messages, so we emit it here to ensure
	// it appears in both the SSE live stream and GET /messages history.
	for _, block := range prompt {
		userContentRaw, err := json.Marshal(block)
		if err != nil {
			return fmt.Errorf("marshal prompt content block: %w", err)
		}
		b.broadcast(jsonRPCMsg{
			JSONRPC: "2.0",
			Method:  "session/update",
			Params: sessionUpdateParams{
				SessionId: b.sessionId,
				Update: acp.SessionUpdate{
					Kind:    acp.SessionUpdateKindUserMessageChunk,
					Content: json.RawMessage(userContentRaw),
				},
				Time: time.Now(),
			},
		})
	}
	b.setStatus("running")

	go func() {
		stopReason, err := b.acp.PromptBlocks(promptCtx, prompt)
		b.finishPrompt(clientID, stopReason, err)
	}()

	return nil
}

func (b *Bridge) finishPrompt(clientID json.RawMessage, stopReason acp.StopReason, err error) {
	// Flush any chunk that was still buffered when the agent turn ended.
	b.flushChunkBuffer()
	b.setStatus("stable")

	if err != nil {
		// A client cancellation or server shutdown interrupts the in-flight ACP
		// request with context.Canceled. This is a normal cancelled turn, not an
		// agent failure. Persisting it as a JSON-RPC error makes a suspended
		// session reopen in an error state after its history is restored.
		if errors.Is(err, context.Canceled) {
			stopReason = acp.StopReasonCancelled
			log.Printf("[bridge] Prompt cancelled (session=%s)", b.sessionId)
		} else {
			log.Printf("[bridge] Prompt error (session=%s): %v", b.sessionId, err)
			b.broadcast(jsonRPCMsg{
				JSONRPC: "2.0",
				ID:      &clientID,
				Error:   &rpcError{Code: -32000, Message: err.Error()},
			})
			return
		}
	}

	log.Printf("[bridge] Prompt done (session=%s, stopReason=%s)", b.sessionId, stopReason)
	b.broadcast(jsonRPCMsg{
		JSONRPC: "2.0",
		ID:      &clientID,
		Result:  acp.PromptResult{StopReason: stopReason},
	})
}

// HandleReply routes a JSON-RPC result from the HTTP client to a pending
// agent-initiated request (e.g. session/request_permission).
// id is the integer id the bridge assigned when it emitted the agent request.
func (b *Bridge) HandleReply(id int64, result json.RawMessage) error {
	b.pendingReplyMu.Lock()
	ch, ok := b.pendingReplies[id]
	b.pendingReplyMu.Unlock()

	if !ok {
		return fmt.Errorf("no pending agent request for id %d", id)
	}
	select {
	case ch <- result:
	default:
	}
	return nil
}

// SetSessionConfigOption forwards a session/set_config_option request to the ACP agent.
// The result is also emitted as a config_option_update notification so live UI clients
// observe the same configuration state that the HTTP caller receives.
