package kvstore

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestLibSQLApplyAtomicallyRollsBackEveryRecordOnConflict(t *testing.T) {
	ctx := context.Background()
	store, err := NewLibSQLStore(ctx, "file://"+filepath.Join(t.TempDir(), "atomic.db"), "")
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, store.Close()) })

	first, err := store.Create(ctx, Record{Kind: KindConfigMap, Namespace: "test", Key: "first", Value: []byte("one")})
	require.NoError(t, err)
	second, err := store.Create(ctx, Record{Kind: KindConfigMap, Namespace: "test", Key: "second", Value: []byte("two")})
	require.NoError(t, err)

	first.Value = []byte("changed")
	second.Value = []byte("stale")
	second.Version++
	_, err = store.ApplyAtomically(ctx, []Mutation{
		{Type: MutationUpdate, Record: first},
		{Type: MutationUpdate, Record: second},
	})
	require.ErrorIs(t, err, ErrConflict)

	loaded, err := store.Get(ctx, first.Kind, first.Namespace, first.Key)
	require.NoError(t, err)
	require.Equal(t, []byte("one"), loaded.Value)
	require.Equal(t, int64(1), loaded.Version)
}

func TestLibSQLApplyAtomicallyCreatesUpdatesAndDeletesTogether(t *testing.T) {
	ctx := context.Background()
	store, err := NewLibSQLStore(ctx, "file://"+filepath.Join(t.TempDir(), "mutations.db"), "")
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, store.Close()) })

	revision, err := store.Create(ctx, Record{Kind: KindConfigMap, Namespace: "test", Key: "revision", Value: []byte("v0")})
	require.NoError(t, err)
	revision.Value = []byte("v1")
	created := Record{Kind: KindSecret, Namespace: "test", Key: "profile", Labels: map[string]string{"agentapi.proxy/session-profile": "true"}, Value: []byte("profile")}
	result, err := store.ApplyAtomically(ctx, []Mutation{
		{Type: MutationCreate, Record: created},
		{Type: MutationUpdate, Record: revision},
	})
	require.NoError(t, err)
	require.Equal(t, int64(1), result[0].Version)
	require.Equal(t, int64(2), result[1].Version)

	revision = result[1]
	revision.Value = []byte("v2")
	_, err = store.ApplyAtomically(ctx, []Mutation{
		{Type: MutationDelete, Record: result[0]},
		{Type: MutationUpdate, Record: revision},
	})
	require.NoError(t, err)
	_, err = store.Get(ctx, created.Kind, created.Namespace, created.Key)
	require.True(t, errors.Is(err, ErrNotFound))
}
