package provisioner

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"sync/atomic"
	"testing"
)

func TestHasACPUserMessageMatchesExactRestoredPrompt(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"messages":[{"jsonrpc":"2.0","method":"session/update","params":{"update":{"sessionUpdate":"user_message_chunk","content":{"type":"text","text":"old prompt"}}}}]}`))
	}))
	defer server.Close()

	client := server.Client()
	if !hasACPUserMessage(client, server.URL, "old prompt") {
		t.Fatal("expected exact restored prompt to be found")
	}
	if hasACPUserMessage(client, server.URL, "new launch prompt") {
		t.Fatal("new launch prompt must not match older restored history")
	}
}

func newMultiTurnACPServer(t *testing.T, turns []string, rpcCalls *atomic.Int32) *httptest.Server {
	t.Helper()
	return newMultiTurnACPServerWithRPC(t, turns, func(w http.ResponseWriter) {
		rpcCalls.Add(1)
		w.WriteHeader(http.StatusAccepted)
	})
}

func newMultiTurnACPServerWithRPC(t *testing.T, turns []string, rpc func(http.ResponseWriter)) *httptest.Server {
	t.Helper()
	userChunk := func(text string) string {
		return fmt.Sprintf(`{"jsonrpc":"2.0","method":"session/update","params":{"update":{"sessionUpdate":"user_message_chunk","content":{"type":"text","text":%q}}}}`, text)
	}
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/messages":
			if len(turns) == 0 {
				_, _ = w.Write([]byte(`{"messages":[],"userPromptCount":0}`))
				return
			}
			index := len(turns) - 1
			if q := r.URL.Query().Get("userPromptIndex"); q != "" {
				i, err := strconv.Atoi(q)
				if err != nil || i < 0 || i >= len(turns) {
					w.WriteHeader(http.StatusBadRequest)
					_, _ = w.Write([]byte(`{"error":"out of range"}`))
					return
				}
				index = i
			}
			_, _ = fmt.Fprintf(w, `{"messages":[%s],"userPromptCount":%d,"userPromptIndex":%d}`, userChunk(turns[index]), len(turns), index)
		case "/status":
			_, _ = w.Write([]byte(`{"status":"stable"}`))
		case "/session":
			_, _ = w.Write([]byte(`{"sessionId":"acp-session","status":"stable"}`))
		case "/rpc":
			rpc(w)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
}

func TestHasACPUserMessageFindsPromptInEarlierTurn(t *testing.T) {
	var rpcCalls atomic.Int32
	server := newMultiTurnACPServer(t, []string{"initial prompt", "follow-up prompt"}, &rpcCalls)
	defer server.Close()

	client := server.Client()
	if !hasACPUserMessage(client, server.URL, "initial prompt") {
		t.Fatal("expected initial prompt in an earlier turn to be found")
	}
	if !hasACPUserMessage(client, server.URL, "follow-up prompt") {
		t.Fatal("expected prompt in the latest turn to be found")
	}
	if hasACPUserMessage(client, server.URL, "new launch prompt") {
		t.Fatal("new launch prompt must not match any restored turn")
	}
}

func TestSendACPInitialMessageRestoredSkipsWhenPromptInEarlierTurn(t *testing.T) {
	var rpcCalls atomic.Int32
	server := newMultiTurnACPServer(t, []string{"initial prompt", "follow-up prompt"}, &rpcCalls)
	defer server.Close()

	sendACPInitialMessage(context.Background(), server.URL, "initial prompt", 0, true, filepath.Join(t.TempDir(), ".session", "initial-message-sent-sess-1"))

	if got := rpcCalls.Load(); got != 0 {
		t.Fatalf("session/prompt calls = %d, want 0", got)
	}
}

func TestSendACPInitialMessageRestoredSendsWhenPromptMissing(t *testing.T) {
	var rpcCalls atomic.Int32
	server := newMultiTurnACPServer(t, []string{"older prompt", "follow-up prompt"}, &rpcCalls)
	defer server.Close()

	sendACPInitialMessage(context.Background(), server.URL, "initial prompt", 0, true, filepath.Join(t.TempDir(), ".session", "initial-message-sent-sess-1"))

	if got := rpcCalls.Load(); got != 1 {
		t.Fatalf("session/prompt calls = %d, want 1", got)
	}
}

func assertMarkerExists(t *testing.T, path string, want bool) {
	t.Helper()
	_, err := os.Stat(path)
	if want && err != nil {
		t.Fatalf("expected marker %s: %v", path, err)
	}
	if !want && !os.IsNotExist(err) {
		t.Fatalf("expected no marker %s: %v", path, err)
	}
}

func writeTestMarker(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("sent\n"), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestSendACPInitialMessageSkipsWhenOwnMarkerExists(t *testing.T) {
	var rpcCalls atomic.Int32
	server := newMultiTurnACPServer(t, nil, &rpcCalls)
	defer server.Close()
	home := t.TempDir()
	marker := initialMessageMarkerPath(home, "sess-1")
	writeTestMarker(t, marker)

	sendACPInitialMessage(context.Background(), server.URL, "initial prompt", 0, true, marker)

	if got := rpcCalls.Load(); got != 0 {
		t.Fatalf("session/prompt calls = %d, want 0", got)
	}
}

func TestSendACPInitialMessageSendsAndWritesMarkerWhenMissing(t *testing.T) {
	var rpcCalls atomic.Int32
	server := newMultiTurnACPServer(t, []string{"older prompt"}, &rpcCalls)
	defer server.Close()
	marker := initialMessageMarkerPath(t.TempDir(), "sess-1")

	sendACPInitialMessage(context.Background(), server.URL, "initial prompt", 0, true, marker)

	if got := rpcCalls.Load(); got != 1 {
		t.Fatalf("session/prompt calls = %d, want 1", got)
	}
	assertMarkerExists(t, marker, true)
}

func TestSendACPInitialMessageDoesNotWriteMarkerWhenSendFails(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var rpcCalls atomic.Int32
	server := newMultiTurnACPServerWithRPC(t, nil, func(w http.ResponseWriter) {
		rpcCalls.Add(1)
		cancel()
		w.WriteHeader(http.StatusInternalServerError)
	})
	defer server.Close()
	marker := initialMessageMarkerPath(t.TempDir(), "sess-1")

	sendACPInitialMessage(ctx, server.URL, "initial prompt", 0, false, marker)

	if got := rpcCalls.Load(); got != 1 {
		t.Fatalf("session/prompt calls = %d, want 1", got)
	}
	assertMarkerExists(t, marker, false)
}

func TestSendACPInitialMessageSendsWhenOnlyAnotherSessionMarkerExists(t *testing.T) {
	var rpcCalls atomic.Int32
	server := newMultiTurnACPServer(t, []string{"initial prompt", "template follow-up"}, &rpcCalls)
	defer server.Close()
	home := t.TempDir()
	writeTestMarker(t, initialMessageMarkerPath(home, "template-source-session"))
	marker := initialMessageMarkerPath(home, "sess-workspace")

	sendACPInitialMessage(context.Background(), server.URL, "initial prompt", 0, true, marker)

	if got := rpcCalls.Load(); got != 1 {
		t.Fatalf("session/prompt calls = %d, want 1", got)
	}
	assertMarkerExists(t, marker, true)
}

func TestSendACPInitialMessageMigratesLegacyHistoryToMarker(t *testing.T) {
	var rpcCalls atomic.Int32
	server := newMultiTurnACPServer(t, []string{"initial prompt", "follow-up prompt"}, &rpcCalls)
	defer server.Close()
	marker := initialMessageMarkerPath(t.TempDir(), "sess-1")

	sendACPInitialMessage(context.Background(), server.URL, "initial prompt", 0, true, marker)

	if got := rpcCalls.Load(); got != 0 {
		t.Fatalf("session/prompt calls = %d, want 0", got)
	}
	assertMarkerExists(t, marker, true)
}

func TestInitialMessageMarkerPathRejectsUnsafeSessionID(t *testing.T) {
	for _, id := range []string{"", "..", "a/b", "../escape"} {
		if got := initialMessageMarkerPath(t.TempDir(), id); got != "" {
			t.Fatalf("initialMessageMarkerPath(%q) = %q, want empty", id, got)
		}
	}
}
