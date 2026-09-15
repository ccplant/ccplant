package provisioner

import (
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/takutakahashi/agentapi-proxy/pkg/modelprovider"
	"github.com/takutakahashi/agentapi-proxy/pkg/sessionsettings"
	"golang.org/x/net/http/httpproxy"
)

// The agent SDKs append fixed endpoint paths. A session-local relay rewrites
// those paths, preserving streaming, authentication and the original API body.
// Only the generated process configuration uses the temporary loopback address;
// persisted connection identity always describes the actual upstream.
func prepareModelEndpoint(settings *sessionsettings.SessionSettings, home string, env map[string]string) (func(), error) {
	c, agent := settings.CodexConnection, "codex"
	if c == nil {
		c, agent = settings.ClaudeConnection, "claude"
	}
	if !c.Compatible() || c.EndpointPath == "" {
		return func() {}, nil
	}
	localURL, stop, err := startModelEndpointRelay(c, agent, env)
	if err != nil {
		return nil, err
	}
	if agent == "claude" {
		env["ANTHROPIC_BASE_URL"] = localURL
		return stop, nil
	}
	path := filepath.Join(home, ".codex", "config.toml")
	raw, err := os.ReadFile(path)
	if err == nil {
		local := c.Clone()
		local.BaseURL = localURL
		var content string
		content, err = sessionsettings.MergeCodexConnectionConfig(string(raw), local)
		if err == nil {
			err = os.WriteFile(path, []byte(content), 0600)
		}
	}
	if err != nil {
		stop()
		return nil, fmt.Errorf("failed to configure model endpoint relay")
	}
	return stop, nil
}

func startModelEndpointRelay(c *modelprovider.Connection, agent string, env map[string]string) (string, func(), error) {
	if err := c.Validate(agent); err != nil {
		return "", nil, err
	}
	upstream, _ := url.Parse(c.BaseURL)
	fixedPath := "/responses"
	if agent == "claude" {
		fixedPath = "/v1/messages"
	}
	prefix := strings.TrimRight(upstream.Path, "/")
	transport := http.DefaultTransport.(*http.Transport).Clone()
	// Use the same outbound proxy and CA configuration as the session process.
	// Do not rely on ProxyFromEnvironment's globally cached parent environment.
	lookup := func(keys ...string) string {
		for _, key := range keys {
			if value, ok := env[key]; ok {
				return value
			}
		}
		for _, key := range keys {
			if value := os.Getenv(key); value != "" {
				return value
			}
		}
		return ""
	}
	proxyConfig := &httpproxy.Config{HTTPProxy: lookup("HTTP_PROXY", "http_proxy"), HTTPSProxy: lookup("HTTPS_PROXY", "https_proxy"), NoProxy: lookup("NO_PROXY", "no_proxy")}
	proxyFunc := proxyConfig.ProxyFunc()
	transport.Proxy = func(r *http.Request) (*url.URL, error) { return proxyFunc(r.URL) }
	roots, _ := x509.SystemCertPool()
	if roots == nil {
		roots = x509.NewCertPool()
	}
	for _, key := range []string{"SSL_CERT_FILE", "NODE_EXTRA_CA_CERTS"} {
		if file := lookup(key); file != "" {
			pem, err := os.ReadFile(file)
			if err != nil || !roots.AppendCertsFromPEM(pem) {
				return "", nil, fmt.Errorf("invalid model endpoint CA configuration")
			}
		}
	}
	transport.TLSClientConfig = &tls.Config{RootCAs: roots, MinVersion: tls.VersionTLS12}
	endpointPath := c.EndpointPath
	proxy := &httputil.ReverseProxy{
		Transport:     transport,
		FlushInterval: -1,
		Rewrite: func(r *httputil.ProxyRequest) {
			incoming := r.In.URL.Path
			r.SetURL(upstream)
			if incoming == fixedPath || strings.HasPrefix(incoming, fixedPath+"/") {
				targetPath := endpointPath
				if suffix := strings.TrimPrefix(incoming, fixedPath); suffix != "" {
					targetPath = strings.TrimRight(targetPath, "/") + suffix
				}
				r.Out.URL.Path = prefix + targetPath
				r.Out.URL.RawPath = ""
			}
			// Authorization remains supplied by the agent; the relay stores no key.
		},
		ErrorHandler: func(w http.ResponseWriter, r *http.Request, err error) {
			// Transport errors may include upstream URL/query data. Keep these private.
			http.Error(w, "compatible API endpoint unavailable", http.StatusBadGateway)
		},
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return "", nil, fmt.Errorf("failed to listen for model endpoint relay")
	}
	server := &http.Server{Handler: proxy, ReadHeaderTimeout: 10 * time.Second}
	go func() { _ = server.Serve(listener) }()
	stop := func() {
		// Close active streams as well when the supervised agent exits.
		_ = server.Close()
		transport.CloseIdleConnections()
	}
	return "http://" + listener.Addr().String(), stop, nil
}
