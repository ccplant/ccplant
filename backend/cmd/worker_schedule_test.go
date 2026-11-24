package cmd

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	testassert "github.com/stretchr/testify/assert"
	"github.com/takutakahashi/agentapi-proxy/internal/infrastructure/controlapi"
)

func TestRemoteScheduleWorkerCallsStartThenFinalizes(t *testing.T) {
	finalized := make(chan map[string]string, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/internal/worker/schedules/claim-due":
			testassert.NoError(t, json.NewEncoder(w).Encode(map[string]any{"jobs": []controlapi.ScheduleJob{{ScheduleID: "schedule", ExecutionID: "execution", SessionID: "session", ExecutionToken: "signed"}}}))
		case "/start":
			if r.Header.Get("Authorization") != "Bearer signed" || r.Header.Get("Idempotency-Key") != "execution" {
				t.Errorf("invalid start headers")
			}
			testassert.NoError(t, json.NewEncoder(w).Encode(map[string]string{"session_id": "session"}))
		case "/internal/worker/schedules/schedule/finalize":
			var body map[string]string
			_ = json.NewDecoder(r.Body).Decode(&body)
			finalized <- body
			w.WriteHeader(http.StatusNoContent)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	w := &remoteScheduleWorker{client: controlapi.NewSessionManager(server.URL, "control"), sessionAPIURL: server.URL}
	w.process(context.Background())
	result := <-finalized
	if result["status"] != "success" || result["session_id"] != "session" {
		t.Fatalf("finalize=%v", result)
	}
}
