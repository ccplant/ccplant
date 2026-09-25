package sessionrunner

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	core "github.com/takutakahashi/agentapi-proxy/internal/core/sessionrunner"
	"github.com/takutakahashi/agentapi-proxy/internal/infrastructure/kvstore"
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

func TestConfigurationOwnerReferenceIsPersisted(t *testing.T) {
	ctx := context.Background()
	store := newVersionedTestStore(t)
	if err := store.CreateConfiguration(ctx, &core.Configuration{SessionID: "one", Input: []byte(`{}`)}); err != nil {
		t.Fatal(err)
	}
	owner := core.OwnerReference{APIVersion: "v1", Kind: "Service", Name: "agentapi-session-one-svc", UID: "service-uid"}
	if err := store.SetConfigurationOwnerReference(ctx, "one", owner); err != nil {
		t.Fatal(err)
	}
	record, err := store.kv.Get(ctx, kvstore.KindSecret, "test", configurationName("one"))
	if err != nil {
		t.Fatal(err)
	}
	var doc secretDocument
	if err := json.Unmarshal(record.Value, &doc); err != nil {
		t.Fatal(err)
	}
	if len(doc.Metadata.OwnerReferences) != 1 || doc.Metadata.OwnerReferences[0] != owner {
		t.Fatalf("owner references = %#v, want %#v", doc.Metadata.OwnerReferences, owner)
	}
}

func TestDeleteConfiguration(t *testing.T) {
	ctx := context.Background()
	store := newVersionedTestStore(t)
	if err := store.CreateConfiguration(ctx, &core.Configuration{SessionID: "one", Input: []byte(`{}`)}); err != nil {
		t.Fatal(err)
	}
	if err := store.DeleteConfiguration(ctx, "one"); err != nil {
		t.Fatal(err)
	}
	if _, err := store.GetConfiguration(ctx, "one"); !errors.Is(err, core.ErrNotFound) {
		t.Fatalf("GetConfiguration() error = %v, want ErrNotFound", err)
	}
}
