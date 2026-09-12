package bridge

import (
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
	if !b.suppressRestoredUserEcho {
		t.Fatal("restored bridge must suppress the session/load user echo")
	}
}
