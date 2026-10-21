package entities

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestStorageApplyFilesUsesOneMonotonicBatchRevision(t *testing.T) {
	storage := NewStorage("storage-1", "shared", "", "alice", ScopeUser, "", StorageKindObjectSnapshot, "", "", "")

	revision := storage.ApplyFiles([]StorageFile{{Path: "a.txt", Digest: "a"}, {Path: "b.txt", Digest: "b"}})
	require.Equal(t, int64(1), revision)
	a, ok := storage.File("a.txt")
	require.True(t, ok)
	b, ok := storage.File("b.txt")
	require.True(t, ok)
	require.Equal(t, revision, a.Revision)
	require.Equal(t, revision, b.Revision)

	storage.ApplyFiles([]StorageFile{{Path: "a.txt", Deleted: true}})
	a, ok = storage.File("a.txt")
	require.True(t, ok)
	require.True(t, a.Deleted)
	require.Equal(t, int64(2), a.Revision)
	require.Equal(t, int64(1), b.Revision)
}

func TestValidateStoragePath(t *testing.T) {
	for _, valid := range []string{"file.txt", "dir/file.txt", ".config/value"} {
		require.NoError(t, ValidateStoragePath(valid), valid)
	}
	for _, invalid := range []string{"", "/etc/passwd", "../outside", "dir/../file", `dir\file`, ".ccplant-storage/state"} {
		require.Error(t, ValidateStoragePath(invalid), invalid)
	}
}

func TestStorageValidationRejectsUnknownKind(t *testing.T) {
	storage := NewStorage("storage-1", "shared", "", "alice", ScopeUser, "", StorageKind("other"), "", "", "")
	require.ErrorContains(t, storage.Validate(), "object_snapshot")
}
