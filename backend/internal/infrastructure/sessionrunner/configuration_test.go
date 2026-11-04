package sessionrunner

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	core "github.com/takutakahashi/agentapi-proxy/internal/core/sessionrunner"
	"github.com/takutakahashi/agentapi-proxy/internal/infrastructure/kvstore"
	"k8s.io/client-go/kubernetes/fake"
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

func TestListConfigurationRefs(t *testing.T) {
	backends := map[string]func(*testing.T) *Store{
		"memory": newVersionedTestStore,
		"kubernetes": func(*testing.T) *Store {
			return NewStore(kvstore.NewKubernetesStore(fake.NewSimpleClientset()), "test")
		},
	}
	for name, newStore := range backends {
		t.Run(name, func(t *testing.T) {
			ctx := context.Background()
			store := newStore(t)
			for _, id := range []string{"owned", "orphan"} {
				if err := store.CreateConfiguration(ctx, &core.Configuration{SessionID: id, Input: []byte(`{"secret":"x"}`)}); err != nil {
					t.Fatal(err)
				}
			}
			owner := core.OwnerReference{APIVersion: "v1", Kind: "Service", Name: "agentapi-session-owned-svc", UID: "uid"}
			if err := store.SetConfigurationOwnerReference(ctx, "owned", owner); err != nil {
				t.Fatal(err)
			}
			// Other session runner resources must not be mistaken for configurations.
			if err := store.CreateRunner(ctx, &core.Runner{ID: "runner", Pool: "pool"}); err != nil {
				t.Fatal(err)
			}

			refs, err := store.ListConfigurationRefs(ctx)
			if err != nil {
				t.Fatal(err)
			}
			got := map[string]core.ConfigurationRef{}
			for _, ref := range refs {
				got[ref.SessionID] = ref
			}
			if len(got) != 2 || !got["owned"].HasOwner || got["orphan"].HasOwner {
				t.Fatalf("refs = %#v, want owned (with owner) and orphan (without)", refs)
			}
			if got["orphan"].UpdatedAt.IsZero() {
				t.Fatal("configuration ref is missing its update time")
			}
		})
	}
}

func TestConfigurationHasSessionIdentityLabel(t *testing.T) {
	ctx := context.Background()
	store := newVersionedTestStore(t)
	if err := store.CreateConfiguration(ctx, &core.Configuration{SessionID: "one", Input: []byte(`{}`)}); err != nil {
		t.Fatal(err)
	}
	record, err := store.kv.Get(ctx, kvstore.KindSecret, "test", configurationName("one"))
	if err != nil {
		t.Fatal(err)
	}
	if got := record.Labels["agentapi.proxy/session-id"]; got != "one" {
		t.Fatalf("session identity label = %q, want one", got)
	}
}
