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
	body := bytes.NewBufferString(`{"scenario_id":"api.smoke.health","environment":"local","parameters":{},"target":{"api_url":"https://ccplant.example.com","auth_type":"bearer","auth_value":"secret-token"}}`)
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
	encoded, _ := json.Marshal(created)
	if bytes.Contains(encoded, []byte("secret-token")) {
		t.Fatal("credential leaked in run response")
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
	claimBody := bytes.NewBufferString(`{"worker_id":"worker-1","kinds":["go-test"]}`)
	claimed, err := http.Post(server.URL+"/internal/v1/runs/claim", "application/json", claimBody)
	if err != nil {
		t.Fatal(err)
	}
	defer claimed.Body.Close()
	var claim struct {
		Target domain.Target `json:"target"`
	}
	if err := json.NewDecoder(claimed.Body).Decode(&claim); err != nil {
		t.Fatal(err)
	}
	if claim.Target.AuthValue != "secret-token" {
		t.Fatal("credential was not delivered to worker")
	}
	after, err := db.GetRun(context.Background(), created.ID)
	if err != nil {
		t.Fatal(err)
	}
	persisted, _ := json.Marshal(after)
	if bytes.Contains(persisted, []byte("secret-token")) {
		t.Fatal("credential persisted in store")
	}
}
