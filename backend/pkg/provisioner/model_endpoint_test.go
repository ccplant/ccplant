package provisioner

import (
	"bufio"
	"encoding/pem"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/takutakahashi/agentapi-proxy/pkg/modelprovider"
	"github.com/takutakahashi/agentapi-proxy/pkg/sessionsettings"
)

func TestModelEndpointRelayUsesSessionProxyAndCA(t *testing.T) {
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "http://upstream.invalid/prefix/generate", r.URL.String())
		_, _ = io.WriteString(w, "proxied")
	}))
	defer proxy.Close()
	c := &modelprovider.Connection{Mode: "openai_compatible", BaseURL: "http://upstream.invalid/prefix", EndpointPath: "/generate", Model: "example", Authentication: "none"}
	local, stop, err := startModelEndpointRelay(c, "codex", map[string]string{"HTTP_PROXY": proxy.URL, "NO_PROXY": ""})
	require.NoError(t, err)
	defer stop()
	res, err := (&http.Client{Timeout: 5 * time.Second}).Get(local + "/responses")
	require.NoError(t, err)
	body, err := io.ReadAll(res.Body)
	res.Body.Close()
	require.NoError(t, err)
	require.Equal(t, "proxied", string(body))

	upstream := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = io.WriteString(w, "tls") }))
	defer upstream.Close()
	caFile := filepath.Join(t.TempDir(), "ca.pem")
	require.NoError(t, os.WriteFile(caFile, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: upstream.Certificate().Raw}), 0600))
	c.BaseURL = upstream.URL
	local, stopTLS, err := startModelEndpointRelay(c, "codex", map[string]string{"SSL_CERT_FILE": caFile})
	require.NoError(t, err)
	defer stopTLS()
	res, err = (&http.Client{Timeout: 5 * time.Second}).Get(local + "/responses")
	require.NoError(t, err)
	body, err = io.ReadAll(res.Body)
	res.Body.Close()
	require.NoError(t, err)
	require.Equal(t, "tls", string(body))
}

func TestModelEndpointRelayRoutesRequests(t *testing.T) {
	for _, tc := range []struct{ agent, mode, source, target string }{
		{"codex", "openai_compatible", "/responses", "/generate"},
		{"codex", "openai_compatible", "/responses/compact", "/generate/compact"},
		{"claude", "anthropic_compatible", "/v1/messages", "/custom/messages"},
		{"claude", "anthropic_compatible", "/v1/messages/count_tokens", "/custom/messages/count_tokens"},
	} {
		t.Run(tc.agent+tc.source, func(t *testing.T) {
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				require.Equal(t, "/prefix"+tc.target, r.URL.Path)
				require.Equal(t, "beta=true", r.URL.RawQuery)
				require.Equal(t, "POST", r.Method)
				require.Equal(t, "Bearer test-key", r.Header.Get("Authorization"))
				require.Equal(t, "test-key", r.Header.Get("X-Api-Key"))
				body, err := io.ReadAll(r.Body)
				require.NoError(t, err)
				require.JSONEq(t, `{"model":"example","stream":true}`, string(body))
				w.Header().Set("Content-Type", "text/event-stream")
				w.WriteHeader(http.StatusAccepted)
				_, _ = io.WriteString(w, "data: hello\n\n")
			}))
			defer upstream.Close()
			endpoint := "/generate"
			if tc.agent == "claude" {
				endpoint = "/custom/messages"
			}
			c := &modelprovider.Connection{Mode: tc.mode, BaseURL: upstream.URL + "/prefix/", EndpointPath: endpoint, Model: "example", Authentication: "api_key", APIKey: "test-key"}
			local, stop, err := startModelEndpointRelay(c, tc.agent, nil)
			require.NoError(t, err)
			defer stop()
			req, err := http.NewRequest("POST", local+tc.source+"?beta=true", strings.NewReader(`{"model":"example","stream":true}`))
			require.NoError(t, err)
			req.Header.Set("Authorization", "Bearer test-key")
			req.Header.Set("X-Api-Key", "test-key")
			res, err := (&http.Client{Timeout: 5 * time.Second}).Do(req)
			require.NoError(t, err)
			defer res.Body.Close()
			body, err := io.ReadAll(res.Body)
			require.NoError(t, err)
			require.Equal(t, http.StatusAccepted, res.StatusCode)
			require.Equal(t, "data: hello\n\n", string(body))
		})
	}
}

func TestModelEndpointRelayFlushesBeforeUpstreamCompletes(t *testing.T) {
	release := make(chan struct{})
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, "data: first\n\n")
		w.(http.Flusher).Flush()
		select {
		case <-release:
		case <-r.Context().Done():
		}
	}))
	defer upstream.Close()
	c := &modelprovider.Connection{Mode: "openai_compatible", BaseURL: upstream.URL, EndpointPath: "/generate", Model: "example", Authentication: "none"}
	local, stop, err := startModelEndpointRelay(c, "codex", nil)
	require.NoError(t, err)
	defer stop()
	res, err := (&http.Client{Timeout: 5 * time.Second}).Get(local + "/responses")
	require.NoError(t, err)
	defer res.Body.Close()
	line, err := bufio.NewReader(res.Body).ReadString('\n')
	require.NoError(t, err)
	require.Equal(t, "data: first\n", line)
	close(release)
}

func TestPrepareModelEndpointPreservesUpstreamIdentity(t *testing.T) {
	home := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(home, ".codex"), 0700))
	path := filepath.Join(home, ".codex", "config.toml")
	require.NoError(t, os.WriteFile(path, []byte("sandbox_mode = 'workspace-write'\n"), 0600))
	c := &modelprovider.Connection{Mode: "openai_compatible", BaseURL: "https://gateway.example/v1", EndpointPath: "/generate", Model: "example", Authentication: "none"}
	settings := &sessionsettings.SessionSettings{CodexConnection: c}
	stop, err := prepareModelEndpoint(settings, home, map[string]string{})
	require.NoError(t, err)
	defer stop()
	require.Equal(t, "https://gateway.example/v1", c.BaseURL)
	raw, err := os.ReadFile(path)
	require.NoError(t, err)
	require.Contains(t, string(raw), "http://127.0.0.1:")
	require.Contains(t, string(raw), "workspace-write")
	require.NoError(t, persistModelConnectionIdentity(settings, home, false))
	c.EndpointPath = "/other"
	require.Error(t, persistModelConnectionIdentity(settings, home, true))
}
