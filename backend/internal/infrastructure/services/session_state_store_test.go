package services

import (
	"context"
	"testing"
)

func TestVolumeSessionStateStore(t *testing.T) {
	store, err := newVolumeSessionStateStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	want := []byte("snapshot")
	if err := store.Save(ctx, "session-1", want); err != nil {
		t.Fatal(err)
	}
	got, err := store.Load(ctx, "session-1")
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(want) {
		t.Fatalf("got %q", got)
	}
	if err := store.Save(ctx, "../escape", want); err == nil {
		t.Fatal("expected invalid id error")
	}
}
