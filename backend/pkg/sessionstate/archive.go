package sessionstate

import (
	"archive/tar"
	"compress/gzip"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

const maxRestoredFileBytes = 1 << 30

// Pack writes the ACP state and workspace files required to resume a session.
func Pack(w io.Writer, agentType, sessionID, home, cwd string) error {
	if sessionID == "" {
		return fmt.Errorf("ACP session id is empty")
	}
	gz := gzip.NewWriter(w)
	tw := tar.NewWriter(gz)
	add := func(path, name string) error {
		info, err := os.Stat(path)
		if err != nil {
			if os.IsNotExist(err) {
				return nil
			}
			return err
		}
		if !info.Mode().IsRegular() {
			return nil
		}
		h, err := tar.FileInfoHeader(info, "")
		if err != nil {
			return err
		}
		h.Name = filepath.ToSlash(name)
		h.Mode = int64(info.Mode().Perm())
		if err := tw.WriteHeader(h); err != nil {
			return err
		}
		f, err := os.Open(path)
		if err != nil {
			return err
		}
		_, copyErr := io.Copy(tw, f)
		closeErr := f.Close()
		if copyErr != nil {
			return copyErr
		}
		return closeErr
	}
	if err := add(filepath.Join(cwd, ".acp-session-id"), "cwd/.acp-session-id"); err != nil {
		return err
	}
	// Preserve the complete working tree, including .git metadata, untracked
	// files, and local edits. Symlinks and other special files are skipped so a
	// workspace cannot cause files outside cwd to be read into the snapshot.
	if err := filepath.Walk(cwd, func(path string, info os.FileInfo, err error) error {
		if os.IsNotExist(err) {
			return nil
		}
		if err != nil {
			return err
		}
		if info.IsDir() || !info.Mode().IsRegular() {
			return nil
		}
		rel, err := filepath.Rel(cwd, path)
		if err != nil {
			return err
		}
		if rel == ".acp-session-id" {
			return nil
		}
		return add(path, filepath.Join("cwd", rel))
	}); err != nil {
		return err
	}
	var root string
	switch agentType {
	case "claude-acp":
		root = filepath.Join(home, ".claude", "projects")
	case "codex-acp":
		root = filepath.Join(home, ".codex", "sessions")
	default:
		return fmt.Errorf("unsupported ACP agent type %q", agentType)
	}
	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if os.IsNotExist(err) {
			return nil
		}
		if err != nil {
			return err
		}
		if info.IsDir() || !info.Mode().IsRegular() {
			return nil
		}
		name := info.Name()
		include := agentType == "claude-acp" && (name == sessionID+".jsonl" || strings.Contains(filepath.ToSlash(path), "/"+sessionID+"/"))
		include = include || agentType == "codex-acp" && strings.Contains(name, sessionID) && strings.HasSuffix(name, ".jsonl")
		if !include {
			return nil
		}
		rel, err := filepath.Rel(home, path)
		if err != nil {
			return err
		}
		return add(path, filepath.Join("home", rel))
	})
	if err != nil {
		return err
	}
	if agentType == "codex-acp" {
		// Current Codex app-server resolves thread/resume through its local index.
		// Keep the SQLite database and WAL pair together with the rollout.
		codexHome := filepath.Join(home, ".codex")
		entries, readErr := os.ReadDir(codexHome)
		if readErr != nil && !os.IsNotExist(readErr) {
			return readErr
		}
		for _, entry := range entries {
			name := entry.Name()
			if name != "session_index.jsonl" && (!strings.HasPrefix(name, "state_") || !strings.Contains(name, ".sqlite")) {
				continue
			}
			if err := add(filepath.Join(codexHome, name), filepath.Join("home", ".codex", name)); err != nil {
				return err
			}
		}
	}
	if err := tw.Close(); err != nil {
		return err
	}
	return gz.Close()
}

// Unpack restores a trusted backend snapshot without permitting path traversal.
func Unpack(r io.Reader, home, cwd string) error {
	gz, err := gzip.NewReader(r)
	if err != nil {
		return err
	}
	defer func() { _ = gz.Close() }()
	tr := tar.NewReader(gz)
	for {
		h, err := tr.Next()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}
		if h.Typeflag != tar.TypeReg {
			return fmt.Errorf("unsupported archive entry %q", h.Name)
		}
		clean := filepath.Clean(filepath.FromSlash(h.Name))
		if filepath.IsAbs(clean) || clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
			return fmt.Errorf("unsafe archive entry %q", h.Name)
		}
		var target string
		switch {
		case strings.HasPrefix(clean, "cwd"+string(filepath.Separator)):
			target = filepath.Join(cwd, strings.TrimPrefix(clean, "cwd"+string(filepath.Separator)))
		case strings.HasPrefix(clean, "home"+string(filepath.Separator)):
			target = filepath.Join(home, strings.TrimPrefix(clean, "home"+string(filepath.Separator)))
		default:
			return fmt.Errorf("unexpected archive entry %q", h.Name)
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o700); err != nil {
			return err
		}
		f, err := os.OpenFile(target, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
		if err != nil {
			return err
		}
		_, copyErr := io.Copy(f, io.LimitReader(tr, maxRestoredFileBytes))
		closeErr := f.Close()
		if copyErr != nil {
			return copyErr
		}
		if closeErr != nil {
			return closeErr
		}
		if err := os.Chmod(target, os.FileMode(h.Mode)&0o777); err != nil {
			return err
		}
	}
}
