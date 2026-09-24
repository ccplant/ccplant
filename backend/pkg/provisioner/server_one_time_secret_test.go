package provisioner

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/takutakahashi/agentapi-proxy/pkg/sessionsettings"
)

func TestHandleOneTimeSecretProxiesWithRuntimeCredential(t *testing.T) {
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

func TestHandleOneTimeSecretRejectsNonLoopback(t *testing.T) {
	s := New(9001, "")
	req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/one-time-secrets/secret-1", nil)
	req.RemoteAddr = "10.0.0.2:12345"
	rec := httptest.NewRecorder()
	s.handleOneTimeSecret(rec, req)
	require.Equal(t, http.StatusForbidden, rec.Code)
}

func TestHandleOneTimeSecretProxiesNextWithoutID(t *testing.T) {
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
