package api

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/ccplant/ccplant/e2e/internal/domain"
	"github.com/ccplant/ccplant/e2e/internal/store"
)

func TestCreateAndListRun(t *testing.T) {
	db, err := store.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	c := &domain.Catalog{Categories: []domain.Category{{ID: "smoke", Description: "スモークテスト"}}, Scenarios: []domain.Scenario{{ID: "api.smoke.health", Category: "smoke", Description: "APIが正常に応答する", Kind: "go-test", Package: "./scenarios/api", Test: "TestHealth", Timeout: "30s"}}}
	server := httptest.NewServer(New(db, c).Handler())
	t.Cleanup(server.Close)
	body := bytes.NewBufferString(`{"scenario_id":"api.smoke.health","environment":"local","parameters":{}}`)
	res, err := http.Post(server.URL+"/api/v1/runs", "application/json", body)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusCreated {
		t.Fatalf("status=%d", res.StatusCode)
	}
	var created domain.Run
	if err := json.NewDecoder(res.Body).Decode(&created); err != nil {
		t.Fatal(err)
	}
	if created.ScenarioDescription != "APIが正常に応答する" || created.CategoryDescription != "スモークテスト" {
		t.Fatalf("metadata not snapshotted: %#v", created)
	}
	req, _ := http.NewRequestWithContext(context.Background(), http.MethodGet, server.URL+"/api/v1/runs?category=smoke", nil)
	listed, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer listed.Body.Close()
	var got struct {
		Runs []domain.Run `json:"runs"`
	}
	if err := json.NewDecoder(listed.Body).Decode(&got); err != nil {
		t.Fatal(err)
	}
	if len(got.Runs) != 1 || got.Runs[0].ID != created.ID {
		t.Fatalf("runs=%#v", got.Runs)
	}
}
