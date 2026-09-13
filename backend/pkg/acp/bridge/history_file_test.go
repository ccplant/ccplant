package bridge

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestSetHistoryFileRestoresRawMessages(t *testing.T) {
	path := filepath.Join(t.TempDir(), "history.jsonl")
	raw := `{"jsonrpc":"2.0","method":"session/update","params":{"update":{"sessionUpdate":"user_message_chunk"}}}`
	if err := os.WriteFile(path, []byte(raw+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	b := New(nil, "session", false, "", false)
	if err := b.SetHistoryFile(path); err != nil {
		t.Fatal(err)
	}
	if len(b.history) != 1 || len(b.userMessageIndices) != 1 {
		t.Fatalf("history=%d user messages=%d", len(b.history), len(b.userMessageIndices))
	}
	if len(b.restoredReplaySignatures) != 1 {
		t.Fatalf("restored replay signatures=%d, want 1", len(b.restoredReplaySignatures))
	}
}

func TestBroadcastSuppressesRestoredSessionLoadTranscript(t *testing.T) {
	path := filepath.Join(t.TempDir(), "history.jsonl")
	user := `{"jsonrpc":"2.0","method":"session/update","params":{"sessionId":"old","update":{"sessionUpdate":"user_message_chunk","content":{"type":"text","text":"hello"}},"time":"old"}}`
	agent := `{"jsonrpc":"2.0","method":"session/update","params":{"sessionId":"old","update":{"sessionUpdate":"agent_message_chunk","content":{"type":"text","text":"hi"}},"time":"old"}}`
	if err := os.WriteFile(path, []byte(user+"\n"+agent+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	b := New(nil, "session", false, "", false)
	if err := b.SetHistoryFile(path); err != nil {
		t.Fatal(err)
	}

	for _, replay := range []string{
		`{"jsonrpc":"2.0","method":"session/update","params":{"sessionId":"new","update":{"sessionUpdate":"user_message_chunk","content":{"type":"text","text":"hello"}},"time":"new"}}`,
		`{"jsonrpc":"2.0","method":"session/update","params":{"sessionId":"new","update":{"sessionUpdate":"session_info_update"},"time":"new"}}`,
		`{"jsonrpc":"2.0","method":"session/update","params":{"sessionId":"new","update":{"sessionUpdate":"agent_message_chunk","content":{"type":"text","text":"hi"}},"time":"new"}}`,
	} {
		var msg jsonRPCMsg
		if err := json.Unmarshal([]byte(replay), &msg); err != nil {
			t.Fatal(err)
		}
		b.broadcast(msg)
	}
	if len(b.history) != 3 { // original transcript plus non-transcript metadata
		t.Fatalf("history=%d, want 3", len(b.history))
	}
	if len(b.userMessageIndices) != 1 {
		t.Fatalf("user messages=%d, want 1", len(b.userMessageIndices))
	}
}
