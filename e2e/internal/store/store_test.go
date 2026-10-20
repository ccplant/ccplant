package store

import (
	"context"
	"github.com/ccplant/ccplant/e2e/internal/domain"
	"testing"
	"time"
)

func TestRunLifecycle(t *testing.T) {
	s, err := Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	ctx := context.Background()
	r := domain.Run{ID: "r1", ScenarioID: "api.smoke.health", ScenarioDescription: "APIが応答する", CategoryID: "smoke", CategoryDescription: "スモークテスト", Kind: "go-test", Environment: "local", Parameters: map[string]any{}, Status: "queued", CreatedAt: time.Now().UTC()}
	if err := s.CreateRun(ctx, r); err != nil {
		t.Fatal(err)
	}
	claimed, err := s.Claim(ctx, "w1", time.Minute, []string{"go-test"})
	if err != nil || claimed.ID != "r1" {
		t.Fatalf("claim: %#v %v", claimed, err)
	}
	if err := s.Complete(ctx, "r1", "w1", "passed"); err != nil {
		t.Fatal(err)
	}
	got, err := s.GetRun(ctx, "r1")
	if err != nil || got.Status != "passed" {
		t.Fatalf("got: %#v %v", got, err)
	}
}
