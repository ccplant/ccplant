package provisioner

import (
	"net/http"
	"net/http/httptest"
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
