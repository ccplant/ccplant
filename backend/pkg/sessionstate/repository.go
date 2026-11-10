package sessionstate

import (
	"bytes"
	"crypto/sha256"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

func BackupRepository(repoDir, archivePath string) error {
	if info, err := os.Stat(filepath.Join(repoDir, ".git")); err != nil || !info.IsDir() {
		return nil
	}
	fingerprint, err := repositoryFingerprint(repoDir)
	if err != nil {
		return err
	}
	fingerprintPath := archivePath + ".fingerprint"
	if previous, err := os.ReadFile(fingerprintPath); err == nil && bytes.Equal(previous, fingerprint) {
		if _, err := os.Stat(archivePath); err == nil {
			return nil
		}
	}
	if err := os.MkdirAll(filepath.Dir(archivePath), 0o700); err != nil {
		return err
	}
	if err := os.Remove(fingerprintPath); err != nil && !os.IsNotExist(err) {
		return err
	}
	cmd := exec.Command("tar", "-C", filepath.Dir(repoDir), "-cf", archivePath, filepath.Base(repoDir))
	if out, err := cmd.CombinedOutput(); err != nil {
		_ = os.Remove(archivePath)
		return fmt.Errorf("archive repository: %w: %s", err, strings.TrimSpace(string(out)))
	}
	return os.WriteFile(fingerprintPath, fingerprint, 0o600)
}

func RestoreRepository(archivePath, repoDir string) (bool, error) {
	if _, err := os.Stat(filepath.Join(repoDir, ".git")); err == nil {
		return false, nil
	}
	if _, err := os.Stat(archivePath); os.IsNotExist(err) {
		return false, nil
	} else if err != nil {
		return false, err
	}
	if err := os.MkdirAll(filepath.Dir(repoDir), 0o755); err != nil {
		return false, err
	}
	cmd := exec.Command("tar", "-C", filepath.Dir(repoDir), "-xf", archivePath)
	if out, err := cmd.CombinedOutput(); err != nil {
		_ = os.RemoveAll(repoDir)
		return false, fmt.Errorf("extract repository archive: %w: %s", err, strings.TrimSpace(string(out)))
	}
	if _, err := os.Stat(filepath.Join(repoDir, ".git")); err != nil {
		_ = os.RemoveAll(repoDir)
		return false, fmt.Errorf("repository archive does not contain %s", filepath.Base(repoDir))
	}
	return true, nil
}

func repositoryFingerprint(repoDir string) ([]byte, error) {
	hash := sha256.New()
	for _, args := range [][]string{
		{"rev-parse", "HEAD"},
		{"for-each-ref"},
		{"diff", "--binary", "HEAD"},
	} {
		out, _ := gitOutput(repoDir, args...)
		_, _ = hash.Write(out)
	}
	status, err := gitOutput(repoDir, "status", "--porcelain=v1", "-z", "--untracked-files=all")
	if err != nil {
		return nil, err
	}
	_, _ = hash.Write(status)
	for _, entry := range bytes.Split(status, []byte{0}) {
		if len(entry) < 4 {
			continue
		}
		info, err := os.Lstat(filepath.Join(repoDir, string(entry[3:])))
		if err != nil {
			continue
		}
		_, _ = fmt.Fprintf(hash, "%s %d %d\n", entry[3:], info.Size(), info.ModTime().UnixNano())
	}
	return hash.Sum(nil), nil
}
