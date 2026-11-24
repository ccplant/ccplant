package sessionstate

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

type magicReader struct {
	magic []byte
	r     io.Reader
}

func newMagicReader(r io.Reader) *magicReader {
	magic := make([]byte, 2)
	n, _ := io.ReadFull(r, magic)
	magic = magic[:n]
	return &magicReader{magic: magic, r: io.MultiReader(bytes.NewReader(magic), r)}
}
func (r *magicReader) Read(p []byte) (int, error) { return r.r.Read(p) }

func restoreGitWorkspace(cwd string, manifest gitManifest, staged, worktree []byte) error {
	if manifest.Version != 1 || manifest.Base == "" {
		return fmt.Errorf("unsupported git snapshot manifest")
	}
	if _, err := os.Stat(filepath.Join(cwd, ".git")); err != nil {
		return fmt.Errorf("git workspace base is unavailable: %w", err)
	}
	if err := runGit(cwd, nil, "fetch", "--depth=1", "origin", manifest.Base); err != nil {
		// The shallow clone often already contains the exact object.
		if _, checkErr := gitOutput(cwd, "cat-file", "-e", manifest.Base+"^{commit}"); checkErr != nil {
			return err
		}
	}
	if err := runGit(cwd, nil, "checkout", "--force", "--detach", manifest.Base); err != nil {
		return err
	}
	if manifest.Branch != "" {
		if err := runGit(cwd, nil, "check-ref-format", "--branch", manifest.Branch); err != nil {
			return fmt.Errorf("invalid git snapshot branch %q: %w", manifest.Branch, err)
		}
		// Recreate the local branch at the captured commit. This also handles
		// branches whose tip only existed in the suspended workspace.
		if err := runGit(cwd, nil, "checkout", "--force", "-B", manifest.Branch, manifest.Base); err != nil {
			return err
		}
	}
	if len(staged) > 0 {
		if err := runGit(cwd, staged, "apply", "--cached", "--binary", "-"); err != nil {
			return err
		}
	}
	if len(worktree) > 0 {
		if err := runGit(cwd, worktree, "apply", "--binary", "-"); err != nil {
			return err
		}
	}
	return nil
}

func runGit(cwd string, stdin []byte, args ...string) error {
	cmd := exec.Command("git", append([]string{"-C", cwd}, args...)...)
	if stdin != nil {
		cmd.Stdin = bytes.NewReader(stdin)
	}
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("git %s: %w: %s", strings.Join(args, " "), err, strings.TrimSpace(string(out)))
	}
	return nil
}
