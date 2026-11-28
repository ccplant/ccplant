//go:build e2e

package api_test

import (
	"context"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/ccplant/ccplant/e2e/pkg/apitest"
)

func TestProfileSecretAccess(t *testing.T) {
	if !strings.EqualFold(os.Getenv("E2E_PARAM_LIVE_SESSION"), "true") {
		t.Skip("set E2E_PARAM_LIVE_SESSION=true to run the resource-consuming profile-secret scenario")
	}

	pool := strings.TrimSpace(os.Getenv("E2E_PARAM_POOL"))
	if pool == "" {
		pool = "fly-dev"
	}
	agentType := strings.TrimSpace(os.Getenv("E2E_PARAM_AGENT_TYPE"))
	if agentType == "" {
		agentType = "codex-acp"
	}

	c := client(t)
	name := apitest.UniqueName()
	secretValue := consumeSecretValue(t)

	var user struct {
		PrincipalID string `json:"principal_id"`
	}
	request(t, c, http.MethodGet, "/user/info", nil, &user, http.StatusOK)
	if user.PrincipalID == "" {
		t.Fatal("authenticated user must have a principal_id")
	}
	settingsName := url.PathEscape(user.PrincipalID)

	var secret struct {
		ID string `json:"id"`
	}
	request(t, c, http.MethodPost, "/settings/"+settingsName+"/secrets", map[string]any{
		"name": name + "-secret",
		"values": map[string]string{
			"file-token": secretValue,
			"token":      secretValue,
		},
		"projections": []map[string]any{
			{"key": "token", "type": "env", "env_name": "E2E_PROFILE_SECRET"},
			{"key": "file-token", "type": "file", "path": "/home/agentapi/.e2e-profile-secret", "permissions": "0600"},
		},
	}, &secret, http.StatusCreated)
	if secret.ID == "" {
		t.Fatal("create settings secret response must contain id")
	}

	var profile struct {
		ID string `json:"id"`
	}
	request(t, c, http.MethodPost, "/session-profiles", map[string]any{
		"name": name + "-profile", "description": "E2E profile secret API access; automatically deleted",
		"scope": "user", "is_default": false,
		"config": map[string]any{
			"pool":                     pool,
			"secret_ids":               []string{secret.ID},
			"command_wrapper_template": profileSecretWrapper(secret.ID, name+"-secret"),
			"params": map[string]any{
				"agent_type":  agentType,
				"session_ttl": "10m",
			},
		},
	}, &profile, http.StatusCreated)
	if profile.ID == "" {
		t.Fatal("create profile response must contain id")
	}

	var sessionID string
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		if sessionID != "" {
			if err := c.Do(ctx, http.MethodDelete, "/sessions/"+url.PathEscape(sessionID), nil, nil,
				http.StatusOK, http.StatusAccepted, http.StatusNotFound); err != nil {
				t.Errorf("cleanup session %s failed: %v", sessionID, err)
			}
		}
		if err := c.Do(ctx, http.MethodDelete, "/session-profiles/"+url.PathEscape(profile.ID), nil, nil,
			http.StatusNoContent, http.StatusNotFound); err != nil {
			t.Errorf("cleanup profile %s failed: %v", profile.ID, err)
		}
		if err := c.Do(ctx, http.MethodDelete, "/settings/"+settingsName+"/secrets/"+url.PathEscape(secret.ID), nil, nil,
			http.StatusNoContent, http.StatusNotFound); err != nil {
			t.Errorf("cleanup settings secret %s failed: %v", secret.ID, err)
		}
	})

	var created struct {
		SessionID string `json:"session_id"`
	}
	request(t, c, http.MethodPost, "/start", map[string]any{
		"scope":              "user",
		"session_profile_id": profile.ID,
		"tags":               map[string]string{"e2e_run": name},
		"params": map[string]any{
			"pool":        pool,
			"agent_type":  agentType,
			"session_ttl": "10m",
		},
	}, &created, http.StatusOK)
	if created.SessionID == "" {
		t.Fatal("start response must contain session_id")
	}
	sessionID = created.SessionID

	var registered struct {
		SecretID string `json:"secret_id"`
	}
	request(t, c, http.MethodPost, "/sessions/"+url.PathEscape(sessionID)+"/secrets", map[string]any{
		"value": secretValue, "expires_in_seconds": 300,
	}, &registered, http.StatusCreated)
	secretValue = ""
	if registered.SecretID == "" {
		t.Fatal("register verification secret response must contain secret_id")
	}

	waitForSecretConsumed(t, c, sessionID, registered.SecretID, 4*time.Minute)
}

func profileSecretWrapper(secretID, secretName string) string {
	id := strconv.Quote(secretID)
	name := strconv.Quote(secretName)
	return "test -z \"${E2E_PROFILE_SECRET+x}\" || exit 21; " +
		"test ! -e /home/agentapi/.e2e-profile-secret || exit 22; " +
		"for attempt in $(seq 1 90); do " +
		"secret_list=$(ccplant client list-secrets 2>/dev/null) && " +
		"printf '%s' \"$secret_list\" | jq -e --arg id " + id + " --arg name " + name +
		" '.secrets == [{id: $id, name: $name, keys: [\"file-token\", \"token\"]}]' >/dev/null && break; " +
		"unset secret_list; sleep 2; done; " +
		"test \"$attempt\" -lt 90 || exit 23; unset secret_list; " +
		"profile_value=$(ccplant client get-secret " + id + " token 2>/dev/null) || exit 24; " +
		"for attempt in $(seq 1 90); do " +
		"if verification_value=$(ccplant client consume-secret 2>/dev/null); then break; fi; sleep 2; done; " +
		"test \"$attempt\" -lt 90 || exit 25; " +
		"test \"$profile_value\" = \"$verification_value\" || exit 26; " +
		"unset profile_value verification_value; exec {{ .Command }}"
}
