package storageattachment

import (
	"context"
	"fmt"
	"path/filepath"
	"sort"
	"strings"

	"github.com/takutakahashi/agentapi-proxy/internal/domain/entities"
	portrepos "github.com/takutakahashi/agentapi-proxy/internal/usecases/ports/repositories"
)

type Request struct {
	Attachments []entities.StorageAttachment
	Scope       entities.ResourceScope
	TeamID      string
	UserID      string
	Pool        string
}

type Resolver struct {
	storages portrepos.StorageRepository
}

func NewResolver(storages portrepos.StorageRepository) *Resolver {
	return &Resolver{storages: storages}
}

func (r *Resolver) Resolve(ctx context.Context, request Request) ([]entities.ResolvedStorageAttachment, error) {
	if len(request.Attachments) == 0 {
		return nil, nil
	}
	if r.storages == nil {
		return nil, fmt.Errorf("storage repository is unavailable")
	}

	seenSources := make(map[string][]entities.StorageAttachment, len(request.Attachments))
	seenPaths := make(map[string]struct{}, len(request.Attachments))
	resolved := make([]entities.ResolvedStorageAttachment, 0, len(request.Attachments))
	for _, attachment := range request.Attachments {
		attachment.StorageID = strings.TrimSpace(attachment.StorageID)
		attachment.StoragePath = strings.Trim(strings.TrimSpace(attachment.StoragePath), "/")
		mountPath, err := normalizeMountPath(attachment.MountPath)
		if attachment.StorageID == "" || err != nil {
			return nil, fmt.Errorf("storage_id and a valid non-root mount_path are required")
		}
		attachment.MountPath = mountPath
		if mountPathReserved(attachment.MountPath) && !attachment.Managed {
			return nil, fmt.Errorf("storage mount path %q is reserved", attachment.MountPath)
		}
		if attachment.StoragePath != "" {
			cleaned := filepath.ToSlash(filepath.Clean(attachment.StoragePath))
			if cleaned == "." || cleaned == ".." || strings.HasPrefix(cleaned, "../") || filepath.IsAbs(attachment.StoragePath) {
				return nil, fmt.Errorf("storage_path must be a normalized relative path")
			}
			attachment.StoragePath = cleaned
		}
		for _, existing := range seenSources[attachment.StorageID] {
			if sourcePathsOverlap(attachment.StoragePath, existing.StoragePath) && !managedNamespaceOverlapAllowed(attachment, existing) {
				return nil, fmt.Errorf("storage path %q overlaps %q for storage %q", attachment.StoragePath, existing.StoragePath, attachment.StorageID)
			}
		}
		for existing := range seenPaths {
			if attachment.MountPath == existing || strings.HasPrefix(attachment.MountPath+"/", existing+"/") || strings.HasPrefix(existing+"/", attachment.MountPath+"/") {
				return nil, fmt.Errorf("storage mount path %q overlaps %q", attachment.MountPath, existing)
			}
		}

		storage, err := r.storages.GetByID(ctx, attachment.StorageID)
		if err != nil {
			return nil, fmt.Errorf("load storage %q: %w", attachment.StorageID, err)
		}
		if storage.Scope() != request.Scope || (storage.Scope() == entities.ScopeUser && storage.OwnerID() != request.UserID) || (storage.Scope() == entities.ScopeTeam && storage.TeamID() != request.TeamID) {
			return nil, fmt.Errorf("storage %q does not belong to the session scope", attachment.StorageID)
		}
		if storage.State() != entities.StorageStateReady {
			return nil, fmt.Errorf("storage %q is not ready", attachment.StorageID)
		}
		if storage.Pool() != "" && request.Pool != "" && storage.Pool() != request.Pool {
			return nil, fmt.Errorf("storage %q requires pool %q, selected pool is %q", attachment.StorageID, storage.Pool(), request.Pool)
		}

		entry := entities.ResolvedStorageAttachment{
			StorageAttachment: attachment, Kind: storage.Kind(), Pool: storage.Pool(),
			Provider: storage.Provider(), Size: storage.Size(), Revision: storage.Revision(),
		}
		if storage.Kind() == entities.StorageKindObjectSnapshot {
			files := storage.Files()
			paths := make([]string, 0, len(files))
			selectedFiles := make(map[string]entities.StorageFile, len(files))
			for filePath, file := range files {
				if !attachmentIncludesRemotePath(attachment, filePath) {
					continue
				}
				if attachment.StoragePath != "" {
					prefix := attachment.StoragePath + "/"
					if !strings.HasPrefix(filePath, prefix) {
						continue
					}
					filePath = strings.TrimPrefix(filePath, prefix)
					file.Path = filePath
				}
				if !file.Deleted {
					paths = append(paths, filePath)
					selectedFiles[filePath] = file
				}
			}
			sort.Strings(paths)
			for _, filePath := range paths {
				entry.Files = append(entry.Files, selectedFiles[filePath])
			}
		}
		seenSources[attachment.StorageID] = append(seenSources[attachment.StorageID], attachment)
		seenPaths[attachment.MountPath] = struct{}{}
		resolved = append(resolved, entry)
	}
	return resolved, nil
}

func normalizeMountPath(value string) (string, error) {
	const sessionHome = "/home/agentapi"
	value = strings.TrimSpace(value)
	for _, segment := range strings.Split(filepath.ToSlash(value), "/") {
		if segment == "." || segment == ".." {
			return "", fmt.Errorf("mount path must not contain traversal segments")
		}
	}
	if value == "" {
		return "", fmt.Errorf("mount path is required")
	}
	if !filepath.IsAbs(value) {
		value = filepath.Join(sessionHome, value)
	}
	value = filepath.Clean(value)
	if value == "/" {
		return "", fmt.Errorf("mount path must not be root")
	}
	return value, nil
}

func sourcePathsOverlap(left, right string) bool {
	return left == "" || right == "" || left == right || strings.HasPrefix(left+"/", right+"/") || strings.HasPrefix(right+"/", left+"/")
}

func managedNamespaceOverlapAllowed(left, right entities.StorageAttachment) bool {
	if left.Managed == right.Managed {
		return false
	}
	managed, regular := left, right
	if !managed.Managed {
		managed, regular = right, left
	}
	return regular.StoragePath == "" && managedNamespace(managed.StoragePath)
}

func managedNamespace(storagePath string) bool {
	return storagePath == "workspace" || strings.HasPrefix(storagePath, "workspace/") || storagePath == "session-state" || strings.HasPrefix(storagePath, "session-state/")
}

func attachmentIncludesRemotePath(attachment entities.StorageAttachment, remotePath string) bool {
	if attachment.Managed || attachment.StoragePath != "" {
		return true
	}
	return !managedNamespace(remotePath)
}

func mountPathReserved(value string) bool {
	reserved := []string{"/bin", "/boot", "/dev", "/etc", "/proc", "/sys", "/usr", "/var/run", "/session-settings", "/credentials", "/opt/ccplant", "/home/agentapi/.codex", "/home/agentapi/.claude", "/home/agentapi/.docker", "/home/agentapi/workdir"}
	for _, prefix := range reserved {
		if value == prefix || (prefix != "/home/agentapi/workdir" && strings.HasPrefix(value, prefix+"/")) {
			return true
		}
	}
	return false
}
