package mcpserver

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	apiservice "github.com/ccplant/ccplant/e2e/internal/api"
	"github.com/ccplant/ccplant/e2e/internal/catalog"
	"github.com/ccplant/ccplant/e2e/internal/domain"
	"github.com/ccplant/ccplant/e2e/internal/store"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestTools(t *testing.T) {
	dir := t.TempDir()
	catalogPath := filepath.Join(dir, "catalog.yaml")
	catalogYAML := []byte("categories:\n  - id: smoke\n    description: smoke\nscenarios:\n  - id: api.smoke.health\n    category: smoke\n    description: health\n    kind: go-test\n    package: ./scenarios/api\n    test: TestHealth\n    timeout: 30s\n")
	if err := os.WriteFile(catalogPath, catalogYAML, 0o600); err != nil {
		t.Fatal(err)
	}
	loaded, err := catalog.Load(catalogPath)
	if err != nil {
		t.Fatal(err)
	}
	db, err := store.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	api := apiservice.NewReloadable(db, loaded, catalogPath)
	server := New(api, Config{TargetAPIURL: "https://target.example/api/v1", AuthValue: "secret"})
	serverTransport, clientTransport := mcp.NewInMemoryTransports()
	serverSession, err := server.Connect(context.Background(), serverTransport, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { serverSession.Close() })
	client := mcp.NewClient(&mcp.Implementation{Name: "test", Version: "1"}, nil)
	clientSession, err := client.Connect(context.Background(), clientTransport, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { clientSession.Close() })

	started, err := clientSession.CallTool(context.Background(), &mcp.CallToolParams{
		Name: "start_e2e", Arguments: map[string]any{"scenario_id": "api.smoke.health"},
	})
	if err != nil {
		t.Fatal(err)
	}
	encoded, _ := json.Marshal(started)
	if bytes.Contains(encoded, []byte("secret")) {
		t.Fatalf("tool response leaked credential: %s", encoded)
	}

	claimRequest := httptest.NewRequest(http.MethodPost, "/internal/v1/runs/claim", bytes.NewBufferString(`{"worker_id":"worker-1","kinds":["go-test"]}`))
	claimRequest.Header.Set("Content-Type", "application/json")
	claimResponse := httptest.NewRecorder()
	api.Handler().ServeHTTP(claimResponse, claimRequest)
	if claimResponse.Code != http.StatusOK {
		t.Fatalf("claim status=%d body=%s", claimResponse.Code, claimResponse.Body.String())
	}
	var claim struct {
		Target domain.Target `json:"target"`
	}
	if err := json.NewDecoder(claimResponse.Body).Decode(&claim); err != nil {
		t.Fatal(err)
	}
	if claim.Target.APIURL != "https://target.example/api/v1" || claim.Target.AuthType != "bearer" || claim.Target.AuthValue != "secret" {
		t.Fatalf("target=%#v", claim.Target)
	}

	reloaded, err := clientSession.CallTool(context.Background(), &mcp.CallToolParams{Name: "reload_e2e_scenarios"})
	if err != nil {
		t.Fatal(err)
	}
	if reloaded.IsError {
		t.Fatalf("reload result=%#v", reloaded)
	}
}
