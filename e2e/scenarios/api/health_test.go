//go:build e2e

package api_test

import (
	"encoding/json"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"
)

func TestHealth(t *testing.T) {
	base := os.Getenv("E2E_TARGET_API_URL")
	if base == "" {
		base = "http://localhost:8080"
	}
	client := &http.Client{Timeout: 10 * time.Second}
	req, _ := http.NewRequest(http.MethodGet, strings.TrimRight(base, "/")+"/health", nil)
	switch os.Getenv("E2E_AUTH_TYPE") {
	case "bearer":
		req.Header.Set("Authorization", "Bearer "+os.Getenv("E2E_AUTH_VALUE"))
	case "api-key":
		req.Header.Set("X-API-Key", os.Getenv("E2E_AUTH_VALUE"))
	}
	res, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		t.Fatalf("status=%d", res.StatusCode)
	}
	var body struct {
		Status string `json:"status"`
	}
	if err := json.NewDecoder(res.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	if body.Status != "ok" {
		t.Fatalf("status body=%q", body.Status)
	}
}
