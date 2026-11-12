package cmd

import (
	"archive/tar"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/klauspost/compress/zstd"
)

func volumeArchiveHas(t *testing.T, path, name string) bool {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	zr, err := zstd.NewReader(f)
	if err != nil {
		t.Fatal(err)
	}
	defer zr.Close()
	tr := tar.NewReader(zr)
	for {
		h, err := tr.Next()
		if err == io.EOF {
			return false
		}
		if err != nil {
			t.Fatal(err)
		}
		if h.Name == name {
			return true
		}
	}
}

func TestVolumeHoldsWorkspace(t *testing.T) {
	cases := []struct {
		archive, cwd string
		want         bool
	}{
		{"/home/agentapi/workdir/.agentapi/session-state.tar.zst", "/home/agentapi/workdir/repo", true},
		{"/home/agentapi/workdir/.agentapi/session-state.tar.zst", "/home/agentapi/workdir", true},
		{"/home/agentapi/workdir/.agentapi/session-state.tar.zst", "/home/agentapi/other", false},
		{"/home/agentapi/workdir/.agentapi/session-state.tar.zst", "/home/agentapi/workdir-old", false},
		{"/state/session-state.tar.zst", "/state/repo", false},
	}
	for _, c := range cases {
		if got := volumeHoldsWorkspace(c.archive, c.cwd); got != c.want {
			t.Errorf("volumeHoldsWorkspace(%q, %q) = %v, want %v", c.archive, c.cwd, got, c.want)
		}
	}
}

func TestWriteSessionStateVolumeChoosesWorkspaceEntries(t *testing.T) {
	cases := []struct {
		name          string
		cwdOnVolume   bool
		templateID    string
		wantWorkspace bool
	}{
		{name: "workspace on the volume", cwdOnVolume: true, wantWorkspace: false},
		{name: "template snapshot", cwdOnVolume: true, templateID: "tpl_1", wantWorkspace: true},
		{name: "workspace outside the volume", cwdOnVolume: false, wantWorkspace: true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Setenv("AGENTAPI_SESSION_TEMPLATE_SNAPSHOT_ID", c.templateID)
			home, volume := t.TempDir(), t.TempDir()
			cwd := filepath.Join(volume, "repo")
			if !c.cwdOnVolume {
				cwd = t.TempDir()
			}
			if err := os.MkdirAll(cwd, 0o700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(cwd, ".acp-session-id"), []byte("thread-1"), 0o600); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(cwd, "notes.txt"), []byte(strings.Repeat("n", 1024)), 0o600); err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(volume, ".agentapi", "session-state.tar.zst")
			if err := writeSessionStateVolume(path, "codex-acp", "thread-1", home, cwd); err != nil {
				t.Fatal(err)
			}
			if !volumeArchiveHas(t, path, "cwd/.acp-session-id") {
				t.Fatal("archive is missing cwd/.acp-session-id")
			}
			if got := volumeArchiveHas(t, path, "cwd/notes.txt"); got != c.wantWorkspace {
				t.Fatalf("archive has cwd/notes.txt = %v, want %v", got, c.wantWorkspace)
			}
		})
	}
}
