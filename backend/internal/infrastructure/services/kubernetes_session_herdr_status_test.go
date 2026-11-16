package services

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestHerdrResponseHasRunningTerminal(t *testing.T) {
	tests := []struct {
		name     string
		terminal string
		want     bool
	}{
		{name: "working agent", terminal: `{"state":"live","status":{"state":"working"}}`, want: true},
		{name: "blocked agent", terminal: `{"state":"live","status":{"state":"blocked"}}`, want: true},
		{name: "plain program", terminal: `{"state":"live","status":null}`, want: true},
		{name: "idle agent", terminal: `{"state":"live","status":{"state":"idle"}}`, want: false},
		{name: "done agent", terminal: `{"state":"live","status":{"state":"done"}}`, want: false},
		{name: "finished program", terminal: `{"state":"finished","status":null}`, want: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				_, _ = w.Write([]byte(`{"jsonrpc":"2.0","id":1,"result":{"terminals":[` + tt.terminal + `]}}`))
			}))
			defer server.Close()
			req, err := http.NewRequestWithContext(context.Background(), http.MethodPost, server.URL, strings.NewReader(`{}`))
			if err != nil {
				t.Fatal(err)
			}
			if got := herdrResponseHasRunningTerminal(server.Client(), req); got != tt.want {
				t.Fatalf("herdrResponseHasRunningTerminal() = %v, want %v", got, tt.want)
			}
		})
	}
}
