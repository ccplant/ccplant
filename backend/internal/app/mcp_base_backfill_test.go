package app

import (
	"context"
	"encoding/json"
	"path/filepath"
	"testing"

	"github.com/takutakahashi/agentapi-proxy/internal/infrastructure/kvstore"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
)

func TestBackfillLegacyMCPServersBaseIntoLibSQL(t *testing.T) {
	ctx := context.Background()
	const namespace = "app"
	store, err := kvstore.NewLibSQLStore(ctx, "file://"+filepath.Join(t.TempDir(), "settings.db"), "")
	if err != nil {
		t.Fatal(err)
	}
	client := kvstore.NewKubernetesAdapter(fake.NewSimpleClientset(), store)
	legacyData := []byte(`{"mcpServers":{"legacy":{"type":"http","url":"https://legacy.example/mcp"},"shared":{"type":"http","url":"https://legacy.example/shared"}}}`)
	for _, secret := range []*corev1.Secret{
		{
			ObjectMeta: metav1.ObjectMeta{Name: "agentapi-settings-base", Namespace: namespace},
			Data:       map[string][]byte{"settings.json": []byte(`{"env_vars":{"KEEP":"value"},"mcp_servers":{"shared":{"type":"http","url":"https://settings.example/shared"}}}`)},
		},
	} {
		if _, err := client.CoreV1().Secrets(namespace).Create(ctx, secret, metav1.CreateOptions{}); err != nil {
			t.Fatal(err)
		}
	}

	// The legacy Secret is deliberately absent from libSQL. Its mounted data is
	// the only source, matching a Kubernetes-only mcp-servers-base deployment.
	changed, err := backfillLegacyMCPServersBaseData(ctx, client, namespace, "mcp-servers-base", "agentapi-settings-base", legacyData)
	if err != nil || !changed {
		t.Fatalf("first backfill = (%t, %v), want changed", changed, err)
	}
	destination, err := client.CoreV1().Secrets(namespace).Get(ctx, "agentapi-settings-base", metav1.GetOptions{})
	if err != nil {
		t.Fatal(err)
	}
	var document struct {
		EnvVars    map[string]string                     `json:"env_vars"`
		MCPServers map[string]map[string]json.RawMessage `json:"mcp_servers"`
	}
	if err := json.Unmarshal(destination.Data["settings.json"], &document); err != nil {
		t.Fatal(err)
	}
	if document.EnvVars["KEEP"] != "value" || len(document.MCPServers) != 2 {
		t.Fatalf("backfilled document = %#v", document)
	}
	var sharedURL string
	if err := json.Unmarshal(document.MCPServers["shared"]["url"], &sharedURL); err != nil {
		t.Fatal(err)
	}
	if sharedURL != "https://settings.example/shared" {
		t.Fatalf("shared URL = %q", sharedURL)
	}

	changed, err = backfillLegacyMCPServersBaseData(ctx, client, namespace, "mcp-servers-base", "agentapi-settings-base", legacyData)
	if err != nil || changed {
		t.Fatalf("second backfill = (%t, %v), want unchanged", changed, err)
	}
}

func TestBackfillLegacyMCPServersBaseCreatesUnifiedSecret(t *testing.T) {
	ctx := context.Background()
	client := fake.NewSimpleClientset(&corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: "mcp-servers-base", Namespace: "app"},
		Data:       map[string][]byte{"mcp-servers.json": []byte(`{"mcpServers":{"global":{"type":"stdio","command":"tool"}}}`)},
	})

	changed, err := backfillLegacyMCPServersBase(ctx, client, "app", "mcp-servers-base", "agentapi-settings-base")
	if err != nil || !changed {
		t.Fatalf("backfill = (%t, %v)", changed, err)
	}
	destination, err := client.CoreV1().Secrets("app").Get(ctx, "agentapi-settings-base", metav1.GetOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if destination.Labels["agentapi.proxy/settings"] != "true" {
		t.Fatalf("labels = %#v", destination.Labels)
	}
}
