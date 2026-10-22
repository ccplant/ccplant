package sessionmanager

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/takutakahashi/agentapi-proxy/internal/domain/entities"
	portrepos "github.com/takutakahashi/agentapi-proxy/internal/usecases/ports/repositories"
	"github.com/takutakahashi/agentapi-proxy/pkg/sessionsettings"
)

type orphanDeleteRecorder struct {
	deleted []string
	fail    string
}

type directCreateRecorder struct{ createdID string }

func (r *directCreateRecorder) CreateSession(context.Context, string, *entities.RunServerRequest, []byte) (entities.Session, error) {
	return nil, errors.New("unexpected CreateSession call")
}
func (r *directCreateRecorder) CreateSessionDirect(_ context.Context, id string, _ *entities.RunServerRequest, _ []byte) (entities.Session, error) {
	r.createdID = id
	return runnerWorkerSession{id: id, status: "running"}, nil
}
func (r *directCreateRecorder) GetSession(string) entities.Session                     { return nil }
func (r *directCreateRecorder) ListSessions(entities.SessionFilter) []entities.Session { return nil }
func (r *directCreateRecorder) DeleteSession(string) error                             { return nil }
func (r *directCreateRecorder) SendMessage(context.Context, string, string) error      { return nil }
func (r *directCreateRecorder) StopAgent(context.Context, string) error                { return nil }
func (r *directCreateRecorder) GetMessages(context.Context, string) ([]portrepos.Message, error) {
	return nil, nil
}
func (r *directCreateRecorder) Shutdown(time.Duration) error { return nil }

func TestRunnerWorkerCreatesWorkloadWithAllocatedRunnerID(t *testing.T) {
	var registeredRunnerID string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		switch {
		case req.Method == http.MethodPost && req.URL.Path == "/internal/session-runners/register":
			var body map[string]string
			require.NoError(t, json.NewDecoder(req.Body).Decode(&body))
			registeredRunnerID = body["runner_id"]
			_ = json.NewEncoder(w).Encode(map[string]string{"runner_token": "token"})
		case req.Method == http.MethodGet && req.URL.Path == "/internal/session-runners/allocations/next":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"allocation": map[string]any{"session_id": "public-session", "manager_id": "manager", "generation": 1},
				"lease_id":   "lease", "runtime_token": "runtime-token", "settings": &sessionsettings.SessionSettings{},
			})
		case req.Method == http.MethodPost && req.URL.Path == "/internal/session-runners/allocations/public-session/ack":
			w.WriteHeader(http.StatusNoContent)
		default:
			t.Fatalf("unexpected request: %s %s", req.Method, req.URL.Path)
		}
	}))
	defer server.Close()

	manager := &directCreateRecorder{}
	NewRunnerWorker(manager, server.URL, "manager", "manager-token").runSlotOnce(context.Background(), "native")
	require.NotEmpty(t, registeredRunnerID)
	require.Equal(t, registeredRunnerID, manager.createdID)
	require.NotEqual(t, "public-session", manager.createdID)
}

func (r *orphanDeleteRecorder) DeleteSession(id string) error {
	if id == r.fail {
		return errors.New("cleanup failed")
	}
	r.deleted = append(r.deleted, id)
	return nil
}

func TestCleanupOrphanedSessionsContinuesAfterFailure(t *testing.T) {
	recorder := &orphanDeleteRecorder{fail: "broken"}
	cleanupOrphanedSessions(recorder, []string{"first", "", "broken", "last"})
	require.Equal(t, []string{"first", "last"}, recorder.deleted)
}

type runnerWorkerSession struct{ id, status string }

func (s runnerWorkerSession) ID() string                    { return s.id }
func (s runnerWorkerSession) Addr() string                  { return "" }
func (s runnerWorkerSession) UserID() string                { return "" }
func (s runnerWorkerSession) Scope() entities.ResourceScope { return "" }
func (s runnerWorkerSession) TeamID() string                { return "" }
func (s runnerWorkerSession) Tags() map[string]string       { return nil }
func (s runnerWorkerSession) Status() string                { return s.status }
func (s runnerWorkerSession) StartedAt() time.Time          { return time.Time{} }
func (s runnerWorkerSession) UpdatedAt() time.Time          { return time.Time{} }
func (s runnerWorkerSession) LastMessageAt() time.Time      { return time.Time{} }
func (s runnerWorkerSession) Description() string           { return "" }
func (s runnerWorkerSession) Cancel()                       {}

func TestActiveSessionCountExcludesTerminalSessions(t *testing.T) {
	sessions := []entities.Session{
		runnerWorkerSession{id: "running", status: "running"},
		runnerWorkerSession{id: "creating", status: "creating"},
		runnerWorkerSession{id: "error", status: "error"},
		runnerWorkerSession{id: "stopped", status: "stopped"},
	}
	require.Equal(t, 2, activeSessionCount(sessions))
}
