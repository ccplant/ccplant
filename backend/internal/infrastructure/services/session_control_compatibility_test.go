package services

import (
	"context"
	"errors"
	"testing"
	"time"

	coresessioncontrol "github.com/takutakahashi/agentapi-proxy/internal/core/sessioncontrol"
)

type compatibilityControlStore struct {
	connected bool
	err       error
	block     bool
}

func (s *compatibilityControlStore) TouchConnection(context.Context, string) error { return s.err }
func (s *compatibilityControlStore) IsConnected(ctx context.Context, _ string) (bool, error) {
	if s.block {
		<-ctx.Done()
		return false, ctx.Err()
	}
	return s.connected, s.err
}
func (s *compatibilityControlStore) EnqueueCommand(context.Context, string, coresessioncontrol.Command) (string, error) {
	return "", nil
}

func TestConnectedSessionControlStoreTimesOutAfterOneSecond(t *testing.T) {
	store := &compatibilityControlStore{block: true}
	manager := &KubernetesSessionManager{sessionControlStore: store}

	started := time.Now()
	if got := manager.connectedSessionControlStore(context.Background(), "blocked-session"); got != nil {
		t.Fatal("timed out connection lookup must use direct transport")
	}
	elapsed := time.Since(started)
	if elapsed < sessionControlConnectionTimeout || elapsed > 2*sessionControlConnectionTimeout {
		t.Fatalf("connection lookup took %s, want between %s and %s", elapsed, sessionControlConnectionTimeout, 2*sessionControlConnectionTimeout)
	}
}
func (s *compatibilityControlStore) ReadCommands(context.Context, string, string, time.Duration, int64) ([]coresessioncontrol.Command, error) {
	return nil, nil
}
func (s *compatibilityControlStore) AckCommand(context.Context, string, string) error { return nil }
func (s *compatibilityControlStore) AppendEvents(context.Context, string, []coresessioncontrol.Event) (string, error) {
	return "", nil
}
func (s *compatibilityControlStore) ReadEvents(context.Context, string, string, time.Duration, int64) ([]coresessioncontrol.Event, error) {
	return nil, nil
}

func TestConnectedSessionControlStoreRequiresActiveLease(t *testing.T) {
	store := &compatibilityControlStore{}
	manager := &KubernetesSessionManager{sessionControlStore: store}
	if got := manager.connectedSessionControlStore(context.Background(), "old-session"); got != nil {
		t.Fatal("old session without a control lease must use direct transport")
	}

	store.connected = true
	if got := manager.connectedSessionControlStore(context.Background(), "new-session"); got != store {
		t.Fatal("session with an active control lease must use long polling")
	}

	store.err = errors.New("redis unavailable")
	if got := manager.connectedSessionControlStore(context.Background(), "new-session"); got != nil {
		t.Fatal("connection lookup failure must fall back to direct transport")
	}
}
