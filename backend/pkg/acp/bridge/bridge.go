// Package bridge provides a minimal HTTP transport layer for the Agent Client Protocol (ACP).
// Instead of translating ACP messages to a proprietary format, it relays JSON-RPC 2.0 messages
// directly over HTTP: POST /rpc (client→agent) and GET /sse (agent→client).
package bridge

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/takutakahashi/agentapi-proxy/pkg/acp"
)

// ----------------------------------------------------------------------------
// Wire types
// ----------------------------------------------------------------------------

// jsonRPCMsg is a JSON-RPC 2.0 message emitted to SSE subscribers.
type jsonRPCMsg struct {
	JSONRPC string           `json:"jsonrpc"`
	ID      *json.RawMessage `json:"id,omitempty"`
	Method  string           `json:"method,omitempty"`
	Params  interface{}      `json:"params,omitempty"`
	Result  interface{}      `json:"result,omitempty"`
	Error   *rpcError        `json:"error,omitempty"`
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

// sessionUpdateParams is the params envelope for a session/update notification.
type sessionUpdateParams struct {
	SessionId string            `json:"sessionId"`
	Update    acp.SessionUpdate `json:"update"`
	Time      time.Time         `json:"time"`
}

// ----------------------------------------------------------------------------
// Subscriber
// ----------------------------------------------------------------------------

type subscriber struct {
	ch chan json.RawMessage
}

// ----------------------------------------------------------------------------
// Bridge
// ----------------------------------------------------------------------------

// Bridge connects an ACP client to HTTP SSE subscribers via raw JSON-RPC 2.0 messages.
// It does not translate ACP semantics – it reconstructs JSON-RPC envelopes from
// the parsed events that acp.Client exposes and broadcasts them to subscribers.
type Bridge struct {
	acp         *acp.Client
	sessionId   string
	verbose     bool
	autoApprove bool // when true, permission requests are auto-approved without broadcasting to the UI
	serverCtx   context.Context
	outputFile  string // path to append conversation history in acp-posts format
	historyFile string // raw JSON-RPC JSONL used to restore GET /messages

	subsMu sync.Mutex
	subs   []*subscriber

	// Message history: every broadcast message is appended so that reconnecting
	// SSE clients can replay missed events via GET /messages.
	histMu                  sync.RWMutex
	history                 []json.RawMessage
	lastUserMessageIdx      int
	userMessageIndices      []int // history indices of each user_message_chunk
	lastHistoryWasUserChunk bool
	restoredHistory         bool

	// Agent-initiated RPCs (e.g. session/request_permission):
	// We assign local sequential ids, emit them via SSE, and await replies on POST /rpc.
	agentReqSeq    atomic.Int64
	pendingReplyMu sync.Mutex
	pendingReplies map[int64]chan json.RawMessage // local agentReqId → reply channel

	// Chunk buffer: accumulates consecutive chunk updates of the same kind and
	// emits them as a single batched session/update when the kind changes or the
	// agent turn ends.  This converts the per-chunk stream into a per-comment bulk
	// delivery so that consumers receive one complete message at a time.
	chunkMu   sync.Mutex
	chunkKind acp.SessionUpdateKind
	chunkText strings.Builder

	// Status tracking: "running" while a prompt is being processed, "stable" otherwise.
	// Mirrors the agentapi status convention so the proxy can watch GET /events.
	statusMu      sync.RWMutex
	currentStatus string
	statusSubsMu  sync.Mutex
	statusSubs    []*statusSubscriber
}

// SetHistoryFile restores prior raw bridge events and persists future events.
func (b *Bridge) SetHistoryFile(path string) error {
	b.historyFile = path
	if path == "" {
		return nil
	}
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	for _, line := range bytes.Split(data, []byte{'\n'}) {
		line = bytes.TrimSpace(line)
		if len(line) == 0 || !json.Valid(line) {
			continue
		}
		raw := append(json.RawMessage(nil), line...)
		// Older runtimes persisted shutdown cancellation as an RPC error. Drop
		// that known-invalid terminal record when restoring history so sessions
		// suspended before the fix do not reopen in an error state.
		if isPromptContextCanceledErrorRaw(raw) {
			continue
		}
		if isUserMessageRaw(raw) {
			b.lastUserMessageIdx = len(b.history)
			b.userMessageIndices = append(b.userMessageIndices, len(b.history))
		}
		b.history = append(b.history, raw)
	}
	b.restoredHistory = len(b.history) > 0
	return nil
}

func isPromptContextCanceledErrorRaw(raw json.RawMessage) bool {
	var msg struct {
		Error *rpcError `json:"error"`
	}
	return json.Unmarshal(raw, &msg) == nil && msg.Error != nil &&
		msg.Error.Code == -32000 && msg.Error.Message == "acp session/prompt: context canceled"
}

func isTranscriptKind(kind acp.SessionUpdateKind) bool {
	switch kind {
	case acp.SessionUpdateKindUserMessageChunk,
		acp.SessionUpdateKindAgentMessageChunk,
		acp.SessionUpdateKindAgentThoughtChunk,
		acp.SessionUpdateKindToolCall,
		acp.SessionUpdateKindToolCallUpdate,
		acp.SessionUpdateKindPlan:
		return true
	default:
		return false
	}
}

func isUserMessageRaw(raw json.RawMessage) bool {
	var msg struct {
		Method string `json:"method"`
		Params struct {
			Update struct {
				SessionUpdate string `json:"sessionUpdate"`
			} `json:"update"`
		} `json:"params"`
	}
	return json.Unmarshal(raw, &msg) == nil && msg.Method == "session/update" && msg.Params.Update.SessionUpdate == "user_message_chunk"
}

type statusSubscriber struct {
	ch chan string
}

// New creates a Bridge backed by the given ACP client.
// sessionId must be the id returned by acp.Client.SessionID() after session/new.
// outputFile, when non-empty, is a path where completed agent messages are appended
// in acp-posts JSONL format for consumption by the acp-posts Slack integration.
// autoApprove, when true, automatically approves all permission requests without
// broadcasting them to the UI (equivalent to always selecting the first option).
func New(client *acp.Client, sessionId string, verbose bool, outputFile string, autoApprove bool) *Bridge {
	return &Bridge{
		acp:                client,
		sessionId:          sessionId,
		verbose:            verbose,
		autoApprove:        autoApprove,
		outputFile:         outputFile,
		pendingReplies:     make(map[int64]chan json.RawMessage),
		currentStatus:      "stable",
		lastUserMessageIdx: -1,
	}
}

// acpPostsEnvelope is the JSONL envelope written to the output file for acp-posts.
type acpPostsEnvelope struct {
	Type      string          `json:"type"`
	Message   acpPostsMessage `json:"message"`
	SessionID string          `json:"sessionId"`
}

type acpPostsMessage struct {
	Content []acpPostsContent `json:"content"`
}

type acpPostsContent struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

// appendToOutputFile writes a completed agent message to b.outputFile in acp-posts format.
func (b *Bridge) appendToOutputFile(text string) {
	if b.outputFile == "" || text == "" {
		return
	}
	if err := os.MkdirAll(filepath.Dir(b.outputFile), 0o755); err != nil {
		log.Printf("[bridge] failed to create output dir for %s: %v", b.outputFile, err)
		return
	}
	envelope := acpPostsEnvelope{
		Type: "assistant",
		Message: acpPostsMessage{
			Content: []acpPostsContent{{Type: "text", Text: text}},
		},
		SessionID: b.sessionId,
	}
	data, err := json.Marshal(envelope)
	if err != nil {
		log.Printf("[bridge] failed to marshal acp-posts envelope: %v", err)
		return
	}
	f, err := os.OpenFile(b.outputFile, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		log.Printf("[bridge] failed to open output file %s: %v", b.outputFile, err)
		return
	}
	defer func() { _ = f.Close() }()
	if _, err := fmt.Fprintf(f, "%s\n", data); err != nil {
		log.Printf("[bridge] failed to write to output file %s: %v", b.outputFile, err)
	}
}

// UserPromptInfo describes one user prompt turn for GET /messages metadata.
type UserPromptInfo struct {
	Index   int    `json:"index"`
	Preview string `json:"preview,omitempty"`
}

// Messages returns a snapshot of broadcasted JSON-RPC messages from the last
// user message onward. This keeps GET /messages focused on the current turn,
// including large tool output produced after the user's most recent prompt.
func (b *Bridge) Messages() []json.RawMessage {
	b.histMu.RLock()
	defer b.histMu.RUnlock()
	return b.messagesFromIndexLocked(b.lastUserMessageIdx)
}

// MessagesSnapshot returns the current-turn messages together with the SSE ID
// of the last event included in the snapshot.
func (b *Bridge) MessagesSnapshot() ([]json.RawMessage, int) {
	b.histMu.RLock()
	defer b.histMu.RUnlock()
	return b.messagesFromIndexLocked(b.lastUserMessageIdx), len(b.history) - 1
}

// UserPromptCount returns the number of user prompts recorded in history.
func (b *Bridge) UserPromptCount() int {
	b.histMu.RLock()
	defer b.histMu.RUnlock()
	return len(b.userMessageIndices)
}

// LatestUserPromptIndex returns the index of the most recent user prompt, or -1
// when no user prompt exists yet.
func (b *Bridge) LatestUserPromptIndex() int {
	b.histMu.RLock()
	defer b.histMu.RUnlock()
	return len(b.userMessageIndices) - 1
}

// MessagesForUserPromptIndex returns messages from the given user prompt index
// up to (but not including) the next user prompt, or the end of history when
// index is the latest prompt.
func (b *Bridge) MessagesForUserPromptIndex(index int) ([]json.RawMessage, bool) {
	b.histMu.RLock()
	defer b.histMu.RUnlock()
	start, end, ok := b.userPromptRangeLocked(index)
	if !ok {
		return nil, false
	}
	out := make([]json.RawMessage, end-start)
	copy(out, b.history[start:end])
	return out, true
}

// UserPromptInfos returns metadata for every user prompt in conversation order.
func (b *Bridge) UserPromptInfos() []UserPromptInfo {
	b.histMu.RLock()
	defer b.histMu.RUnlock()

	infos := make([]UserPromptInfo, len(b.userMessageIndices))
	for i, histIdx := range b.userMessageIndices {
		infos[i] = UserPromptInfo{
			Index:   i,
			Preview: userPromptPreviewFromRawLocked(b.history[histIdx]),
		}
	}
	return infos
}

func (b *Bridge) messagesFromIndexLocked(start int) []json.RawMessage {
	if start < 0 {
		start = 0
	}
	if start >= len(b.history) {
		return []json.RawMessage{}
	}
	out := make([]json.RawMessage, len(b.history)-start)
	copy(out, b.history[start:])
	return out
}

func (b *Bridge) userPromptRangeLocked(index int) (start, end int, ok bool) {
	if index < 0 || index >= len(b.userMessageIndices) {
		return 0, 0, false
	}
	start = b.userMessageIndices[index]
	if index+1 < len(b.userMessageIndices) {
		end = b.userMessageIndices[index+1]
	} else {
		end = len(b.history)
	}
	return start, end, true
}

const userPromptPreviewMaxLen = 80

func userPromptPreviewFromRawLocked(raw json.RawMessage) string {
	var envelope struct {
		Params struct {
			Update struct {
				Content json.RawMessage `json:"content"`
			} `json:"update"`
		} `json:"params"`
	}
	if err := json.Unmarshal(raw, &envelope); err != nil {
		return ""
	}
	var content struct {
		Text string `json:"text"`
	}
	if err := json.Unmarshal(envelope.Params.Update.Content, &content); err != nil {
		return ""
	}
	text := strings.TrimSpace(content.Text)
	if text == "" {
		return ""
	}
	runes := []rune(text)
	if len(runes) <= userPromptPreviewMaxLen {
		return text
	}
	return string(runes[:userPromptPreviewMaxLen]) + "…"
}

// SessionID returns the ACP session ID.
