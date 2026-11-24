package provisioner

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	closeutils "github.com/takutakahashi/agentapi-proxy/pkg/utils"
	"net/http"
	"net/url"
	"time"
)

func reportProvisionRequestStatus(ctx context.Context, client *http.Client, cfg PullClientConfig, requestID string, st Status, msg string) error {
	return reportProvisionRequestStatusWithCode(ctx, client, cfg, requestID, st, msg, "")
}

func reportProvisionRequestStatusWithCode(ctx context.Context, client *http.Client, cfg PullClientConfig, requestID string, st Status, msg, errorCode string) error {
	path := "/internal/session-provisioners/" + url.PathEscape(cfg.SessionID) + "/provision-requests/" + url.PathEscape(requestID) + "/status"
	return postJSON(ctx, client, cfg, path, map[string]interface{}{
		"status":     string(st),
		"message":    msg,
		"error_code": errorCode,
		"pod_name":   cfg.PodName,
	})
}

func postJSON(ctx context.Context, client *http.Client, cfg PullClientConfig, path string, body interface{}) error {
	data, err := json.Marshal(body)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, cfg.ProxyURL+path, bytes.NewReader(data))
	if err != nil {
		return err
	}
	authorizePullRequest(req, cfg)
	req.Header.Set("Content-Type", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer closeutils.SafeCloseResponseBody(resp.Body) //nolint:errcheck
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return &httpStatusError{method: http.MethodPost, path: path, status: resp.StatusCode}
	}
	return nil
}

type httpStatusError struct {
	method string
	path   string
	status int
}

func (e *httpStatusError) Error() string {
	return fmt.Sprintf("%s %s returned HTTP %d", e.method, e.path, e.status)
}

func authorizePullRequest(req *http.Request, cfg PullClientConfig) {
	if cfg.UpstreamAuthToken != "" {
		req.Header.Set("Authorization", "Bearer "+cfg.UpstreamAuthToken)
		req.Header.Set("X-Session-Manager-Token", cfg.Token)
		return
	}
	req.Header.Set("Authorization", "Bearer "+cfg.Token)
}

func sleepOrDone(ctx context.Context, d time.Duration) {
	select {
	case <-ctx.Done():
	case <-time.After(d):
	}
}
