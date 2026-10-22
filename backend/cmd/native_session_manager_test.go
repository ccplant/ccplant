package cmd

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/labstack/echo/v4"
	"github.com/stretchr/testify/require"
	"github.com/takutakahashi/agentapi-proxy/internal/infrastructure/services"
)

func TestNativeSessionManagerLocalURL(t *testing.T) {
	tests := []struct {
		name   string
		listen string
		want   string
	}{
		{name: "all IPv4 interfaces", listen: ":8080", want: "http://127.0.0.1:8080"},
		{name: "explicit all IPv4 interfaces", listen: "0.0.0.0:8081", want: "http://127.0.0.1:8081"},
		{name: "all IPv6 interfaces", listen: "[::]:8082", want: "http://127.0.0.1:8082"},
		{name: "explicit loopback", listen: "127.0.0.1:8083", want: "http://127.0.0.1:8083"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := nativeSessionManagerLocalURL(tt.listen)
			require.NoError(t, err)
			require.Equal(t, tt.want, got)
		})
	}
}

func TestNativeSessionControlRoutesUseBearerAuthAndInternalPathsDoNotFallThrough(t *testing.T) {
	const master = "provisioner-token"
	manager, err := services.NewNativeSessionManager(filepath.Join(t.TempDir(), "state"), "http://127.0.0.1:8080", master, "", "ccplant", false)
	require.NoError(t, err)
	e := echo.New()
	registerNativeSessionControlRoutes(e, manager)

	req := httptest.NewRequest(http.MethodGet, "/internal/session-control/session-a/commands", nil)
	req.Header.Set("Authorization", "Bearer "+nativeSessionControlToken(master, "session-a"))
	recorder := httptest.NewRecorder()
	e.ServeHTTP(recorder, req)
	require.Equal(t, http.StatusNoContent, recorder.Code)

	unknown := httptest.NewRequest(http.MethodGet, "/internal/not-a-public-session/status", nil)
	unknownRecorder := httptest.NewRecorder()
	e.ServeHTTP(unknownRecorder, unknown)
	require.Equal(t, http.StatusNotFound, unknownRecorder.Code)
}

func nativeSessionControlToken(master, sessionID string) string {
	mac := hmac.New(sha256.New, []byte(master))
	_, _ = mac.Write([]byte("session-control-auth:v1\x00"))
	_, _ = mac.Write([]byte(sessionID))
	return base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}

func TestNativeSessionManagerLocalURLRejectsInvalidListenAddress(t *testing.T) {
	_, err := nativeSessionManagerLocalURL("8080")
	require.Error(t, err)
}
