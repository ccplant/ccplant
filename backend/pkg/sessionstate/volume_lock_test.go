package sessionstate

import (
	"os"
	"path/filepath"
	"testing"
)

func TestRemoveStaleVolumeTempsKeepsUnrelatedFiles(t *testing.T) {
	dir := t.TempDir()
	stale := filepath.Join(dir, ".session-state-123.tmp")
	keep := []string{
		filepath.Join(dir, "session-state.tar.zst"),
		filepath.Join(dir, "notes.tmp"),
		filepath.Join(dir, ".session-state-lock"),
	}
	for _, path := range append([]string{stale}, keep...) {
		if err := os.WriteFile(path, []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if err := RemoveStaleVolumeTemps(dir); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(stale); !os.IsNotExist(err) {
		t.Fatalf("stale temporary archive still present: err=%v", err)
	}
	for _, path := range keep {
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("%s was removed: %v", filepath.Base(path), err)
		}
	}
}

func TestTryRemoveStaleVolumeTempsSkipsWhileBackupHoldsLock(t *testing.T) {
	dir := t.TempDir()
	inFlight := filepath.Join(dir, ".session-state-456.tmp")
	if err := os.WriteFile(inFlight, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	release, err := LockVolume(dir)
	if err != nil {
		t.Fatal(err)
	}
	cleaned, err := TryRemoveStaleVolumeTemps(dir)
	if err != nil || cleaned {
		t.Fatalf("cleaned=%v err=%v, want no cleanup while the lock is held", cleaned, err)
	}
	if _, err := os.Stat(inFlight); err != nil {
		t.Fatalf("in-flight temporary archive was removed: %v", err)
	}
	release()
	cleaned, err = TryRemoveStaleVolumeTemps(dir)
	if err != nil || !cleaned {
		t.Fatalf("cleaned=%v err=%v, want cleanup after the lock is released", cleaned, err)
	}
	if _, err := os.Stat(inFlight); !os.IsNotExist(err) {
		t.Fatalf("stale temporary archive still present: err=%v", err)
	}
}

func TestTryRemoveStaleVolumeTempsIgnoresMissingDirectory(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "missing")
	cleaned, err := TryRemoveStaleVolumeTemps(dir)
	if err != nil || cleaned {
		t.Fatalf("cleaned=%v err=%v", cleaned, err)
	}
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Fatalf("directory was created: err=%v", err)
	}
}
