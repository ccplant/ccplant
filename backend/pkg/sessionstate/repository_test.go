package sessionstate

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func newTestRepository(t *testing.T) string {
	t.Helper()
	repo := filepath.Join(t.TempDir(), "repo")
	if err := os.MkdirAll(repo, 0o755); err != nil {
		t.Fatal(err)
	}
	runTestGit(t, repo, "init")
	runTestGit(t, repo, "config", "user.email", "test@example.com")
	runTestGit(t, repo, "config", "user.name", "Test")
	mustWrite(t, filepath.Join(repo, "tracked.txt"), "base\n")
	runTestGit(t, repo, "add", ".")
	runTestGit(t, repo, "commit", "-m", "base")
	return repo
}

func TestBackupAndRestoreRepositoryKeepsLocalCommitsAndChanges(t *testing.T) {
	repo := newTestRepository(t)
	runTestGit(t, repo, "checkout", "-b", "feature/local-only")
	mustWrite(t, filepath.Join(repo, "tracked.txt"), "committed\n")
	runTestGit(t, repo, "commit", "-am", "local only")
	head := strings.TrimSpace(runTestGit(t, repo, "rev-parse", "HEAD"))
	mustWrite(t, filepath.Join(repo, "tracked.txt"), "working\n")
	mustWrite(t, filepath.Join(repo, "untracked.txt"), "new\n")
	archive := filepath.Join(t.TempDir(), ".agentapi", "repo.tar")

	if err := BackupRepository(repo, archive); err != nil {
		t.Fatal(err)
	}

	restored := filepath.Join(t.TempDir(), "repo")
	ok, err := RestoreRepository(archive, restored)
	if err != nil {
		t.Fatal(err)
	}
	if !ok {
		t.Fatal("RestoreRepository did not restore the archive")
	}
	if got := strings.TrimSpace(runTestGit(t, restored, "rev-parse", "HEAD")); got != head {
		t.Fatalf("HEAD = %s, want %s", got, head)
	}
	if got := strings.TrimSpace(runTestGit(t, restored, "symbolic-ref", "--short", "HEAD")); got != "feature/local-only" {
		t.Fatalf("branch = %s", got)
	}
	assertFile(t, filepath.Join(restored, "tracked.txt"), "working\n")
	assertFile(t, filepath.Join(restored, "untracked.txt"), "new\n")
}

func TestBackupRepositorySkipsUnchangedRepository(t *testing.T) {
	repo := newTestRepository(t)
	archive := filepath.Join(t.TempDir(), "repo.tar")
	if err := BackupRepository(repo, archive); err != nil {
		t.Fatal(err)
	}
	old := time.Unix(1, 0)
	if err := os.Chtimes(archive, old, old); err != nil {
		t.Fatal(err)
	}

	if err := BackupRepository(repo, archive); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(archive)
	if err != nil {
		t.Fatal(err)
	}
	if !info.ModTime().Equal(old) {
		t.Fatal("unchanged repository was archived again")
	}

	mustWrite(t, filepath.Join(repo, "tracked.txt"), "changed\n")
	if err := BackupRepository(repo, archive); err != nil {
		t.Fatal(err)
	}
	info, err = os.Stat(archive)
	if err != nil {
		t.Fatal(err)
	}
	if info.ModTime().Equal(old) {
		t.Fatal("changed repository was not archived again")
	}
}

func TestBackupRepositoryDetectsUntrackedFileContentChange(t *testing.T) {
	repo := newTestRepository(t)
	untracked := filepath.Join(repo, "untracked.txt")
	mustWrite(t, untracked, "first\n")
	archive := filepath.Join(t.TempDir(), "repo.tar")
	if err := BackupRepository(repo, archive); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(archive + ".fingerprint")
	if err != nil {
		t.Fatal(err)
	}

	mustWrite(t, untracked, "second, longer\n")
	if err := BackupRepository(repo, archive); err != nil {
		t.Fatal(err)
	}
	after, err := os.ReadFile(archive + ".fingerprint")
	if err != nil {
		t.Fatal(err)
	}
	if string(before) == string(after) {
		t.Fatal("untracked file change did not refresh the archive")
	}
}

func TestRestoreRepositoryKeepsExistingRepository(t *testing.T) {
	repo := newTestRepository(t)
	archive := filepath.Join(t.TempDir(), "repo.tar")
	if err := BackupRepository(repo, archive); err != nil {
		t.Fatal(err)
	}
	mustWrite(t, filepath.Join(repo, "tracked.txt"), "local\n")

	ok, err := RestoreRepository(archive, repo)
	if err != nil {
		t.Fatal(err)
	}
	if ok {
		t.Fatal("RestoreRepository overwrote an existing repository")
	}
	assertFile(t, filepath.Join(repo, "tracked.txt"), "local\n")
}

func TestRestoreRepositoryWithoutArchive(t *testing.T) {
	ok, err := RestoreRepository(filepath.Join(t.TempDir(), "missing.tar"), filepath.Join(t.TempDir(), "repo"))
	if err != nil {
		t.Fatal(err)
	}
	if ok {
		t.Fatal("RestoreRepository reported a restore without an archive")
	}
}

func TestRestoreRepositoryRemovesPartialExtraction(t *testing.T) {
	repo := newTestRepository(t)
	archive := filepath.Join(t.TempDir(), "repo.tar")
	if err := BackupRepository(repo, archive); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(archive)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(archive, data[:len(data)/3], 0o600); err != nil {
		t.Fatal(err)
	}

	restored := filepath.Join(t.TempDir(), "repo")
	if ok, err := RestoreRepository(archive, restored); err == nil || ok {
		t.Fatalf("RestoreRepository = %v, %v; want an error", ok, err)
	}
	if _, err := os.Stat(restored); !os.IsNotExist(err) {
		t.Fatalf("partial repository was left behind: %v", err)
	}
}
