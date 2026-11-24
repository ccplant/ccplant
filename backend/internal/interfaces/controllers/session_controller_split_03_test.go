package controllers

import (
	"testing"

	"github.com/takutakahashi/agentapi-proxy/internal/domain/entities"
)

func TestFindUncreatedSessionAllocation(t *testing.T) {
	pending := &sessionListTestSession{id: "pending-id", status: "pending"}
	sessions := []entities.Session{
		&sessionListTestSession{id: "running-id", status: "running"},
		pending,
		&sessionListTestSession{id: "allocating-id", status: "allocating"},
	}

	if got := findUncreatedSessionAllocation(sessions, "pending-id"); got != pending {
		t.Fatalf("findUncreatedSessionAllocation() = %v, want pending session", got)
	}
	if got := findUncreatedSessionAllocation(sessions, "running-id"); got != nil {
		t.Fatalf("findUncreatedSessionAllocation() returned running session %v", got)
	}
	if got := findUncreatedSessionAllocation(sessions, "allocating-id"); got != sessions[2] {
		t.Fatalf("findUncreatedSessionAllocation() = %v, want allocating session", got)
	}
}
