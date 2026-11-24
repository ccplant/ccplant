package provisioner

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

func scanStorageTree(root string) (map[string]storageLocalFile, error) {
	result := make(map[string]storageLocalFile)
	err := filepath.WalkDir(root, func(filePath string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		relative, err := filepath.Rel(root, filePath)
		if err != nil || relative == "." {
			return err
		}
		if entry.IsDir() {
			if entry.Name() == ".ccplant-storage" {
				return filepath.SkipDir
			}
			if filepath.Clean(root) == filepath.Join(runtimeHome, "workdir") && filepath.ToSlash(relative) == ".git/objects" {
				return filepath.SkipDir
			}
			return nil
		}
		relativeSlash := filepath.ToSlash(relative)
		if !managedStoragePathAllowed(root, relativeSlash) {
			return nil
		}
		info, err := entry.Info()
		if err != nil || !info.Mode().IsRegular() || info.Size() > storageSyncMaxFileBytes {
			return err
		}
		data, err := os.ReadFile(filePath)
		if err != nil {
			return err
		}
		digest := sha256.Sum256(data)
		result[filepath.ToSlash(relative)] = storageLocalFile{digest: hex.EncodeToString(digest[:]), mode: uint32(info.Mode().Perm())}
		return nil
	})
	return result, err
}

func managedStoragePathAllowed(root, relative string) bool {
	root = filepath.Clean(root)
	switch root {
	case filepath.Join(runtimeHome, ".codex"):
		return strings.HasPrefix(relative, "sessions/") || relative == "session_index.jsonl" || (strings.HasPrefix(relative, "state_") && strings.Contains(relative, ".sqlite"))
	case filepath.Join(runtimeHome, ".claude"):
		return strings.HasPrefix(relative, "projects/")
	case filepath.Join(runtimeHome, ".session"):
		return relative == "acp-history.jsonl" || relative == "model-connection.json"
	case filepath.Join(runtimeHome, "workdir"):
		return relative != ".git" && !strings.HasPrefix(relative, ".git/objects/")
	default:
		return true
	}
}

func safeStorageTarget(root, relative string) (string, error) {
	if relative == "" || filepath.IsAbs(relative) || strings.Contains(relative, "\\") {
		return "", fmt.Errorf("invalid storage path %q", relative)
	}
	clean := filepath.Clean(filepath.FromSlash(relative))
	if clean == "." || clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("storage path escapes mount: %q", relative)
	}
	target := filepath.Join(root, clean)
	resolvedRoot, err := filepath.Abs(root)
	if err != nil {
		return "", err
	}
	resolvedTarget, err := filepath.Abs(target)
	if err != nil || (resolvedTarget != resolvedRoot && !strings.HasPrefix(resolvedTarget, resolvedRoot+string(filepath.Separator))) {
		return "", fmt.Errorf("storage path escapes mount: %q", relative)
	}
	return resolvedTarget, nil
}
