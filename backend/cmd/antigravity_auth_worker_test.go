package cmd

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestCollectAntigravityAuthBundleExcludesRuntimeData(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	root := filepath.Join(home, ".gemini", "antigravity-cli")
	if err := os.MkdirAll(filepath.Join(root, "log"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "auth-token.json"), []byte(`{"refresh_token":"secret"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "log", "cli.log"), []byte("noise"), 0o600); err != nil {
		t.Fatal(err)
	}
	raw, err := collectAntigravityAuthBundle()
	if err != nil {
		t.Fatal(err)
	}
	var bundle antigravityAuthBundle
	if err := json.Unmarshal(raw, &bundle); err != nil {
		t.Fatal(err)
	}
	if len(bundle.Files) != 1 || bundle.Files[0].Path != "auth-token.json" {
		t.Fatalf("unexpected bundle: %+v", bundle)
	}
}
