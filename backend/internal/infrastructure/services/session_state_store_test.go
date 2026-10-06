package services

import (
	"bytes"
	"context"
	"io"
	"os"
	"strings"
	"testing"

	"github.com/takutakahashi/agentapi-proxy/pkg/config"
)

func TestVolumeSessionStateStore(t *testing.T) {
	store, err := newVolumeSessionStateStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	want := []byte("snapshot")
	if err := store.Save(ctx, "session-1", bytes.NewReader(want)); err != nil {
		t.Fatal(err)
	}
	got, err := store.Load(ctx, "session-1")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = got.Close() }()
	gotBytes, err := io.ReadAll(got)
	if err != nil {
		t.Fatal(err)
	}
	if string(gotBytes) != string(want) {
		t.Fatalf("got %q", gotBytes)
	}
	if err := store.Save(ctx, "../escape", bytes.NewReader(want)); err == nil {
		t.Fatal("expected invalid id error")
	}
}

func TestVolumeSessionStateStoreDelete(t *testing.T) {
	store, err := newVolumeSessionStateStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Save(context.Background(), "tpl_one", strings.NewReader("snapshot")); err != nil {
		t.Fatal(err)
	}
	deleter := store.(SessionStateDeleter)
	if err := deleter.Delete(context.Background(), "tpl_one"); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Load(context.Background(), "tpl_one"); !os.IsNotExist(err) {
		t.Fatalf("Load error = %v, want not exist", err)
	}
}

func TestVolumeSessionStateStoreIsOwnedBySessionPod(t *testing.T) {
	path := t.TempDir() + "/manager-state"
	store, err := NewSessionStateStore(context.Background(), config.SessionPersistenceConfig{Backend: "volume", Path: path})
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := store.(podVolumeSessionStateStore); !ok {
		t.Fatalf("store type = %T, want podVolumeSessionStateStore", store)
	}
}
