package provisioner

import (
	"encoding/base64"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/takutakahashi/agentapi-proxy/pkg/sessionsettings"
)

func TestRestoreAntigravityAuthBundle(t *testing.T) {
	oldHome := runtimeHome
	runtimeHome = t.TempDir()
	t.Cleanup(func() { runtimeHome = oldHome })
	bundle := portableAntigravityAuthBundle{Version: 1}
	bundle.Files = append(bundle.Files, struct {
		Path    string `json:"path"`
		Content string `json:"content_base64"`
	}{Path: "tokens/profile.json", Content: base64.StdEncoding.EncodeToString([]byte(`{"token":"secret"}`))})
	raw, _ := json.Marshal(bundle)
	files := []sessionsettings.ManagedFile{{Path: filepath.Join(runtimeHome, ".ccplant", "antigravity-auth.json"), Content: string(raw)}}
	if err := restoreAntigravityAuthBundle(files); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(filepath.Join(runtimeHome, ".gemini", "antigravity-cli", "tokens", "profile.json"))
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != `{"token":"secret"}` {
		t.Fatalf("unexpected content: %s", got)
	}
}

func TestRestoreAntigravityAuthBundleRejectsTraversal(t *testing.T) {
	oldHome := runtimeHome
	runtimeHome = t.TempDir()
	t.Cleanup(func() { runtimeHome = oldHome })
	raw := `{"version":1,"files":[{"path":"../escape","content_base64":"eA=="}]}`
	files := []sessionsettings.ManagedFile{{Path: filepath.Join(runtimeHome, ".ccplant", "antigravity-auth.json"), Content: raw}}
	if err := restoreAntigravityAuthBundle(files); err == nil {
		t.Fatal("expected traversal rejection")
	}
}
