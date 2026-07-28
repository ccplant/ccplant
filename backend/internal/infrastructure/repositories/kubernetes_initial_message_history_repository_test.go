package repositories

import (
	"context"
	"fmt"
	"testing"

	"k8s.io/client-go/kubernetes/fake"
)

func TestKubernetesInitialMessageHistoryRepositoryUpsertDeduplicatesAndTrims(t *testing.T) {
	repo := NewKubernetesInitialMessageHistoryRepository(fake.NewSimpleClientset(), "test")
	ctx := context.Background()

	for i := 0; i < 41; i++ {
		if _, err := repo.UpsertAndTrim(ctx, "alice", fmt.Sprintf("message-%02d", i), 40); err != nil {
			t.Fatalf("UpsertAndTrim(%d): %v", i, err)
		}
	}

	items, err := repo.List(ctx, "alice", 40)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(items) != 40 {
		t.Fatalf("got %d items, want 40", len(items))
	}
	if items[0].Content != "message-40" || items[39].Content != "message-01" {
		t.Fatalf("unexpected trimmed order: first=%q last=%q", items[0].Content, items[39].Content)
	}

	originalID := items[9].ID
	if _, err := repo.UpsertAndTrim(ctx, "alice", "message-31", 40); err != nil {
		t.Fatalf("deduplicating UpsertAndTrim: %v", err)
	}
	items, err = repo.List(ctx, "alice", 40)
	if err != nil {
		t.Fatalf("List after deduplication: %v", err)
	}
	if len(items) != 40 || items[0].Content != "message-31" || items[0].ID != originalID {
		t.Fatalf("message was not moved to front in place: %#v", items[0])
	}
}

func TestKubernetesInitialMessageHistoryRepositoryIsUserScopedAndDeleteIsIdempotent(t *testing.T) {
	repo := NewKubernetesInitialMessageHistoryRepository(fake.NewSimpleClientset(), "test")
	ctx := context.Background()

	if _, err := repo.UpsertAndTrim(ctx, "alice", "alice message", 40); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.UpsertAndTrim(ctx, "bob", "bob message", 40); err != nil {
		t.Fatal(err)
	}
	if err := repo.DeleteAll(ctx, "alice"); err != nil {
		t.Fatal(err)
	}
	if err := repo.DeleteAll(ctx, "alice"); err != nil {
		t.Fatalf("second delete should be idempotent: %v", err)
	}

	alice, err := repo.List(ctx, "alice", 40)
	if err != nil || len(alice) != 0 {
		t.Fatalf("alice history = %#v, err=%v", alice, err)
	}
	bob, err := repo.List(ctx, "bob", 40)
	if err != nil || len(bob) != 1 || bob[0].Content != "bob message" {
		t.Fatalf("bob history = %#v, err=%v", bob, err)
	}
}
