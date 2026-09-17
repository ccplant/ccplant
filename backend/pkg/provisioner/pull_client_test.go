package provisioner

import (
	"context"
	"encoding/pem"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestNewPullHTTPClientLoadsSCIACA(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()

	caFile := filepath.Join(t.TempDir(), "scia-ca.pem")
	caPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: server.Certificate().Raw})
	require.NoError(t, os.WriteFile(caFile, caPEM, 0o600))

	client, err := newPullHTTPClient(context.Background(), caFile)
	require.NoError(t, err)

	resp, err := client.Get(server.URL)
	require.NoError(t, err)
	require.NoError(t, resp.Body.Close())
	require.Equal(t, http.StatusNoContent, resp.StatusCode)
}

func TestAuthorizePullRequestWithParentAuthentication(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	authorizePullRequest(req, PullClientConfig{Token: "manager-token", UpstreamAuthToken: "parent-token"})
	require.Equal(t, "Bearer parent-token", req.Header.Get("Authorization"))
	require.Equal(t, "manager-token", req.Header.Get("X-Session-Manager-Token"))
}

func TestLocalPullProxyAddress(t *testing.T) {
	tests := []struct {
		name      string
		proxyURL  string
		want      string
		wantLocal bool
	}{
		{name: "network filter sidecar", proxyURL: "http://127.0.0.1:3128", want: "127.0.0.1:3128", wantLocal: true},
		{name: "localhost default port", proxyURL: "http://localhost", want: "localhost:80", wantLocal: true},
		{name: "IPv6 loopback", proxyURL: "http://[::1]:3128", want: "[::1]:3128", wantLocal: true},
		{name: "remote proxy", proxyURL: "http://proxy.example:3128"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			proxyURL := tt.proxyURL
			client := &http.Client{Transport: &http.Transport{Proxy: func(*http.Request) (*url.URL, error) {
				return url.Parse(proxyURL)
			}}}
			got, local, err := localPullProxyAddress(client, "https://control.example")
			require.NoError(t, err)
			require.Equal(t, tt.wantLocal, local)
			require.Equal(t, tt.want, got)
		})
	}
}
