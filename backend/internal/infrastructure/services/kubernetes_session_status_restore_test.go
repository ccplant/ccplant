package services

import (
	"context"
	"testing"
	"time"

	"github.com/takutakahashi/agentapi-proxy/internal/domain/entities"
	portrepos "github.com/takutakahashi/agentapi-proxy/internal/usecases/ports/repositories"
)

type fixedStatusRepository struct {
	status    string
	updatedAt time.Time
}

func (r fixedStatusRepository) SetStatus(context.Context, string, string, string, time.Time) (bool, error) {
	return true, nil
}
func (r fixedStatusRepository) GetStatus(context.Context, string) (string, time.Time, error) {
	return r.status, r.updatedAt, nil
}
func (r fixedStatusRepository) PublishStatusChange(context.Context, portrepos.StatusChangeEvent) error {
	return nil
}
func (r fixedStatusRepository) SubscribeGlobal(context.Context) (<-chan portrepos.StatusChangeEvent, error) {
	return make(chan portrepos.StatusChangeEvent), nil
}
func (r fixedStatusRepository) DeleteStatus(context.Context, string) error { return nil }

func newStatusRestoreTestSession(status string, updatedAt time.Time) *KubernetesSession {
	session := NewKubernetesSession(
		"session-1",
		&entities.RunServerRequest{UserID: "user-1"},
		"deployment-1", "service-1", "pvc-1", "namespace-1", 9000, nil, nil,
	)
	session.SetStatusSilentAt(status, updatedAt)
	return session
}

func TestRuntimeStatusOverrideFromRedisUsesNewerRuntimeStatusForRestoredStartingSession(t *testing.T) {
	workloadTime := time.Now().Add(-time.Minute)
	runtimeTime := workloadTime.Add(30 * time.Second)
	session := newStatusRestoreTestSession("starting", workloadTime)

	runtimeStatusOverrideFromRedis(fixedStatusRepository{status: "active", updatedAt: runtimeTime}, session)

	if got := session.Status(); got != "active" {
		t.Fatalf("status = %q, want active", got)
	}
	if got := session.UpdatedAt(); !got.Equal(runtimeTime) {
		t.Fatalf("updated_at = %v, want %v", got, runtimeTime)
	}
}

func TestRuntimeStatusOverrideFromRedisKeepsNewerWorkloadRestart(t *testing.T) {
	runtimeTime := time.Now().Add(-time.Minute)
	workloadTime := runtimeTime.Add(30 * time.Second)
	session := newStatusRestoreTestSession("starting", workloadTime)

	runtimeStatusOverrideFromRedis(fixedStatusRepository{status: "active", updatedAt: runtimeTime}, session)

	if got := session.Status(); got != "starting" {
		t.Fatalf("status = %q, want starting", got)
	}
}

func TestSetStatusSilentAtRejectsOutOfOrderAndConflictingEvents(t *testing.T) {
	newest := time.Now()
	session := newStatusRestoreTestSession("running", newest)

	if session.SetStatusSilentAt("active", newest.Add(-time.Second)) {
		t.Fatal("older event was accepted")
	}
	if got := session.Status(); got != "running" {
		t.Fatalf("older event changed status to %q", got)
	}
	if session.SetStatusSilentAt("active", newest) {
		t.Fatal("conflicting event with the same timestamp was accepted")
	}
	if got := session.Status(); got != "running" {
		t.Fatalf("conflicting event changed status to %q", got)
	}
	if !session.SetStatusSilentAt("active", newest.Add(time.Second)) {
		t.Fatal("newer event was not accepted")
	}
}
