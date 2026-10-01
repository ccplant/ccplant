package kvstore

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"testing"

	"github.com/takutakahashi/agentapi-proxy/pkg/authzscope"
)

type unwrapCountingKeyring struct {
	EnvelopeKeyring
	unwraps int
}

func (k *unwrapCountingKeyring) UnwrapDataKey(ctx context.Context, keyID string, wrapped []byte, record Record) ([]byte, error) {
	k.unwraps++
	return k.EnvelopeKeyring.UnwrapDataKey(ctx, keyID, wrapped, record)
}

func TestEncryptedStoreRejectsUserSwapBeforeKeyUnwrap(t *testing.T) {
	backend := newMemoryStore()
	base, err := NewLocalKeyring("current", map[string]string{"current": randomEncodedKey(t)})
	if err != nil {
		t.Fatal(err)
	}
	keyring := &unwrapCountingKeyring{EnvelopeKeyring: base}
	store, err := NewEncryptedStore(backend, keyring)
	if err != nil {
		t.Fatal(err)
	}
	labels := map[string]string{"resource": "settings", "agentapi.proxy/settings-name": "alice"}
	value := secretDocument(t, "alice", labels, "alice-secret")
	created, err := store.Create(context.Background(), Record{Kind: KindSecret, Namespace: "ns", Key: "alice", Labels: labels, Value: value})
	if err != nil {
		t.Fatal(err)
	}

	bob := authzscope.WithPrincipal(context.Background(), authzscope.Principal{ActorID: "bob", UserID: "bob"})
	if _, err := store.Get(bob, KindSecret, "ns", "alice"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("cross-user Get error = %v, want not found", err)
	}
	if keyring.unwraps != 0 {
		t.Fatalf("unauthorized Get unwrapped %d keys", keyring.unwraps)
	}
	if err := store.Delete(bob, KindSecret, "ns", "alice", created.Version); !errors.Is(err, ErrNotFound) {
		t.Fatalf("cross-user Delete error = %v, want not found", err)
	}
	bobLabels := map[string]string{"resource": "settings", "agentapi.proxy/settings-name": "bob"}
	bobValue := secretDocument(t, "alice", bobLabels, "replacement")
	if _, err := store.Update(bob, Record{Kind: KindSecret, Namespace: "ns", Key: "alice", Labels: bobLabels, Value: bobValue, Version: created.Version}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("cross-user Update error = %v, want not found", err)
	}
	records, err := store.List(bob, Query{Kind: KindSecret, Namespace: "ns", LabelSelector: "resource=settings"})
	if err != nil {
		t.Fatal(err)
	} else if len(records) != 0 {
		t.Fatal("cross-user List returned an owned record")
	}

	alice := authzscope.WithPrincipal(context.Background(), authzscope.Principal{ActorID: "alice", UserID: "alice"})
	if _, err := store.Get(alice, KindSecret, "ns", "alice"); err != nil {
		t.Fatalf("owner Get: %v", err)
	}
	if keyring.unwraps != 1 {
		t.Fatalf("owner Get unwraps = %d, want 1", keyring.unwraps)
	}
}

func TestEncryptedStoreRejectsTeamSwap(t *testing.T) {
	backend := newMemoryStore()
	store := encryptedTestStore(t, backend, "current", map[string]string{"current": randomEncodedKey(t)})
	labels := map[string]string{
		"resource": "profile", "agentapi.proxy/session-profile-scope": "team",
		"agentapi.proxy/session-profile-user-id":      "creator",
		"agentapi.proxy/session-profile-team-id-hash": hashIdentity("org/red")[:63],
	}
	value := secretDocument(t, "profile", labels, "team-secret")
	if _, err := store.Create(context.Background(), Record{Kind: KindSecret, Namespace: "ns", Key: "profile", Labels: labels, Value: value}); err != nil {
		t.Fatal(err)
	}
	red := authzscope.WithPrincipal(context.Background(), authzscope.Principal{ActorID: "member", UserID: "member", TeamIDs: []string{"org/red"}})
	if _, err := store.Get(red, KindSecret, "ns", "profile"); err != nil {
		t.Fatalf("team member Get: %v", err)
	}
	blue := authzscope.WithPrincipal(context.Background(), authzscope.Principal{ActorID: "member", UserID: "creator", TeamIDs: []string{"org/blue"}})
	if _, err := store.Get(blue, KindSecret, "ns", "profile"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("other-team Get error = %v, want not found", err)
	}
}

func hashIdentity(value string) string {
	digest := sha256.Sum256([]byte(value))
	return hex.EncodeToString(digest[:])
}
