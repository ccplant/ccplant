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
	session := entities.NewProxySessionWithStatus(id, "user", entities.ScopeUser, "", tags, lastMessageAt.Add(-time.Hour), "active")
	session.SetLastMessageAt(lastMessageAt)
	session.SetUpdatedAt(lastMessageAt)
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

func TestOneshotCleanupRegardlessOfOriginAndExplicitTTL(t *testing.T) {
	for _, slack := range []bool{false, true} {
		for _, explicitTTL := range []bool{false, true} {
			stale := completedOneshotSession("stale", time.Now().Add(-2*time.Minute))
			fresh := completedOneshotSession("fresh", time.Now().Add(-30*time.Second))
			running := entities.NewProxySessionWithStatus("running", "user", entities.ScopeUser, "", map[string]string{"oneshot": "true", "session_ttl": "1m"}, time.Now().Add(-100*time.Hour), "running")
			for _, session := range []entities.Session{stale, fresh, running} {
				if slack {
					session.Tags()["slackbot_id"] = "bot"
				}
				if !explicitTTL {
					delete(session.Tags(), "session_ttl")
				}
			}
			mgr := &mockSessionManager{sessions: []entities.Session{stale, fresh, running}}
			worker := NewCleanupWorker(mgr, CleanupWorkerConfig{SessionTTL: 72 * time.Hour})
			worker.pruneStaleSlackbotSessions(context.Background())
			worker.pruneSessionsWithTTL(context.Background())
			if len(mgr.deletedIDs) != 1 || mgr.deletedIDs[0] != "stale" {
				t.Fatalf("slack=%v explicitTTL=%v: deleted=%v, want [stale]", slack, explicitTTL, mgr.deletedIDs)
			}
		}
	}
}

func TestSessionTTLStartsAfterProcessingEnds(t *testing.T) {
	for _, source := range []struct {
		name string
		tags map[string]string
	}{
		{"slack-global", map[string]string{"slackbot_id": "bot"}},
		{"slack-explicit", map[string]string{"slackbot_id": "bot", "session_ttl": "1h"}},
		{"session-explicit", map[string]string{"session_ttl": "1h"}},
	} {
		t.Run(source.name, func(t *testing.T) {
			now := time.Now()
			for _, tc := range []struct {
				name       string
				status     string
				updatedAt  time.Time
				wantDelete bool
			}{
				{"long-running", "running", now.Add(-100 * time.Hour), false},
				{"starting", "starting", now.Add(-100 * time.Hour), false},
				{"pending", "pending", now.Add(-100 * time.Hour), false},
				{"resuming", "resuming", now.Add(-100 * time.Hour), false},
				{"unknown-state", "unknown", now.Add(-100 * time.Hour), false},
				{"unhealthy-is-not-completion", "unhealthy", now.Add(-100 * time.Hour), false},
				{"just-finished-long-turn", "active", now.Add(-30 * time.Minute), false},
				{"idle-past-ttl", "active", now.Add(-100 * time.Hour), true},
				{"explicit-ttl-overrides-global", "active", now.Add(-2 * time.Hour), source.name != "slack-global"},
				{"recently-stopped", "stopped", now.Add(-30 * time.Minute), false},
				{"stopped-past-ttl", "stopped", now.Add(-100 * time.Hour), true},
				{"recently-suspended", "suspended", now.Add(-30 * time.Minute), false},
				{"suspended-past-ttl", "suspended", now.Add(-100 * time.Hour), true},
				{"error-past-ttl", "error", now.Add(-100 * time.Hour), true},
				{"timeout-past-ttl", "timeout", now.Add(-100 * time.Hour), true},
				{"missing-completion-time", "active", time.Time{}, false},
			} {
				t.Run(tc.name, func(t *testing.T) {
					session := entities.NewProxySessionWithStatus("session", "user", entities.ScopeUser, "", source.tags, now.Add(-200*time.Hour), tc.status)
					session.SetLastMessageAt(now.Add(-150 * time.Hour))
					session.SetUpdatedAt(tc.updatedAt)
					mgr := &mockSessionManager{sessions: []entities.Session{session}}
					worker := NewCleanupWorker(mgr, CleanupWorkerConfig{SessionTTL: 72 * time.Hour})
					worker.pruneStaleSlackbotSessions(context.Background())
					worker.pruneSessionsWithTTL(context.Background())
					if got := len(mgr.deletedIDs) == 1; got != tc.wantDelete {
						t.Fatalf("deleted=%v, wantDelete=%v", mgr.deletedIDs, tc.wantDelete)
					}
				})
			}
		})
	}
}

func TestSessionTTLRestartsAfterNextTurn(t *testing.T) {
	for _, slack := range []bool{false, true} {
		tags := map[string]string{"session_ttl": "1h"}
		if slack {
			tags["slackbot_id"] = "bot"
		}
		mgr := &mockSessionManager{}
		worker := NewCleanupWorker(mgr, CleanupWorkerConfig{SessionTTL: 72 * time.Hour})
		now := time.Now()
		for _, phase := range []struct {
			status     string
			updatedAt  time.Time
			wantDelete bool
		}{
			{"active", now.Add(-30 * time.Minute), false},
			{"running", now.Add(-2 * time.Hour), false},
			{"active", now.Add(-30 * time.Minute), false},
			{"active", now.Add(-2 * time.Hour), true},
		} {
			session := entities.NewProxySessionWithStatus("session", "user", entities.ScopeUser, "", tags, now.Add(-100*time.Hour), phase.status)
			session.SetUpdatedAt(phase.updatedAt)
			mgr.sessions = []entities.Session{session}
			worker.pruneStaleSlackbotSessions(context.Background())
			worker.pruneSessionsWithTTL(context.Background())
			if got := len(mgr.deletedIDs) == 1; got != phase.wantDelete {
				t.Fatalf("slack=%v phase=%+v: deleted=%v", slack, phase, mgr.deletedIDs)
			}
		}
	}
}

func TestExplicitSessionTTLDryRunAndMissingCompletion(t *testing.T) {
	for _, dryRun := range []bool{false, true} {
		completed := completedOneshotSession("completed", time.Now().Add(-2*time.Hour))
		missing := completedOneshotSession("missing", time.Time{})
		interactive := testSession("interactive", false, time.Now().Add(-100*time.Hour))
		interactive.Tags()["session_ttl"] = "1h"
		mgr := &mockSessionManager{sessions: []entities.Session{completed, missing, interactive}}
		worker := NewCleanupWorker(mgr, CleanupWorkerConfig{DryRun: dryRun})
		worker.pruneSessionsWithTTL(context.Background())
		want := 2
		if dryRun {
			want = 0
		}
		if len(mgr.deletedIDs) != want {
			t.Fatalf("dryRun=%v: deleted=%v, want %d", dryRun, mgr.deletedIDs, want)
		}
	}
}
