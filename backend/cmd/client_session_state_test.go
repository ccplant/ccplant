package cmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestBackupSessionStateRejectsCheckpointBeforeRestoreCompletes(t *testing.T) {
	id := "restore-pending-" + t.Name()
	t.Setenv("PROVISIONER_PROXY_URL", "http://127.0.0.1:1")
	t.Setenv("SESSION_STATE_PROXY_URL", "")
	t.Setenv("PROVISIONER_TOKEN", "token")
	t.Setenv("AGENTAPI_SESSION_ID", id)
	t.Setenv("AGENTAPI_SESSION_STATE_VOLUME_PATH", filepath.Join(t.TempDir(), "state.tar.zst"))
	marker := filepath.Join(os.TempDir(), "agentapi-session-restore-complete-"+id)
	_ = os.Remove(marker)
	t.Cleanup(func() { _ = os.Remove(marker) })

	err := runBackupSessionState(nil, nil)
	if err == nil || !strings.Contains(err.Error(), "restore has not completed") {
		t.Fatalf("error = %v, want restore-not-completed rejection", err)
	}
}

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
