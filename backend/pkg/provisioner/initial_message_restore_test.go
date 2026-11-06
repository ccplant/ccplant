package provisioner

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
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
	userChunk := func(text string) string {
		return fmt.Sprintf(`{"jsonrpc":"2.0","method":"session/update","params":{"update":{"sessionUpdate":"user_message_chunk","content":{"type":"text","text":%q}}}}`, text)
	}
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/messages":
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
			rpcCalls.Add(1)
			w.WriteHeader(http.StatusAccepted)
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

	sendACPInitialMessage(context.Background(), server.URL, "initial prompt", 0, true)

	if got := rpcCalls.Load(); got != 0 {
		t.Fatalf("session/prompt calls = %d, want 0", got)
	}
}

func TestSendACPInitialMessageRestoredSendsWhenPromptMissing(t *testing.T) {
	var rpcCalls atomic.Int32
	server := newMultiTurnACPServer(t, []string{"older prompt", "follow-up prompt"}, &rpcCalls)
	defer server.Close()

	sendACPInitialMessage(context.Background(), server.URL, "initial prompt", 0, true)

	if got := rpcCalls.Load(); got != 1 {
		t.Fatalf("session/prompt calls = %d, want 1", got)
	}
}
