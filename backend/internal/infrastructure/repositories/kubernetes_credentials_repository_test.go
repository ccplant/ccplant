package repositories

import (
	"context"
	"encoding/json"
	"path/filepath"
	"testing"

	"k8s.io/client-go/kubernetes/fake"

	"github.com/takutakahashi/agentapi-proxy/internal/domain/entities"
	"github.com/takutakahashi/agentapi-proxy/internal/infrastructure/kvstore"
	"github.com/takutakahashi/agentapi-proxy/pkg/sessionsettings"
)

func newCredentialsRepoWithLibSQL(t *testing.T) (*KubernetesCredentialsRepository, func()) {
	t.Helper()
	ctx := context.Background()
	store, err := kvstore.NewLibSQLStore(ctx, "file://"+filepath.Join(t.TempDir(), "creds.db"), "")
	if err != nil {
		t.Fatalf("new libsql store: %v", err)
	}
	client := kvstore.NewKubernetesAdapter(fake.NewSimpleClientset(), store)
	return NewKubernetesCredentialsRepository(client, "default"), func() { _ = store.Close() }
}

func TestKubernetesCredentialsRepository_SaveUpdateCodexAuth(t *testing.T) {
	ctx := context.Background()
	repo, cleanup := newCredentialsRepoWithLibSQL(t)
	defer cleanup()

	// First save (Create path).
	first := entities.NewCredentials("takutakahashi", json.RawMessage(`{"auth_mode":"chatgpt","v":1}`))
	first.SetFileType(sessionsettings.FileTypeCodexAuth)
	if err := repo.Save(ctx, first); err != nil {
		t.Fatalf("initial save: %v", err)
	}

	// Second save of the same file type must Update the existing Secret.
	// This is the case that previously failed with "kv record version conflict"
	// because ResourceVersion was not propagated to the Update call.
	second := entities.NewCredentials("takutakahashi", json.RawMessage(`{"auth_mode":"chatgpt","v":2}`))
	second.SetFileType(sessionsettings.FileTypeCodexAuth)
	if err := repo.Save(ctx, second); err != nil {
		t.Fatalf("update save: %v", err)
	}

	loaded, err := repo.FindByName(ctx, "takutakahashi")
	if err != nil {
		t.Fatalf("find: %v", err)
	}
	var got map[string]any
	for _, f := range loaded.Files() {
		if f.Path == sessionsettings.ManagedFileTypes[sessionsettings.FileTypeCodexAuth] {
			if err := json.Unmarshal([]byte(f.Content), &got); err != nil {
				t.Fatalf("unmarshal: %v", err)
			}
		}
	}
	if got["v"] != float64(2) {
		t.Fatalf("codex auth content = %v, want v=2", got)
	}
}

func TestKubernetesCredentialsRepository_SavePreservesOtherFileTypes(t *testing.T) {
	ctx := context.Background()
	repo, cleanup := newCredentialsRepoWithLibSQL(t)
	defer cleanup()

	codex := entities.NewCredentials("takutakahashi", json.RawMessage(`{"auth_mode":"chatgpt"}`))
	codex.SetFileType(sessionsettings.FileTypeCodexAuth)
	if err := repo.Save(ctx, codex); err != nil {
		t.Fatalf("save codex: %v", err)
	}

	claude := entities.NewCredentials("takutakahashi", json.RawMessage(`{"mcpOAuth":{}}`))
	claude.SetFileType(sessionsettings.FileTypeClaudeCredentials)
	if err := repo.Save(ctx, claude); err != nil {
		t.Fatalf("save claude: %v", err)
	}

	loaded, err := repo.FindByName(ctx, "takutakahashi")
	if err != nil {
		t.Fatalf("find: %v", err)
	}
	paths := map[string]bool{}
	for _, f := range loaded.Files() {
		paths[f.Path] = true
	}
	wantCodex := sessionsettings.ManagedFileTypes[sessionsettings.FileTypeCodexAuth]
	wantClaude := sessionsettings.ManagedFileTypes[sessionsettings.FileTypeClaudeCredentials]
	if !paths[wantCodex] || !paths[wantClaude] {
		t.Fatalf("expected both file types present, got %v", paths)
	}
}
