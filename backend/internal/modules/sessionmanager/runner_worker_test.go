package sessionmanager

import (
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/takutakahashi/agentapi-proxy/internal/domain/entities"
)

type orphanDeleteRecorder struct {
	deleted []string
	fail    string
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
