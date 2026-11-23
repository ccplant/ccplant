package client

import (
	"context"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"testing"
)

func TestConfigFromEnvAllowsLocalSocketWithoutAPIKey(t *testing.T) {
	t.Setenv("AGENTAPI_SESSION_ID", "session-1")
	t.Setenv("AGENTAPI_KEY", "")
	t.Setenv("AGENTAPI_PROXY_ENDPOINT", "http+unix://%2Ftmp%2Fccplant%2Fprovisioner.sock")

	config, err := ConfigFromEnv()
	if err != nil {
		t.Fatalf("ConfigFromEnv() error = %v", err)
	}
	if config.APIKey != "" {
		t.Fatalf("ConfigFromEnv() APIKey = %q, want empty", config.APIKey)
	}
}

func TestNewClientUsesUnixSocketEndpoint(t *testing.T) {
	socketPath := filepath.Join(t.TempDir(), "broker.sock")
	listener, err := net.Listen("unix", socketPath)
	if err != nil {
		t.Fatal(err)
	}
	server := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/search" {
			t.Errorf("path = %q, want /search", r.URL.Path)
		}
		_, _ = w.Write([]byte(`{"sessions":[]}`))
	})}
	go func() { _ = server.Serve(listener) }()
	t.Cleanup(func() {
		_ = server.Shutdown(context.Background())
		_ = os.Remove(socketPath)
	})

	endpoint := "http+unix://" + url.PathEscape(socketPath)
	if _, err := NewClient(endpoint).Search(context.Background(), ""); err != nil {
		t.Fatalf("Search() error = %v", err)
	}
}

func TestEndpointFromEnvPrefersExplicitEndpoint(t *testing.T) {
	t.Setenv("AGENTAPI_PROXY_ENDPOINT", "https://proxy.example/base")
	t.Setenv("AGENTAPI_PROXY_SERVICE_HOST", "local-proxy")
	t.Setenv("AGENTAPI_PROXY_SERVICE_PORT_HTTP", "8080")

	endpoint, err := EndpointFromEnv()
	if err != nil {
		t.Fatalf("EndpointFromEnv() error = %v", err)
	}
	if endpoint != "https://proxy.example/base" {
		t.Fatalf("EndpointFromEnv() = %q, want explicit endpoint", endpoint)
	}
}

func TestEndpointFromEnvFallsBackToServiceEnvironment(t *testing.T) {
	t.Setenv("AGENTAPI_PROXY_ENDPOINT", "")
	t.Setenv("AGENTAPI_PROXY_SERVICE_HOST", "local-proxy")
	t.Setenv("AGENTAPI_PROXY_SERVICE_PORT_HTTP", "8080")

	endpoint, err := EndpointFromEnv()
	if err != nil {
		t.Fatalf("EndpointFromEnv() error = %v", err)
	}
	if endpoint != "http://local-proxy:8080" {
		t.Fatalf("EndpointFromEnv() = %q, want service endpoint", endpoint)
	}
}
