package session

import (
	"context"
	"strings"
	"testing"

	"github.com/takutakahashi/agentapi-proxy/internal/domain/entities"
)

type initialMessageHistoryRepoStub struct {
	userID   string
	content  string
	maxItems int
	calls    int
}

func (r *initialMessageHistoryRepoStub) List(context.Context, string, int) ([]entities.InitialMessageHistoryItem, error) {
	return nil, nil
}
func (r *initialMessageHistoryRepoStub) UpsertAndTrim(_ context.Context, userID, content string, maxItems int) (entities.InitialMessageHistoryItem, error) {
	r.userID, r.content, r.maxItems, r.calls = userID, content, maxItems, r.calls+1
	return entities.InitialMessageHistoryItem{}, nil
}
func (r *initialMessageHistoryRepoStub) DeleteAll(context.Context, string) error { return nil }

func TestInitialMessageHistoryServiceRecord(t *testing.T) {
	repo := &initialMessageHistoryRepoStub{}
	service := NewInitialMessageHistoryService(repo)

	if err := service.Record(context.Background(), "alice", "  build it  "); err != nil {
		t.Fatal(err)
	}
	if repo.calls != 1 || repo.userID != "alice" || repo.content != "build it" || repo.maxItems != 40 {
		t.Fatalf("unexpected repository call: %#v", repo)
	}
	if err := service.Record(context.Background(), "alice", " \n "); err != nil {
		t.Fatal(err)
	}
	if repo.calls != 1 {
		t.Fatalf("blank content should not be recorded, calls=%d", repo.calls)
	}
	if err := service.Record(context.Background(), "alice", strings.Repeat("x", InitialMessageHistoryMaxContentBytes+1)); err == nil {
		t.Fatal("oversized content should be rejected")
	}
	if repo.calls != 1 {
		t.Fatalf("oversized content should not be recorded, calls=%d", repo.calls)
	}
}
