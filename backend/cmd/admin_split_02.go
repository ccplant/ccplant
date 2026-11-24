package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/takutakahashi/agentapi-proxy/pkg/client"
	closeutils "github.com/takutakahashi/agentapi-proxy/pkg/utils"
)

func (opts *adminOptions) api() (*adminAPI, error) {
	endpoint, apiKey, err := resolveCLIConnection(context.Background(), opts.endpoint, opts.apiKey)
	if err != nil {
		return nil, err
	}
	return &adminAPI{baseURL: strings.TrimRight(endpoint, "/"), apiKey: apiKey, client: http.DefaultClient}, nil
}

func (a *adminAPI) get(ctx context.Context, path string, query url.Values, out any) error {
	u, err := url.Parse(a.baseURL + path)
	if err != nil {
		return err
	}
	u.RawQuery = query.Encode()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return err
	}
	if a.apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+a.apiKey)
	}
	resp, err := a.client.Do(req)
	if err != nil {
		return fmt.Errorf("request %s: %w", path, err)
	}
	defer func() { closeutils.SafeCloseResponseBody(resp.Body) }()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 64<<10))
		return fmt.Errorf("request %s failed: %s: %s", path, resp.Status, strings.TrimSpace(string(body)))
	}
	if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
		return fmt.Errorf("decode %s response: %w", path, err)
	}
	return nil
}

func (a *adminAPI) delete(ctx context.Context, path string, query url.Values) error {
	u, err := url.Parse(a.baseURL + path)
	if err != nil {
		return err
	}
	u.RawQuery = query.Encode()
	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, u.String(), nil)
	if err != nil {
		return err
	}
	if a.apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+a.apiKey)
	}
	resp, err := a.client.Do(req)
	if err != nil {
		return fmt.Errorf("request %s: %w", path, err)
	}
	defer func() { closeutils.SafeCloseResponseBody(resp.Body) }()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 64<<10))
		return fmt.Errorf("request %s failed: %s: %s", path, resp.Status, strings.TrimSpace(string(body)))
	}
	return nil
}

func (a *adminAPI) runners(ctx context.Context) ([]adminRunner, error) {
	var result adminRunnerList
	if err := a.get(ctx, "/admin/session-runners", url.Values{"scope": {"system"}}, &result); err != nil {
		return nil, err
	}
	return result.Runners, nil
}

func (a *adminAPI) deleteRunner(ctx context.Context, runner adminRunner, force bool) error {
	query := url.Values{"manager_id": {runner.ManagerID}}
	if force {
		query.Set("force", "true")
	}
	return a.delete(ctx, "/admin/session-runners/"+url.PathEscape(runner.ID), query)
}

func (a *adminAPI) session(ctx context.Context, id string) (*client.SessionInfo, error) {
	var result client.SearchResponse
	if err := a.get(ctx, "/search", nil, &result); err != nil {
		return nil, err
	}
	for i := range result.Sessions {
		if result.Sessions[i].SessionID == id {
			return &result.Sessions[i], nil
		}
	}
	return nil, fmt.Errorf("session %q not found", id)
}

func (a *adminAPI) logs(ctx context.Context, path string, query url.Values) (*adminLogs, error) {
	var result adminLogs
	if err := a.get(ctx, path, query, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

func findRunner(items []adminRunner, matches func(adminRunner) bool, kind, id string) (*adminRunner, error) {
	var found *adminRunner
	for i := range items {
		if !matches(items[i]) {
			continue
		}
		if found != nil {
			return nil, fmt.Errorf("%s %q matches multiple runners", kind, id)
		}
		found = &items[i]
	}
	if found == nil {
		return nil, fmt.Errorf("%s %q not found", kind, id)
	}
	return found, nil
}

func writeJSON(w io.Writer, value any) error {
	encoder := json.NewEncoder(w)
	encoder.SetIndent("", "  ")
	return encoder.Encode(value)
}

func emptyDash(value string) string {
	if value == "" {
		return "-"
	}
	return value
}

func formatAdminTime(value time.Time) string {
	if value.IsZero() {
		return "-"
	}
	return value.Local().Format(time.RFC3339)
}
