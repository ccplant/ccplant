package services

import (
	"context"
	"net/http"
	"testing"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func (f *snapshotMarkerFixture) setSuspendAt(t *testing.T, at time.Time) {
	t.Helper()
	svc, err := f.client.CoreV1().Services("test-ns").Get(context.Background(), "agentapi-session-runner-1-svc", metav1.GetOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if svc.Annotations == nil {
		svc.Annotations = map[string]string{}
	}
	svc.Annotations[sessionSuspendAtAnnotation] = at.UTC().Format(time.RFC3339Nano)
	svc.Annotations[sessionAutoSuspendEnabledAnnotation] = "true"
	if _, err := f.client.CoreV1().Services("test-ns").Update(context.Background(), svc, metav1.UpdateOptions{}); err != nil {
		t.Fatal(err)
	}
}

func (f *snapshotMarkerFixture) suspendAt(t *testing.T) time.Time {
	t.Helper()
	svc, err := f.client.CoreV1().Services("test-ns").Get(context.Background(), "agentapi-session-runner-1-svc", metav1.GetOptions{})
	if err != nil {
		t.Fatal(err)
	}
	at, err := time.Parse(time.RFC3339Nano, svc.Annotations[sessionSuspendAtAnnotation])
	if err != nil {
		t.Fatalf("suspend-at=%q: %v", svc.Annotations[sessionSuspendAtAnnotation], err)
	}
	return at
}

func TestIdleSuspendPostponesWhileSetupInProgress(t *testing.T) {
	fixture := newSnapshotMarkerFixture(t, http.StatusTooEarly)
	fixture.setSuspendAt(t, time.Now().Add(-time.Minute))

	fixture.manager.reconcileSessionSuspends(context.Background())

	if !fixture.deploymentExists(t) {
		t.Fatal("suspend removed the workload during setup")
	}
	if at := fixture.suspendAt(t); !at.After(time.Now().Add(30 * time.Minute)) {
		t.Fatalf("suspend-at=%s, want postponed by the idle timeout", at)
	}
	if got := fixture.session.Status(); got != "active" {
		t.Fatalf("status=%q, want active", got)
	}
}

func TestIdleSuspendKeepsFailedSetupAsError(t *testing.T) {
	fixture := newSnapshotMarkerFixture(t, http.StatusFailedDependency)
	fixture.setSuspendAt(t, time.Now().Add(-time.Minute))

	fixture.manager.reconcileSessionSuspends(context.Background())

	if !fixture.deploymentExists(t) {
		t.Fatal("suspend removed the workload of a failed setup")
	}
	if got := fixture.session.Status(); got != "error" {
		t.Fatalf("status=%q, want error", got)
	}
	if got := fixture.stored(t).Session.ResumeFrom; got != "" {
		t.Fatalf("resume_from=%q, want unchanged", got)
	}
	if at := fixture.suspendAt(t); !at.After(time.Now()) {
		t.Fatalf("suspend-at=%s, want no immediate retry", at)
	}
}

func TestIdleSuspendCancelledWhenSessionStartsDuringCheckpoint(t *testing.T) {
	fixture := newSnapshotMarkerFixtureWithCheckpoint(t, func(f *snapshotMarkerFixture) int {
		f.session.SetStatus("running")
		return http.StatusNoContent
	})
	fixture.setSuspendAt(t, time.Now().Add(-time.Minute))

	fixture.manager.reconcileSessionSuspends(context.Background())

	if !fixture.deploymentExists(t) {
		t.Fatal("suspend removed the workload of a running session")
	}
	if got := fixture.session.Status(); got != "running" {
		t.Fatalf("status=%q, want running", got)
	}
}

func TestIdleSuspendCancelledWhenDeadlineMovesDuringCheckpoint(t *testing.T) {
	fixture := newSnapshotMarkerFixtureWithCheckpoint(t, func(f *snapshotMarkerFixture) int {
		f.setSuspendAt(t, time.Now().Add(5*time.Minute))
		return http.StatusNoContent
	})
	fixture.setSuspendAt(t, time.Now().Add(-time.Minute))

	fixture.manager.reconcileSessionSuspends(context.Background())

	if !fixture.deploymentExists(t) {
		t.Fatal("suspend ignored the postponed deadline")
	}
	svc, err := fixture.client.CoreV1().Services("test-ns").Get(context.Background(), "agentapi-session-runner-1-svc", metav1.GetOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if svc.Annotations[sessionSuspendOwnerAnnotation] != "" {
		t.Fatalf("suspend ownership was not released: %#v", svc.Annotations)
	}
}

func TestExplicitSuspendIgnoresIdleDeadline(t *testing.T) {
	fixture := newSnapshotMarkerFixture(t, http.StatusNoContent)
	fixture.setSuspendAt(t, time.Now().Add(time.Hour))

	if err := fixture.manager.SuspendSession(context.Background(), "runner-1"); err != nil {
		t.Fatal(err)
	}
	if fixture.deploymentExists(t) {
		t.Fatal("explicit suspend kept the workload")
	}
}
