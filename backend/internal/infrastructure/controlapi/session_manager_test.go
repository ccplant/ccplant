package controlapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/takutakahashi/agentapi-proxy/internal/domain/entities"
)

func TestSessionManagerDelegatesCreateAndStockToControlAPI(t *testing.T) {
	requests := make(chan string, 2)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer token" {
			t.Errorf("authorization = %q", r.Header.Get("Authorization"))
		}
		requests <- r.Method + " " + r.URL.RequestURI()
		switch r.URL.Path {
		case "/internal/worker/sessions/session-1":
			w.WriteHeader(http.StatusCreated)
			_ = json.NewEncoder(w).Encode(sessionInfo{ID: "session-1", UserID: "user", Scope: entities.ScopeUser, Status: "running"})
		case "/internal/worker/stock":
			w.WriteHeader(http.StatusCreated)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	client := NewSessionManager(server.URL, "token")
	if _, err := client.CreateSession(context.Background(), "session-1", &entities.RunServerRequest{UserID: "user"}, nil); err != nil {
		t.Fatal(err)
	}
	if err := client.CreateStockSession(context.Background(), true); err != nil {
		t.Fatal(err)
	}
	if got := <-requests; got != "POST /internal/worker/sessions/session-1" {
		t.Fatalf("request = %q", got)
	}
	if got := <-requests; got != "POST /internal/worker/stock?dind=true" {
		t.Fatalf("request = %q", got)
	}
}

func TestSessionManagerDelegatesLeaseToControlAPI(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/internal/worker/leases/agentapi:leader:test:worker" {
			http.NotFound(w, r)
			return
		}
		var request leaseRequest
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Fatal(err)
		}
		if request.Action != "acquire" || request.Identity != "worker-1" || request.DurationMS != 15000 {
			t.Fatalf("request = %#v", request)
		}
		_ = json.NewEncoder(w).Encode(map[string]bool{"acquired": true})
	}))
	defer server.Close()

	acquired, err := NewSessionManager(server.URL, "token").Acquire(context.Background(), "agentapi:leader:test:worker", "worker-1", 15*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if !acquired {
		t.Fatal("lease was not acquired")
	}
}

func TestScheduleClientClaimsStartsAndFinalizes(t *testing.T) {
	var finalized bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/internal/worker/schedules/claim-due":
			if r.Header.Get("Authorization") != "Bearer control" {
				t.Fatal("missing control token")
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"jobs": []ScheduleJob{{ScheduleID: "s", ExecutionID: "e", SessionID: "session", ExecutionToken: "execution", StartRequest: entities.StartRequest{}}}})
		case "/start":
			if r.Header.Get("Authorization") != "Bearer execution" || r.Header.Get("Idempotency-Key") != "e" {
				t.Fatal("missing execution authentication")
			}
			_ = json.NewEncoder(w).Encode(map[string]string{"session_id": "session"})
		case "/internal/worker/schedules/s/finalize":
			finalized = true
			w.WriteHeader(http.StatusNoContent)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	client := NewSessionManager(server.URL, "control")
	jobs, err := client.ClaimDueSchedules(context.Background())
	if err != nil || len(jobs) != 1 {
		t.Fatalf("jobs=%v err=%v", jobs, err)
	}
	id, err := client.StartScheduledSession(context.Background(), server.URL, jobs[0])
	if err != nil || id != "session" {
		t.Fatalf("id=%q err=%v", id, err)
	}
	if err := client.FinalizeSchedule(context.Background(), jobs[0], "success", id, ""); err != nil {
		t.Fatal(err)
	}
	if !finalized {
		t.Fatal("execution was not finalized")
	}
}
