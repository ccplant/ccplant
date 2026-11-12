package cmd

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

func TestHelperWriteSessionStateVolume(t *testing.T) {
	dir := os.Getenv("BACKUP_HELPER_CWD")
	if dir == "" {
		t.Skip("helper process only")
	}
	path := filepath.Join(dir, ".agentapi", "session-state.tar.zst")
	if err := writeSessionStateVolume(path, "claude-acp", "thread-1", os.Getenv("BACKUP_HELPER_HOME"), dir); err != nil {
		t.Fatal(err)
	}
}

type slowBackup struct {
	home    string
	cwd     string
	big     string
	pattern string
	path    string
	process *exec.Cmd
}

func startSlowBackup(t *testing.T) *slowBackup {
	t.Helper()
	home := t.TempDir()
	cwd := t.TempDir()
	if err := os.WriteFile(filepath.Join(cwd, ".acp-session-id"), []byte("thread-1"), 0o600); err != nil {
		t.Fatal(err)
	}
	big := filepath.Join(cwd, "big.bin")
	f, err := os.Create(big)
	if err != nil {
		t.Fatal(err)
	}
	if err := f.Truncate(64 << 30); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	helper := exec.Command(os.Args[0], "-test.run=^TestHelperWriteSessionStateVolume$")
	helper.Env = append(os.Environ(), "BACKUP_HELPER_CWD="+cwd, "BACKUP_HELPER_HOME="+home)
	if err := helper.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = helper.Process.Kill()
		_ = helper.Wait()
	})
	backup := &slowBackup{
		home:    home,
		cwd:     cwd,
		big:     big,
		pattern: filepath.Join(cwd, ".agentapi", ".session-state-*.tmp"),
		path:    filepath.Join(cwd, ".agentapi", "session-state.tar.zst"),
		process: helper,
	}
	deadline := time.Now().Add(30 * time.Second)
	for len(backup.temps(t)) != 1 {
		if time.Now().After(deadline) {
			t.Fatal("backup helper never created its temporary archive")
		}
		time.Sleep(20 * time.Millisecond)
	}
	return backup
}

func (b *slowBackup) temps(t *testing.T) []string {
	t.Helper()
	matches, err := filepath.Glob(b.pattern)
	if err != nil {
		t.Fatal(err)
	}
	return matches
}

func (b *slowBackup) kill(t *testing.T) {
	t.Helper()
	if err := b.process.Process.Kill(); err != nil {
		t.Fatal(err)
	}
	_ = b.process.Wait()
}

func TestKilledBackupTemporaryArchiveIsRemovedByNextBackup(t *testing.T) {
	backup := startSlowBackup(t)
	backup.kill(t)

	leftover := backup.temps(t)
	if len(leftover) != 1 {
		t.Fatalf("temporary archives after kill = %v, want 1 leftover", leftover)
	}
	if err := os.Remove(backup.big); err != nil {
		t.Fatal(err)
	}
	if err := writeSessionStateVolume(backup.path, "claude-acp", "thread-1", backup.home, backup.cwd); err != nil {
		t.Fatal(err)
	}
	if after := backup.temps(t); len(after) != 0 {
		t.Fatalf("temporary archives after the next successful backup = %v, want none", after)
	}
	if info, err := os.Stat(backup.path); err != nil || info.Size() == 0 {
		t.Fatalf("archive missing after the next backup: %v", err)
	}
}

func TestConcurrentBackupWaitsForRunningBackup(t *testing.T) {
	backup := startSlowBackup(t)
	running := backup.temps(t)

	done := make(chan error, 1)
	go func() {
		done <- writeSessionStateVolume(backup.path, "claude-acp", "thread-1", backup.home, backup.cwd)
	}()
	select {
	case err := <-done:
		t.Fatalf("second backup finished while the first was still running: %v", err)
	case <-time.After(500 * time.Millisecond):
	}
	if during := backup.temps(t); len(during) != 1 || during[0] != running[0] {
		t.Fatalf("temporary archives while both backups run = %v, want only %v", during, running)
	}

	if err := os.Remove(backup.big); err != nil {
		t.Fatal(err)
	}
	backup.kill(t)
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(30 * time.Second):
		t.Fatal("second backup did not finish after the first was killed")
	}
	if after := backup.temps(t); len(after) != 0 {
		t.Fatalf("temporary archives after the second backup = %v, want none", after)
	}
	if info, err := os.Stat(backup.path); err != nil || info.Size() == 0 {
		t.Fatalf("archive missing after the second backup: %v", err)
	}
}
