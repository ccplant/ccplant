package sessionrunner

import (
	"context"
	"errors"
	core "github.com/takutakahashi/agentapi-proxy/internal/core/sessionrunner"
	"testing"
)

func TestConfigurationClaimsAreCompareAndSwap(t *testing.T) {
	ctx := context.Background()
	store := newVersionedTestStore(t)
	if err := store.CreateConfiguration(ctx, &core.Configuration{SessionID: "one", Input: []byte(`{"params":{"model":"explicit"}}`)}); err != nil {
		t.Fatal(err)
	}
	a, err := store.GetConfiguration(ctx, "one")
	if err != nil {
		t.Fatal(err)
	}
	b, err := store.GetConfiguration(ctx, "one")
	if err != nil {
		t.Fatal(err)
	}
	a.Phase = "restarting"
	a.RequestID = "first"
	if err := store.SaveConfiguration(ctx, a); err != nil {
		t.Fatal(err)
	}
	b.Phase = "restarting"
	b.RequestID = "second"
	if err := store.SaveConfiguration(ctx, b); !errors.Is(err, core.ErrConflict) {
		t.Fatalf("stale operation: %v", err)
	}
	got, err := store.GetConfiguration(ctx, "one")
	if err != nil {
		t.Fatal(err)
	}
	if got.RequestID != "first" {
		t.Fatal("stale write replaced operation")
	}
}
