//go:build e2e

package api_test

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"net/http"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/ccplant/ccplant/e2e/pkg/apitest"
)

func TestConsumeSecret(t *testing.T) {
	if !strings.EqualFold(os.Getenv("E2E_PARAM_LIVE_SESSION"), "true") {
		t.Skip("set E2E_PARAM_LIVE_SESSION=true to run the resource-consuming consume-secret scenario")
	}

	pool := strings.TrimSpace(os.Getenv("E2E_PARAM_POOL"))
	if pool == "" {
		pool = "fly-dev"
	}
	agentType := strings.TrimSpace(os.Getenv("E2E_PARAM_AGENT_TYPE"))
	if agentType == "" {
		agentType = "codex-acp"
	}

	secretValue := consumeSecretValue(t)

	c := client(t)
	name := apitest.UniqueName()
	var profile struct {
		ID string `json:"id"`
	}
	request(t, c, http.MethodPost, "/session-profiles", map[string]any{
		"name": name + "-profile", "description": "E2E consume-secret profile; automatically deleted",
		"scope": "user", "is_default": false,
		"config": map[string]any{
			"pool":                     pool,
			"command_wrapper_template": consumeSecretWrapper(),
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
		t.Fatal("register secret response must contain secret_id")
	}

	waitForSecretConsumed(t, c, sessionID, registered.SecretID, 4*time.Minute)
}

func consumeSecretValue(t *testing.T) string {
	t.Helper()
	var value [32]byte
	if _, err := rand.Read(value[:]); err != nil {
		t.Fatal("generate one-time secret")
	}
	return hex.EncodeToString(value[:])
}

func consumeSecretWrapper() string {
	return "for attempt in $(seq 1 90); do " +
		"if secret_value=$(ccplant client consume-secret 2>/dev/null); then " +
		"unset secret_value; break; fi; sleep 2; done; exec {{ .Command }}"
}

func waitForSecretConsumed(t *testing.T, c *apitest.Client, sessionID, secretID string, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for {
		var result struct {
			Secrets []struct {
				SecretID string `json:"secret_id"`
				Status   string `json:"status"`
			} `json:"secrets"`
		}
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		err := c.Do(ctx, http.MethodGet, "/sessions/"+url.PathEscape(sessionID)+"/secrets", nil, &result, http.StatusOK)
		cancel()
		if err == nil {
			for _, secret := range result.Secrets {
				if secret.SecretID == secretID && secret.Status == "consumed" {
					return
				}
			}
		}
		if time.Now().Add(2 * time.Second).After(deadline) {
			t.Fatalf("session %s did not consume one-time secret %s", sessionID, secretID)
		}
		time.Sleep(2 * time.Second)
	}
}
