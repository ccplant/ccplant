package client

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestClientSimulateSlackBot(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/slackbots/bot-1/simulate" {
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Fatal(err)
		}
		if string(body) != `{"event":{"type":"message"}}` {
			t.Fatalf("unexpected body: %s", body)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"dry_run":true,"decision":"create_or_reuse","side_effects":[]}`))
	}))
	defer server.Close()

	result, err := NewClient(server.URL).SimulateSlackBot(context.Background(), "bot-1", []byte(`{"event":{"type":"message"}}`))
	if err != nil {
		t.Fatal(err)
	}
	if string(result) != `{"dry_run":true,"decision":"create_or_reuse","side_effects":[]}` {
		t.Fatalf("unexpected result: %s", result)
	}
}
