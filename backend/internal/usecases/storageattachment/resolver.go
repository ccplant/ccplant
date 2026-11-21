package storageattachment

import (
	"context"
	"crypto/sha256"
	"encoding/base32"
	"encoding/binary"
	"fmt"
	"path/filepath"
	"sort"
	"strings"

	"github.com/takutakahashi/agentapi-proxy/internal/domain/entities"
	portrepos "github.com/takutakahashi/agentapi-proxy/internal/usecases/ports/repositories"
)

type Request struct {
	Attachments []entities.StorageAttachment
	Tags        map[string]string
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
			if cleaned == ".ccplant" || strings.HasPrefix(cleaned, ".ccplant/") {
				return nil, fmt.Errorf("storage_path uses a reserved namespace")
			}
		}
		namespaceID, err := resolveNamespace(&attachment, request.Tags)
		if err != nil {
			return nil, err
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
			NamespaceID: namespaceID, Provider: storage.Provider(), Size: storage.Size(), Revision: storage.Revision(),
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

const (
	maxNamespaceTagKeys       = 8
	maxNamespaceTagKeyBytes   = 128
	maxNamespaceTagValueBytes = 512
	maxNamespaceCanonicalSize = 4 * 1024
)

func resolveNamespace(attachment *entities.StorageAttachment, tags map[string]string) (string, error) {
	if attachment.Namespace == nil {
		return "", nil
	}
	selector := *attachment.Namespace
	selector.TagKeys = append([]string(nil), attachment.Namespace.TagKeys...)
	attachment.Namespace = &selector
	if attachment.Managed {
		return "", fmt.Errorf("managed storage attachments cannot use a namespace selector")
	}
	if attachment.Namespace.Mode != "tags" {
		return "", fmt.Errorf("storage namespace mode must be tags")
	}
	if len(attachment.Namespace.TagKeys) == 0 || len(attachment.Namespace.TagKeys) > maxNamespaceTagKeys {
		return "", fmt.Errorf("storage namespace requires between 1 and %d tag keys", maxNamespaceTagKeys)
	}
	keys := append([]string(nil), selector.TagKeys...)
	for index := range keys {
		keys[index] = strings.TrimSpace(keys[index])
	}
	sort.Strings(keys)
	canonical := make([]byte, 0, 256)
	for index, key := range keys {
		if key == "" || len(key) > maxNamespaceTagKeyBytes {
			return "", fmt.Errorf("storage namespace contains an invalid tag key")
		}
		if index > 0 && key == keys[index-1] {
			return "", fmt.Errorf("storage namespace tag keys must be unique")
		}
		value, exists := tags[key]
		if !exists || strings.TrimSpace(value) == "" {
			return "", fmt.Errorf("storage namespace tag %q is required", key)
		}
		if len(value) > maxNamespaceTagValueBytes {
			return "", fmt.Errorf("storage namespace tag %q exceeds %d bytes", key, maxNamespaceTagValueBytes)
		}
		canonical = appendLengthPrefixed(canonical, key)
		canonical = appendLengthPrefixed(canonical, value)
		if len(canonical) > maxNamespaceCanonicalSize {
			return "", fmt.Errorf("storage namespace tags exceed %d bytes", maxNamespaceCanonicalSize)
		}
	}
	attachment.Namespace.TagKeys = keys
	digest := sha256.Sum256(canonical)
	namespaceID := strings.ToLower(base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(digest[:]))
	parts := []string{attachment.StoragePath, ".ccplant", "namespaces", "v1", namespaceID[:2], namespaceID}
	attachment.StoragePath = strings.TrimLeft(filepath.ToSlash(filepath.Join(parts...)), "/")
	return namespaceID, nil
}

func appendLengthPrefixed(target []byte, value string) []byte {
	var length [4]byte
	binary.BigEndian.PutUint32(length[:], uint32(len(value)))
	target = append(target, length[:]...)
	return append(target, value...)
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
	return !managedNamespace(remotePath) && remotePath != ".ccplant" && !strings.HasPrefix(remotePath, ".ccplant/")
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
