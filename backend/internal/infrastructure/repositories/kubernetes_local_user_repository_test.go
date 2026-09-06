package repositories

import (
	"context"
	"errors"
	"testing"

	"github.com/takutakahashi/agentapi-proxy/internal/domain/entities"
	"k8s.io/client-go/kubernetes/fake"
)

func TestKubernetesLocalUserRepositoryCreateGetAndConflict(t *testing.T) {
	repo := NewKubernetesLocalUserRepository(fake.NewSimpleClientset(), "default")
	user, err := entities.NewLocalUser("alice", "Alice", "alice@example.com", entities.RoleUser, "admin")
	if err != nil {
		t.Fatal(err)
	}
	if err := repo.Create(context.Background(), user); err != nil {
		t.Fatal(err)
	}
	got, err := repo.GetByID(context.Background(), user.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Username != "alice" || got.DisplayName != "Alice" || got.ID != user.ID {
		t.Fatalf("unexpected user: %#v", got)
	}
	byUsername, err := repo.GetByUsername(context.Background(), "alice")
	if err != nil || byUsername.ID != user.ID {
		t.Fatalf("username lookup: %#v, %v", byUsername, err)
	}
	duplicate, err := entities.NewLocalUser("alice", "Another Alice", "", entities.RoleUser, "admin")
	if err != nil {
		t.Fatal(err)
	}
	if err := repo.Create(context.Background(), duplicate); !errors.Is(err, entities.ErrLocalUserAlreadyExists) {
		t.Fatalf("expected conflict, got %v", err)
	}
}

func TestKubernetesLocalUserRepositoryNotFound(t *testing.T) {
	repo := NewKubernetesLocalUserRepository(fake.NewSimpleClientset(), "default")
	_, err := repo.GetByID(context.Background(), "missing")
	if !errors.Is(err, entities.ErrLocalUserNotFound) {
		t.Fatalf("expected not found, got %v", err)
	}
}
