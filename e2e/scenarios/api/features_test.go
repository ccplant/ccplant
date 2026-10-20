//go:build e2e

package api_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"testing"
	"time"

	"github.com/ccplant/ccplant/e2e/pkg/apitest"
)

func client(t *testing.T) *apitest.Client {
	t.Helper()
	c, err := apitest.FromEnv()
	if err != nil {
		t.Fatal(err)
	}
	return c
}
func request(t *testing.T, c *apitest.Client, method, path string, in, out any, status ...int) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
	defer cancel()
	if err := c.Do(ctx, method, path, in, out, status...); err != nil {
		t.Fatal(err)
	}
}
func object(t *testing.T, c *apitest.Client, path string) map[string]json.RawMessage {
	t.Helper()
	var out map[string]json.RawMessage
	request(t, c, http.MethodGet, path, nil, &out, 200)
	if out == nil {
		t.Fatal("expected JSON object")
	}
	return out
}
func requireArray(t *testing.T, out map[string]json.RawMessage, key string) {
	t.Helper()
	raw, ok := out[key]
	if !ok {
		t.Fatalf("missing field %s", key)
	}
	var rows []json.RawMessage
	if err := json.Unmarshal(raw, &rows); err != nil {
		t.Fatalf("%s must be an array or null", key)
	}
}

func TestAuthenticatedUser(t *testing.T) {
	out := object(t, client(t), "/user/info")
	var id string
	if json.Unmarshal(out["principal_id"], &id) != nil || id == "" {
		t.Fatal("principal_id must be nonempty")
	}
	requireArray(t, out, "teams")
}
func TestUnauthenticatedAccessDenied(t *testing.T) {
	c := client(t)
	anonymous := &apitest.Client{BaseURL: c.BaseURL, HTTP: c.HTTP}
	request(t, anonymous, "GET", "/user/info", nil, nil, 401, 403)
}
func TestSessionSearch(t *testing.T) {
	c := client(t)
	t.Run("list accessible sessions", func(t *testing.T) { requireArray(t, object(t, c, "/search"), "sessions") })
	t.Run("filter by unique tag", func(t *testing.T) {
		out := object(t, c, "/search?tag.e2e_run="+url.QueryEscape(apitest.UniqueName()))
		requireArray(t, out, "sessions")
		var rows []json.RawMessage
		if err := json.Unmarshal(out["sessions"], &rows); err != nil || len(rows) != 0 {
			t.Fatal("unique tag search must not return unrelated sessions")
		}
	})
}
func TestSettingsManagers(t *testing.T) {
	requireArray(t, object(t, client(t), "/settings/managers"), "managers")
}
func TestSessionProfilesList(t *testing.T) {
	requireArray(t, object(t, client(t), "/session-profiles?scope=user"), "session_profiles")
}
func TestSchedulesList(t *testing.T) {
	requireArray(t, object(t, client(t), "/schedules?scope=user"), "schedules")
}
func TestWebhooksList(t *testing.T) {
	requireArray(t, object(t, client(t), "/webhooks?scope=user"), "webhooks")
}
func TestSlackbotsList(t *testing.T) {
	var rows []json.RawMessage
	request(t, client(t), "GET", "/slackbots?scope=user", nil, &rows, 200)
}

type resource struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	Status string `json:"status"`
}

// lifecycle only addresses the generated resource ID; existing resources are never changed.
func lifecycle(t *testing.T, path, listKey string, create map[string]any, deleteStatus int) {
	t.Helper()
	c := client(t)
	name := apitest.UniqueName()
	create["name"] = name
	create["scope"] = "user"
	var created resource
	request(t, c, "POST", path, create, &created, 201)
	if created.ID == "" {
		t.Fatal("create response must contain id")
	}
	item := path + "/" + url.PathEscape(created.ID)
	deleted := false
	t.Cleanup(func() {
		if deleted {
			return
		}
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		if err := c.Do(ctx, "DELETE", item, nil, nil, deleteStatus, 404); err != nil {
			t.Errorf("cleanup of test resource %s failed: %v", created.ID, err)
		}
	})
	if created.Name != name {
		t.Fatal("created name differs")
	}
	if status, ok := create["status"].(string); ok && created.Status != status {
		t.Fatal("resource was not created in paused state")
	}
	steps := []struct {
		name string
		run  func(*testing.T)
	}{
		{"read created resource", func(t *testing.T) {
			var got resource
			request(t, c, "GET", item, nil, &got, 200)
			if got.ID != created.ID || got.Name != name {
				t.Fatal("resource does not match create")
			}
		}},
		{"find resource in list", func(t *testing.T) {
			out := object(t, c, path+"?scope=user")
			var rows []resource
			if err := json.Unmarshal(out[listKey], &rows); err != nil {
				t.Fatal("invalid list")
			}
			for _, row := range rows {
				if row.ID == created.ID {
					return
				}
			}
			t.Fatal("created resource missing from list")
		}},
		{"update name", func(t *testing.T) {
			var got resource
			request(t, c, "PUT", item, map[string]any{"name": name + "-updated"}, &got, 200)
			if got.Name != name+"-updated" {
				t.Fatal("updated name differs")
			}
			request(t, c, "GET", item, nil, &got, 200)
			if got.Name != name+"-updated" {
				t.Fatal("update was not persisted")
			}
			if status, ok := create["status"].(string); ok && got.Status != status {
				t.Fatal("update changed paused state")
			}
		}},
		{"delete resource", func(t *testing.T) {
			request(t, c, "DELETE", item, nil, nil, deleteStatus)
			deleted = true
			request(t, c, "GET", item, nil, nil, 404)
		}},
	}
	for _, step := range steps {
		if !t.Run(step.name, step.run) {
			return
		}
	}
}
func TestSessionProfileLifecycle(t *testing.T) {
	lifecycle(t, "/session-profiles", "session_profiles", map[string]any{"description": "E2E専用・自動削除", "is_default": false, "config": map[string]any{}}, 204)
}
func TestScheduleLifecycle(t *testing.T) {
	lifecycle(t, "/schedules", "schedules", map[string]any{"status": "paused", "cron_expr": "0 0 1 1 *", "timezone": "UTC", "session_config": map[string]any{}}, 200)
}
func TestWebhookLifecycle(t *testing.T) {
	lifecycle(t, "/webhooks", "webhooks", map[string]any{"status": "paused", "type": "custom", "triggers": []any{map[string]any{"name": "e2e-disabled", "enabled": false, "conditions": map[string]any{"go_template": "{{ false }}"}}}}, 200)
}

func TestSessionDryRun(t *testing.T) {
	c := client(t)
	var out struct {
		DryRun    bool   `json:"dry_run"`
		SessionID string `json:"session_id"`
		Decision  string `json:"decision"`
	}
	request(t, c, "POST", "/start?dry_run=true", map[string]any{"scope": "user", "tags": map[string]string{"e2e_run": apitest.UniqueName()}}, &out, 200)
	if !out.DryRun || out.SessionID == "" || out.Decision != "create" {
		t.Fatal("expected a create preview without starting a session")
	}
}
func TestInvalidProfileRejected(t *testing.T) {
	request(t, client(t), "POST", "/session-profiles", map[string]any{"name": "", "config": map[string]any{}}, nil, 400)
}
func TestInvalidScheduleRejected(t *testing.T) {
	request(t, client(t), "POST", "/schedules", map[string]any{"name": apitest.UniqueName(), "status": "paused", "cron_expr": "not-a-cron"}, nil, 400)
}
