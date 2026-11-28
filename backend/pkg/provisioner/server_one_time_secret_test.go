package provisioner

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/takutakahashi/agentapi-proxy/pkg/sessionsettings"
)

func TestHandleOneTimeSecretProxiesWithRuntimeCredential(t *testing.T) {
	t.Setenv("NETWORK_FILTER_CONTROL_SOCKET", "")
	parent := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "/internal/session-control/public-session/secrets/secret-1", r.URL.Path)
		require.Equal(t, "4", r.URL.Query().Get("generation"))
		require.Equal(t, "Bearer runtime-token", r.Header.Get("Authorization"))
		_, _ = w.Write([]byte(`{"value":"test-value"}`))
	}))
	defer parent.Close()

	s := New(9001, "")
	s.activeSettings = &sessionsettings.SessionSettings{ParentRuntime: &sessionsettings.ParentRuntimeConfig{
		Enabled: true, Endpoint: parent.URL, SessionID: "public-session", Token: "runtime-token", Generation: 4,
	}}
	req := httptest.NewRequest(http.MethodGet, "/one-time-secrets/secret-1", nil)
	req.RemoteAddr = "127.0.0.1:12345"
	rec := httptest.NewRecorder()
	s.handleOneTimeSecret(rec, req)
	require.Equal(t, http.StatusOK, rec.Code)
	require.JSONEq(t, `{"value":"test-value"}`, rec.Body.String())
	require.Equal(t, "no-store", rec.Header().Get("Cache-Control"))
}

func TestHandleSettingsSecretProxiesWithRuntimeCredential(t *testing.T) {
	t.Setenv("NETWORK_FILTER_CONTROL_SOCKET", "")
	parent := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "/internal/session-control/public-session/settings-secrets/sec_allowed", r.URL.Path)
		require.Equal(t, "4", r.URL.Query().Get("generation"))
		require.Equal(t, "TOKEN", r.URL.Query().Get("key"))
		require.Equal(t, "Bearer runtime-token", r.Header.Get("Authorization"))
		_, _ = w.Write([]byte(`{"value":"test-value"}`))
	}))
	defer parent.Close()

	s := New(9001, "")
	s.activeSettings = &sessionsettings.SessionSettings{ParentRuntime: &sessionsettings.ParentRuntimeConfig{
		Enabled: true, Endpoint: parent.URL, SessionID: "public-session", Token: "runtime-token", Generation: 4,
	}}
	req := httptest.NewRequest(http.MethodGet, "/settings-secrets/sec_allowed?key=TOKEN", nil)
	req.RemoteAddr = "127.0.0.1:12345"
	rec := httptest.NewRecorder()
	s.handleSettingsSecret(rec, req)
	require.Equal(t, http.StatusOK, rec.Code)
	require.JSONEq(t, `{"value":"test-value"}`, rec.Body.String())
	require.Equal(t, "no-store", rec.Header().Get("Cache-Control"))
}

func TestHandleOneTimeSecretRejectsNonLoopback(t *testing.T) {
	s := New(9001, "")
	req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/one-time-secrets/secret-1", nil)
	req.RemoteAddr = "10.0.0.2:12345"
	rec := httptest.NewRecorder()
	s.handleOneTimeSecret(rec, req)
	require.Equal(t, http.StatusForbidden, rec.Code)
}

func TestHandleOneTimeSecretProxiesNextWithoutID(t *testing.T) {
	t.Setenv("NETWORK_FILTER_CONTROL_SOCKET", "")
	parent := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "/internal/session-control/public-session/secrets/next", r.URL.Path)
		require.Equal(t, "4", r.URL.Query().Get("generation"))
		_, _ = w.Write([]byte(`{"value":"next-value"}`))
	}))
	defer parent.Close()
	s := New(9001, "")
	s.activeSettings = &sessionsettings.SessionSettings{ParentRuntime: &sessionsettings.ParentRuntimeConfig{
		Enabled: true, Endpoint: parent.URL, SessionID: "public-session", Token: "runtime-token", Generation: 4,
	}}
	req := httptest.NewRequest(http.MethodGet, "/one-time-secrets/next", nil)
	req.RemoteAddr = "127.0.0.1:12345"
	rec := httptest.NewRecorder()
	s.handleOneTimeSecret(rec, req)
	require.Equal(t, http.StatusOK, rec.Code)
	require.JSONEq(t, `{"value":"next-value"}`, rec.Body.String())
}

func TestHandleOneTimeSecretDoesNotUseNetworkFilterControlSocketForParent(t *testing.T) {
	t.Setenv("NETWORK_FILTER_CONTROL_SOCKET", filepath.Join(t.TempDir(), "control.sock"))
	parent := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"value":"next-value"}`))
	}))
	defer parent.Close()

	s := New(9001, "")
	s.activeSettings = &sessionsettings.SessionSettings{ParentRuntime: &sessionsettings.ParentRuntimeConfig{
		Enabled: true, Endpoint: parent.URL, SessionID: "public-session", Token: "runtime-token", Generation: 4,
	}}
	req := httptest.NewRequest(http.MethodGet, "/one-time-secrets/next", nil)
	req.RemoteAddr = "127.0.0.1:12345"
	rec := httptest.NewRecorder()
	s.handleOneTimeSecret(rec, req)
	require.Equal(t, http.StatusOK, rec.Code)
	require.JSONEq(t, `{"value":"next-value"}`, rec.Body.String())
}

func TestHandleOneTimeSecretAcceptsUnixSocketCaller(t *testing.T) {
	t.Setenv("NETWORK_FILTER_CONTROL_SOCKET", "")
	parent := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "/internal/session-control/public-session/secrets/next", r.URL.Path)
		_, _ = w.Write([]byte(`{"value":"next-value"}`))
	}))
	defer parent.Close()

	socketPath := filepath.Join(t.TempDir(), "provisioner.sock")
	s := NewWithSocket(socketPath, "")
	s.activeSettings = &sessionsettings.SessionSettings{ParentRuntime: &sessionsettings.ParentRuntimeConfig{
		Enabled: true, Endpoint: parent.URL, SessionID: "public-session", Token: "runtime-token", Generation: 4,
	}}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- s.Start(ctx) }()
	defer func() {
		cancel()
		require.NoError(t, <-done)
	}()

	require.Eventually(t, func() bool {
		_, err := os.Stat(socketPath)
		return err == nil
	}, 2*time.Second, 10*time.Millisecond)
	client := &http.Client{Transport: &http.Transport{DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, "unix", socketPath)
	}}}
	req, err := http.NewRequest(http.MethodGet, "http://unix/one-time-secrets/next", nil)
	require.NoError(t, err)
	resp, err := client.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close() //nolint:errcheck
	require.Equal(t, http.StatusOK, resp.StatusCode)
}
