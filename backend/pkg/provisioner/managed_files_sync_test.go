package provisioner

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/takutakahashi/agentapi-proxy/pkg/sessionsettings"
)

func TestSaveManagedFilesUsesSessionControlToken(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, http.MethodPut, r.Method)
		require.Equal(t, "/internal/session-control/session-1/managed-files", r.URL.Path)
		require.Equal(t, "Bearer token-1", r.Header.Get("Authorization"))
		require.Equal(t, "token-1", r.Header.Get("X-Session-Control-Token"))
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()
	require.NoError(t, saveManagedFiles(context.Background(), server.Client(), server.URL, "session-1", "token-1", []sessionsettings.ManagedFile{{Path: sessionsettings.ManagedFileTypes[sessionsettings.FileTypeCodexAuth], Content: "updated"}}))
}

func TestRunFilesSyncPrefersLocalSessionControlProxy(t *testing.T) {
	t.Setenv("PROVISIONER_PROXY_URL", "http://parent.invalid")
	local := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/internal/session-control/session-1/managed-files" {
			t.Fatalf("path = %q", r.URL.Path)
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer local.Close()
	t.Setenv("PROVISIONER_LOCAL_PROXY_URL", local.URL)
	t.Setenv("AGENTAPI_SESSION_ID", "session-1")
	t.Setenv("SESSION_CONTROL_TOKEN", "token")

	require.Equal(t, local.URL, sessionControlProxyURL())
}
