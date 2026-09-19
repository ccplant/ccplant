package cmd

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSessionStateCWDPrefersCloneDirectory(t *testing.T) {
	hookCWD := t.TempDir()
	cloneDir := t.TempDir()
	if err := os.Mkdir(filepath.Join(cloneDir, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Chdir(hookCWD)
	t.Setenv("AGENTAPI_CLONE_DIR", cloneDir)

	got, err := sessionStateCWD()
	if err != nil {
		t.Fatal(err)
	}
	if got != cloneDir {
		t.Fatalf("sessionStateCWD() = %q, want %q", got, cloneDir)
	}
}

func TestSessionStateCWDFallsBackOutsideGitClone(t *testing.T) {
	hookCWD := t.TempDir()
	t.Chdir(hookCWD)
	t.Setenv("AGENTAPI_CLONE_DIR", t.TempDir())

	got, err := sessionStateCWD()
	if err != nil {
		t.Fatal(err)
	}
	if got != hookCWD {
		t.Fatalf("sessionStateCWD() = %q, want %q", got, hookCWD)
	}
}

func TestWriteSessionStateVolumeAtomicallyCreatesArchive(t *testing.T) {
	home := t.TempDir()
	cwd := t.TempDir()
	if err := os.WriteFile(filepath.Join(cwd, ".acp-session-id"), []byte("thread-1"), 0o600); err != nil {
		t.Fatal(err)
	}
	// The production archive lives inside the persisted workdir. Packing a
	// non-Git workspace must not include the archive (or its temporary file).
	path := filepath.Join(cwd, ".agentapi", "session-state.tar.zst")
	if err := writeSessionStateVolume(path, "codex-acp", "thread-1", home, cwd); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Size() == 0 || info.Mode().Perm() != 0o600 {
		t.Fatalf("archive size=%d mode=%o", info.Size(), info.Mode().Perm())
	}
	matches, err := filepath.Glob(filepath.Join(filepath.Dir(path), ".session-state-*.tmp"))
	if err != nil || len(matches) != 0 {
		t.Fatalf("temporary archives remain: %v (err=%v)", matches, err)
	}
}
