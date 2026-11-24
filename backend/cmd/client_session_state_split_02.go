package cmd

import (
	closeutils "github.com/takutakahashi/agentapi-proxy/pkg/utils"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"
)

func objectStorageClient(rawURL string, timeout time.Duration) *http.Client {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return &http.Client{Timeout: timeout}
	}
	hostname := parsed.Hostname()
	if strings.Contains(hostname, ".") && !strings.HasSuffix(hostname, ".svc") && !strings.HasSuffix(hostname, ".cluster.local") {
		return &http.Client{Timeout: timeout}
	}
	transport, ok := http.DefaultTransport.(*http.Transport)
	if !ok {
		return &http.Client{Timeout: timeout}
	}
	direct := transport.Clone()
	direct.Proxy = nil
	return &http.Client{Timeout: timeout, Transport: direct}
}

func abortDirectUpload(client *http.Client, proxy, token, id, uploadID string) {
	req, err := internalRequest(http.MethodDelete, proxy+"/internal/session-state/"+id+"/uploads/"+uploadID, token, nil)
	if err == nil {
		resp, callErr := client.Do(req)
		if callErr == nil {
			closeutils.SafeCloseResponseBody(resp.Body)
		}
	}
}

func readACPSessionID(cwd string) string {
	data, _ := os.ReadFile(filepath.Join(cwd, ".acp-session-id"))
	return strings.TrimSpace(string(data))
}
