package provisioner

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/takutakahashi/agentapi-proxy/pkg/sessionsettings"
)

func TestRestoreObjectStorages(t *testing.T) {
	content := []byte("shared value")
	digestBytes := sha256.Sum256(content)
	digest := hex.EncodeToString(digestBytes[:])
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		require.Equal(t, "Bearer secret", request.Header.Get("Authorization"))
		require.Equal(t, "/storages/storage-1/files/dir/value.txt", request.URL.Path)
		_, _ = w.Write(content)
	}))
	defer server.Close()

	mount := t.TempDir()
	settings := &sessionsettings.SessionSettings{
		Env:           map[string]string{"AGENTAPI_KEY": "secret"},
		ParentRuntime: &sessionsettings.ParentRuntimeConfig{Endpoint: server.URL},
		Storages: []sessionsettings.StorageAttachment{{
			StorageID: "storage-1", MountPath: mount, Kind: "object_snapshot",
			Files: []sessionsettings.StorageFile{{Path: "dir/value.txt", Digest: digest, Mode: 0o600}},
		}},
	}
	require.NoError(t, restoreObjectStorages(context.Background(), settings))
	data, err := os.ReadFile(filepath.Join(mount, "dir", "value.txt"))
	require.NoError(t, err)
	require.Equal(t, content, data)
	info, err := os.Stat(filepath.Join(mount, "dir", "value.txt"))
	require.NoError(t, err)
	require.Equal(t, os.FileMode(0o600), info.Mode().Perm())
}

func TestSafeStorageTargetRejectsTraversal(t *testing.T) {
	root := t.TempDir()
	_, err := safeStorageTarget(root, "../outside")
	require.Error(t, err)
	target, err := safeStorageTarget(root, "dir/file")
	require.NoError(t, err)
	require.Equal(t, filepath.Join(root, "dir", "file"), target)
}
