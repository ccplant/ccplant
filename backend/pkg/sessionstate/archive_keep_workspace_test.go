package sessionstate

import (
	"bytes"
	"path/filepath"
	"strings"
	"testing"
)

func TestUnpackKeepingWorkspaceLeavesGitWorkspaceUntouched(t *testing.T) {
	home, cwd := t.TempDir(), t.TempDir()
	runTestGit(t, cwd, "init")
	runTestGit(t, cwd, "config", "user.email", "test@example.com")
	runTestGit(t, cwd, "config", "user.name", "Test")
	mustWrite(t, filepath.Join(cwd, "tracked.txt"), "v1\n")
	runTestGit(t, cwd, "add", ".")
	runTestGit(t, cwd, "commit", "-m", "v1")
	runTestGit(t, cwd, "checkout", "-b", "feature/work")
	mustWrite(t, filepath.Join(cwd, "tracked.txt"), "v1 uncommitted\n")
	id := "55555555-5555-5555-5555-555555555555"
	mustWrite(t, filepath.Join(cwd, ".acp-session-id"), id)
	mustWrite(t, filepath.Join(home, ".claude", "projects", "p", id+".jsonl"), "chat\n")

	var archive bytes.Buffer
	if err := Pack(&archive, "claude-acp", id, home, cwd); err != nil {
		t.Fatal(err)
	}

	mustWrite(t, filepath.Join(cwd, "tracked.txt"), "v2\n")
	runTestGit(t, cwd, "commit", "-am", "v2")
	mustWrite(t, filepath.Join(cwd, "tracked.txt"), "v2 uncommitted\n")
	mustWrite(t, filepath.Join(cwd, "untracked.txt"), "new after snapshot\n")
	head := strings.TrimSpace(runTestGit(t, cwd, "rev-parse", "HEAD"))

	restoredHome := t.TempDir()
	if err := UnpackKeepingWorkspace(&archive, restoredHome, cwd); err != nil {
		t.Fatal(err)
	}

	if got := strings.TrimSpace(runTestGit(t, cwd, "rev-parse", "HEAD")); got != head {
		t.Fatalf("HEAD = %s, want %s", got, head)
	}
	if branch := strings.TrimSpace(runTestGit(t, cwd, "branch", "--show-current")); branch != "feature/work" {
		t.Fatalf("branch = %q, want feature/work", branch)
	}
	assertFile(t, filepath.Join(cwd, "tracked.txt"), "v2 uncommitted\n")
	assertFile(t, filepath.Join(cwd, "untracked.txt"), "new after snapshot\n")
	assertFile(t, filepath.Join(restoredHome, ".claude", "projects", "p", id+".jsonl"), "chat\n")
}

func TestUnpackKeepingWorkspaceLeavesPlainWorkspaceUntouched(t *testing.T) {
	home, cwd := t.TempDir(), t.TempDir()
	id := "66666666-6666-6666-6666-666666666666"
	mustWrite(t, filepath.Join(cwd, ".acp-session-id"), id)
	mustWrite(t, filepath.Join(cwd, "notes.txt"), "v1\n")
	mustWrite(t, filepath.Join(home, ".claude", "projects", "p", id+".jsonl"), "chat\n")

	var archive bytes.Buffer
	if err := Pack(&archive, "claude-acp", id, home, cwd); err != nil {
		t.Fatal(err)
	}
	mustWrite(t, filepath.Join(cwd, "notes.txt"), "v2\n")

	restoredHome := t.TempDir()
	if err := UnpackKeepingWorkspace(&archive, restoredHome, cwd); err != nil {
		t.Fatal(err)
	}

	assertFile(t, filepath.Join(cwd, "notes.txt"), "v2\n")
	assertFile(t, filepath.Join(restoredHome, ".claude", "projects", "p", id+".jsonl"), "chat\n")
}

func TestUnpackStillRestoresGitWorkspace(t *testing.T) {
	home, cwd := t.TempDir(), t.TempDir()
	runTestGit(t, cwd, "init")
	runTestGit(t, cwd, "config", "user.email", "test@example.com")
	runTestGit(t, cwd, "config", "user.name", "Test")
	mustWrite(t, filepath.Join(cwd, "tracked.txt"), "v1\n")
	runTestGit(t, cwd, "add", ".")
	runTestGit(t, cwd, "commit", "-m", "v1")
	id := "77777777-7777-7777-7777-777777777777"
	mustWrite(t, filepath.Join(cwd, ".acp-session-id"), id)
	mustWrite(t, filepath.Join(home, ".claude", "projects", "p", id+".jsonl"), "chat\n")
	snapshotHead := strings.TrimSpace(runTestGit(t, cwd, "rev-parse", "HEAD"))

	var archive bytes.Buffer
	if err := Pack(&archive, "claude-acp", id, home, cwd); err != nil {
		t.Fatal(err)
	}

	mustWrite(t, filepath.Join(cwd, "tracked.txt"), "v2\n")
	runTestGit(t, cwd, "commit", "-am", "v2")

	if err := Unpack(&archive, t.TempDir(), cwd); err != nil {
		t.Fatal(err)
	}

	if got := strings.TrimSpace(runTestGit(t, cwd, "rev-parse", "HEAD")); got != snapshotHead {
		t.Fatalf("HEAD = %s, want snapshot %s", got, snapshotHead)
	}
}
