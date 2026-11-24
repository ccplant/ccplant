package bridge

import (
	"context"
	"encoding/json"
	"log"
	"os"
	"path/filepath"
	"time"

	"github.com/takutakahashi/agentapi-proxy/pkg/acp"
)

func (b *Bridge) SetSessionConfigOption(ctx context.Context, configId, value string) (acp.SessionSetConfigOptionResult, error) {
	result, err := b.acp.SetSessionConfigOption(ctx, configId, value)
	if err != nil {
		return acp.SessionSetConfigOptionResult{}, err
	}

	b.broadcast(jsonRPCMsg{
		JSONRPC: "2.0",
		Method:  "session/update",
		Params: sessionUpdateParams{
			SessionId: b.sessionId,
			Update: acp.SessionUpdate{
				Kind:          acp.SessionUpdateKindConfigOptionUpdate,
				ConfigOptions: result.ConfigOptions,
			},
			Time: time.Now(),
		},
	})

	return result, nil
}

// Cancel cancels the current agent turn.
func (b *Bridge) Cancel(ctx context.Context) error {
	return b.acp.Cancel(ctx)
}

// SubscribeFromCurrent is a sentinel value for SubscribeFrom that means
// "subscribe from the current position without replaying any history".
// Use this when the caller obtains history separately (e.g. GET /messages).
const SubscribeFromCurrent = -2

// SubscribeFrom subscribes to new messages and atomically returns a snapshot of
// history starting from lastEventID+1. This prevents race conditions where messages
// could be missed between fetching history and subscribing to the live channel.
//
// Pass SubscribeFromCurrent as lastEventID to subscribe from the current position
// without replaying any history (useful when history is fetched via GET /messages).
//
// Callers must invoke the returned cancel function when done.
// The returned nextIdx is the history index that the first channel message will have.
func (b *Bridge) SubscribeFrom(lastEventID int) (<-chan json.RawMessage, []json.RawMessage, int, func()) {
	// Hold subsMu first to block concurrent broadcasts while we snapshot history
	// and add the subscriber atomically. broadcast() must also acquire subsMu first.
	b.subsMu.Lock()

	b.histMu.RLock()
	histLen := len(b.history)
	var start int
	if lastEventID == SubscribeFromCurrent {
		start = histLen // no replay: subscribe from current position
	} else {
		start = lastEventID + 1
		if start < 0 {
			start = 0
		}
		if start > histLen {
			start = histLen
		}
	}
	history := make([]json.RawMessage, histLen-start)
	copy(history, b.history[start:])
	b.histMu.RUnlock()

	sub := &subscriber{ch: make(chan json.RawMessage, 128)}
	b.subs = append(b.subs, sub)
	count := len(b.subs)
	b.subsMu.Unlock()

	log.Printf("[bridge] SSE subscriber connected (session=%s, total=%d, lastEventID=%d, histLen=%d)", b.sessionId, count, lastEventID, histLen)

	cancel := func() {
		b.subsMu.Lock()
		for i, s := range b.subs {
			if s == sub {
				b.subs = append(b.subs[:i], b.subs[i+1:]...)
				break
			}
		}
		remaining := len(b.subs)
		b.subsMu.Unlock()
		close(sub.ch)
		log.Printf("[bridge] SSE subscriber disconnected (session=%s, remaining=%d)", b.sessionId, remaining)
	}
	return sub.ch, history, histLen, cancel
}

// ----------------------------------------------------------------------------
// Internal helpers
// ----------------------------------------------------------------------------

func (b *Bridge) broadcast(msg jsonRPCMsg) {
	raw, err := json.Marshal(msg)
	if err != nil {
		if b.verbose {
			log.Printf("[bridge] marshal error: %v", err)
		}
		return
	}
	if b.verbose {
		log.Printf("[bridge] broadcast: %s", raw)
	}

	// Acquire subsMu FIRST (same order as SubscribeFrom) to ensure that when a
	// new subscriber is being added concurrently, the boundary between history
	// and live channel messages is consistent with no gaps or duplicates.
	b.subsMu.Lock()
	defer b.subsMu.Unlock()

	// Persist to history inside subsMu so SubscribeFrom sees a consistent snapshot.
	b.histMu.Lock()
	isUserChunk := isUserMessageUpdate(msg)
	if isUserChunk && !b.lastHistoryWasUserChunk {
		b.lastUserMessageIdx = len(b.history)
		b.userMessageIndices = append(b.userMessageIndices, len(b.history))
	}
	b.lastHistoryWasUserChunk = isUserChunk
	b.history = append(b.history, raw)
	if b.historyFile != "" {
		if err := os.MkdirAll(filepath.Dir(b.historyFile), 0o700); err == nil {
			if f, openErr := os.OpenFile(b.historyFile, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600); openErr == nil {
				_, _ = f.Write(append(append([]byte(nil), raw...), '\n'))
				_ = f.Close()
			}
		}
	}
	b.histMu.Unlock()

	for _, sub := range b.subs {
		select {
		case sub.ch <- raw:
		default:
			// Slow subscriber – drop the message rather than blocking.
		}
	}
}

func isUserMessageUpdate(msg jsonRPCMsg) bool {
	if msg.Method != "session/update" {
		return false
	}
	switch params := msg.Params.(type) {
	case sessionUpdateParams:
		return params.Update.Kind == acp.SessionUpdateKindUserMessageChunk
	case *sessionUpdateParams:
		return params != nil && params.Update.Kind == acp.SessionUpdateKindUserMessageChunk
	default:
		return false
	}
}
