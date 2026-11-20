package cmd

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
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

func TestBoundedAntigravityError(t *testing.T) {
	message := boundedAntigravityError(errors.New(strings.Repeat("x", 4096)))
	if len(message) != 2048 {
		t.Fatalf("bounded error length = %d", len(message))
	}
}
