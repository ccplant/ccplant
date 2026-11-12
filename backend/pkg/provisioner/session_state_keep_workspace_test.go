package provisioner

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/takutakahashi/agentapi-proxy/pkg/sessionstate"
)

func writeKeepWorkspaceFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestRestoreSessionStateKeepsWorkspaceOnSameVolume(t *testing.T) {
	sourceHome := t.TempDir()
	volume := t.TempDir()
	cwd := filepath.Join(volume, "repo")
	writeKeepWorkspaceFile(t, filepath.Join(sourceHome, ".session", "acp-history.jsonl"), "history\n")
	writeKeepWorkspaceFile(t, filepath.Join(cwd, ".acp-session-id"), "thread-1")
	writeKeepWorkspaceFile(t, filepath.Join(cwd, "notes.txt"), "v1\n")

	archivePath := filepath.Join(volume, ".agentapi", "session-state.tar.zst")
	if err := os.MkdirAll(filepath.Dir(archivePath), 0o700); err != nil {
		t.Fatal(err)
	}
	archive, err := os.Create(archivePath)
	if err != nil {
		t.Fatal(err)
	}
	if err := sessionstate.Pack(archive, "codex-acp", "thread-1", sourceHome, cwd); err != nil {
		t.Fatal(err)
	}
	if err := archive.Close(); err != nil {
		t.Fatal(err)
	}
	writeKeepWorkspaceFile(t, filepath.Join(cwd, "notes.txt"), "v2\n")

	oldRuntimeHome := runtimeHome
	runtimeHome = t.TempDir()
	t.Cleanup(func() { runtimeHome = oldRuntimeHome })
	t.Setenv("AGENTAPI_SESSION_STATE_VOLUME_PATH", archivePath)
	t.Setenv("PROVISIONER_PROXY_URL", "http://must-not-be-used.invalid")

	found, err := (&Server{}).restoreSessionState(context.Background(), "session-1", cwd)
	if err != nil {
		t.Fatal(err)
	}
	if !found {
		t.Fatal("volume snapshot was not restored")
	}
	if got, err := os.ReadFile(filepath.Join(cwd, "notes.txt")); err != nil || string(got) != "v2\n" {
		t.Fatalf("workspace file = %q, err=%v, want the newer v2", got, err)
	}
	if got, err := os.ReadFile(filepath.Join(runtimeHome, ".session", "acp-history.jsonl")); err != nil || string(got) != "history\n" {
		t.Fatalf("restored history = %q, err=%v", got, err)
	}
}

func TestRestoreSessionStateRestoresWorkspaceOutsideTheVolume(t *testing.T) {
	sourceHome := t.TempDir()
	sourceCWD := t.TempDir()
	writeKeepWorkspaceFile(t, filepath.Join(sourceHome, ".session", "acp-history.jsonl"), "history\n")
	writeKeepWorkspaceFile(t, filepath.Join(sourceCWD, ".acp-session-id"), "thread-1")
	writeKeepWorkspaceFile(t, filepath.Join(sourceCWD, "notes.txt"), "v1\n")

	volume := t.TempDir()
	archivePath := filepath.Join(volume, ".agentapi", "session-state.tar.zst")
	if err := os.MkdirAll(filepath.Dir(archivePath), 0o700); err != nil {
		t.Fatal(err)
	}
	archive, err := os.Create(archivePath)
	if err != nil {
		t.Fatal(err)
	}
	if err := sessionstate.Pack(archive, "codex-acp", "thread-1", sourceHome, sourceCWD); err != nil {
		t.Fatal(err)
	}
	if err := archive.Close(); err != nil {
		t.Fatal(err)
	}

	oldRuntimeHome := runtimeHome
	runtimeHome = t.TempDir()
	t.Cleanup(func() { runtimeHome = oldRuntimeHome })
	restoreCWD := t.TempDir()
	t.Setenv("AGENTAPI_SESSION_STATE_VOLUME_PATH", archivePath)
	t.Setenv("PROVISIONER_PROXY_URL", "http://must-not-be-used.invalid")

	found, err := (&Server{}).restoreSessionState(context.Background(), "session-1", restoreCWD)
	if err != nil {
		t.Fatal(err)
	}
	if !found {
		t.Fatal("volume snapshot was not restored")
	}
	if got, err := os.ReadFile(filepath.Join(restoreCWD, "notes.txt")); err != nil || string(got) != "v1\n" {
		t.Fatalf("workspace file = %q, err=%v, want it restored from the snapshot", got, err)
	}
}

func TestSessionStateVolumeRoot(t *testing.T) {
	t.Run("no volume backend", func(t *testing.T) {
		t.Setenv("AGENTAPI_SESSION_STATE_VOLUME_PATH", "")
		if got := sessionStateVolumeRoot(); got != "" {
			t.Fatalf("root = %q, want empty", got)
		}
	})
	t.Run("no snapshot on the volume yet", func(t *testing.T) {
		t.Setenv("AGENTAPI_SESSION_STATE_VOLUME_PATH", filepath.Join(t.TempDir(), ".agentapi", "session-state.tar.zst"))
		if got := sessionStateVolumeRoot(); got != "" {
			t.Fatalf("root = %q, want empty", got)
		}
	})
	t.Run("snapshot outside the workspace layout", func(t *testing.T) {
		archivePath := filepath.Join(t.TempDir(), "session-state.tar.zst")
		writeKeepWorkspaceFile(t, archivePath, "snapshot")
		t.Setenv("AGENTAPI_SESSION_STATE_VOLUME_PATH", archivePath)
		if got := sessionStateVolumeRoot(); got != "" {
			t.Fatalf("root = %q, want empty", got)
		}
	})
	t.Run("snapshot on the volume", func(t *testing.T) {
		volume := t.TempDir()
		archivePath := filepath.Join(volume, ".agentapi", "session-state.tar.zst")
		writeKeepWorkspaceFile(t, archivePath, "snapshot")
		t.Setenv("AGENTAPI_SESSION_STATE_VOLUME_PATH", archivePath)
		if got := sessionStateVolumeRoot(); got != volume {
			t.Fatalf("root = %q, want %q", got, volume)
		}
	})
}
