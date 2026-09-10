package slackbot_cleanup

import (
	"context"
	"testing"
	"time"

	"github.com/takutakahashi/agentapi-proxy/internal/domain/entities"
	portrepos "github.com/takutakahashi/agentapi-proxy/internal/usecases/ports/repositories"
)

type mockSessionManager struct {
	sessions   []entities.Session
	deletedIDs []string
}

func (m *mockSessionManager) CreateSession(context.Context, string, *entities.RunServerRequest, []byte) (entities.Session, error) {
	return nil, nil
}
func (m *mockSessionManager) GetSession(string) entities.Session { return nil }
func (m *mockSessionManager) ListSessions(entities.SessionFilter) []entities.Session {
	return m.sessions
}
func (m *mockSessionManager) DeleteSession(id string) error {
	m.deletedIDs = append(m.deletedIDs, id)
	return nil
}
func (m *mockSessionManager) SendMessage(context.Context, string, string) error { return nil }
func (m *mockSessionManager) StopAgent(context.Context, string) error           { return nil }
func (m *mockSessionManager) GetMessages(context.Context, string) ([]portrepos.Message, error) {
	return nil, nil
}
func (m *mockSessionManager) Shutdown(time.Duration) error { return nil }

func testSession(id string, slack bool, lastMessageAt time.Time) entities.Session {
	tags := map[string]string{}
	if slack {
		tags["slackbot_id"] = "bot"
	}
	session := entities.NewProxySession(id, "user", entities.ScopeUser, "", tags, lastMessageAt.Add(-time.Hour))
	session.SetLastMessageAt(lastMessageAt)
	return session
}

func completedOneshotSession(id string, completedAt time.Time) entities.Session {
	session := entities.NewProxySessionWithStatus(id, "user", entities.ScopeUser, "", map[string]string{"oneshot": "true", "session_ttl": "1m"}, completedAt.Add(-time.Hour), "stopped")
	session.SetUpdatedAt(completedAt)
	return session
}

func TestPruneStaleSlackbotSessionsUsesControlSessionPort(t *testing.T) {
	mgr := &mockSessionManager{sessions: []entities.Session{testSession("stale", true, time.Now().Add(-100*time.Hour)), testSession("fresh", true, time.Now()), testSession("non-slack", false, time.Now().Add(-100*time.Hour))}}
	worker := NewCleanupWorker(mgr, CleanupWorkerConfig{SessionTTL: 72 * time.Hour})
	worker.pruneStaleSlackbotSessions(context.Background())
	if len(mgr.deletedIDs) != 1 || mgr.deletedIDs[0] != "stale" {
		t.Fatalf("deleted = %v", mgr.deletedIDs)
	}
}

func TestPruneStaleSlackbotSessionsDryRun(t *testing.T) {
	mgr := &mockSessionManager{sessions: []entities.Session{testSession("stale", true, time.Now().Add(-100*time.Hour))}}
	worker := NewCleanupWorker(mgr, CleanupWorkerConfig{SessionTTL: 72 * time.Hour, DryRun: true})
	worker.pruneStaleSlackbotSessions(context.Background())
	if len(mgr.deletedIDs) != 0 {
		t.Fatalf("dry-run deleted = %v", mgr.deletedIDs)
	}
}

func TestPruneSessionsWithTTLDeletesCompletedOneshotAfterOneMinute(t *testing.T) {
	now := time.Now()
	stale := completedOneshotSession("stale-oneshot", now.Add(-2*time.Minute))
	fresh := completedOneshotSession("fresh-oneshot", now.Add(-30*time.Second))
	running := entities.NewProxySessionWithStatus("running-oneshot", "user", entities.ScopeUser, "", map[string]string{"oneshot": "true"}, now.Add(-time.Hour), "running")
	regular := testSession("regular", false, now.Add(-100*time.Hour))
	mgr := &mockSessionManager{sessions: []entities.Session{stale, fresh, running, regular}}

	worker := NewCleanupWorker(mgr, CleanupWorkerConfig{SessionTTL: 72 * time.Hour})
	worker.pruneSessionsWithTTL(context.Background())

	if len(mgr.deletedIDs) != 1 || mgr.deletedIDs[0] != "stale-oneshot" {
		t.Fatalf("deleted = %v", mgr.deletedIDs)
	}
}

func TestPruneSessionsWithTTLExplicitTTLOverridesOneshotDefault(t *testing.T) {
	session := completedOneshotSession("explicit-ttl", time.Now().Add(-2*time.Hour))
	session.Tags()["session_ttl"] = "1h"
	mgr := &mockSessionManager{sessions: []entities.Session{session}}

	worker := NewCleanupWorker(mgr, CleanupWorkerConfig{SessionTTL: 72 * time.Hour})
	worker.pruneSessionsWithTTL(context.Background())

	if len(mgr.deletedIDs) != 1 || mgr.deletedIDs[0] != "explicit-ttl" {
		t.Fatalf("deleted = %v", mgr.deletedIDs)
	}
}
