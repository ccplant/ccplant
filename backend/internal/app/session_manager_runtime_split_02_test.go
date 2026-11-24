package app

import (
	"context"
	"errors"
	"testing"

	coreallocation "github.com/takutakahashi/agentapi-proxy/internal/core/sessionallocation"
)

func TestLocalAllocationClientCompleteKeepsAllocationWhenRouteSaveFails(t *testing.T) {
	queue := &durableAllocationQueue{allocation: &coreallocation.AllocationRequest{SessionID: "public-id"}}
	routes := &recordingSessionRouteRepository{saveErr: errors.New("route unavailable")}
	client := newLocalAllocationClient(queue, routes)

	err := client.Complete(context.Background(), "public-id", coreallocation.AllocationResult{
		Status:             coreallocation.StatusAssigned,
		AllocatedSessionID: "runtime-id",
	})
	if err == nil {
		t.Fatal("Complete() error = nil, want route save error")
	}
	if queue.completed {
		t.Fatal("Complete() acknowledged allocation after route save failure")
	}
}

func TestLocalAllocationClientCompleteFailsWhenDurableAllocationIsMissing(t *testing.T) {
	queue := &durableAllocationQueue{getErr: errors.New("not found")}
	client := newLocalAllocationClient(queue, &recordingSessionRouteRepository{})

	err := client.Complete(context.Background(), "public-id", coreallocation.AllocationResult{
		Status:             coreallocation.StatusAssigned,
		AllocatedSessionID: "runtime-id",
	})
	if err == nil {
		t.Fatal("Complete() error = nil, want durable allocation error")
	}
	if queue.completed {
		t.Fatal("Complete() acknowledged missing allocation")
	}
}
