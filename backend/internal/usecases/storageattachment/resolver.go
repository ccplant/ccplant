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

	state := resolutionState{
		seenSources: make(map[string][]entities.StorageAttachment, len(request.Attachments)),
		seenPaths:   make(map[string]struct{}, len(request.Attachments)),
	}
	resolved := make([]entities.ResolvedStorageAttachment, 0, len(request.Attachments))
	for _, attachment := range request.Attachments {
		entry, normalized, err := r.resolveAttachment(ctx, request, attachment, state)
		if err != nil {
			return nil, err
		}
		state.record(normalized)
		resolved = append(resolved, entry)
	}
	return resolved, nil
}

type resolutionState struct {
	seenSources map[string][]entities.StorageAttachment
	seenPaths   map[string]struct{}
}

func (state resolutionState) record(attachment entities.StorageAttachment) {
	state.seenSources[attachment.StorageID] = append(state.seenSources[attachment.StorageID], attachment)
	state.seenPaths[attachment.MountPath] = struct{}{}
}

func (r *Resolver) resolveAttachment(ctx context.Context, request Request, attachment entities.StorageAttachment, state resolutionState) (entities.ResolvedStorageAttachment, entities.StorageAttachment, error) {
	attachment, namespaceID, err := normalizeAttachment(attachment, request.Tags)
	if err != nil {
		return entities.ResolvedStorageAttachment{}, attachment, err
	}
	if err := state.validate(attachment); err != nil {
		return entities.ResolvedStorageAttachment{}, attachment, err
	}
	storage, err := r.storages.GetByID(ctx, attachment.StorageID)
	if err != nil {
		return entities.ResolvedStorageAttachment{}, attachment, fmt.Errorf("load storage %q: %w", attachment.StorageID, err)
	}
	if err := validateStorage(storage, attachment.StorageID, request); err != nil {
		return entities.ResolvedStorageAttachment{}, attachment, err
	}
	entry := entities.ResolvedStorageAttachment{
		StorageAttachment: attachment, Kind: storage.Kind(), Pool: storage.Pool(), NamespaceID: namespaceID,
		Provider: storage.Provider(), Size: storage.Size(), Revision: storage.Revision(),
	}
	if storage.Kind() == entities.StorageKindObjectSnapshot {
		entry.Files = selectStorageFiles(storage.Files(), attachment)
	}
	return entry, attachment, nil
}

func normalizeAttachment(attachment entities.StorageAttachment, tags map[string]string) (entities.StorageAttachment, string, error) {
	attachment.StorageID = strings.TrimSpace(attachment.StorageID)
	attachment.StoragePath = strings.Trim(strings.TrimSpace(attachment.StoragePath), "/")
	mountPath, err := normalizeMountPath(attachment.MountPath)
	if attachment.StorageID == "" || err != nil {
		return attachment, "", fmt.Errorf("storage_id and a valid non-root mount_path are required")
	}
	attachment.MountPath = mountPath
	if mountPathReserved(mountPath) && !attachment.Managed {
		return attachment, "", fmt.Errorf("storage mount path %q is reserved", mountPath)
	}
	if err := normalizeStoragePath(&attachment); err != nil {
		return attachment, "", err
	}
	namespaceID, err := resolveNamespace(&attachment, tags)
	return attachment, namespaceID, err
}

func normalizeStoragePath(attachment *entities.StorageAttachment) error {
	if attachment.StoragePath == "" {
		return nil
	}
	cleaned := filepath.ToSlash(filepath.Clean(attachment.StoragePath))
	if cleaned == "." || cleaned == ".." || strings.HasPrefix(cleaned, "../") || filepath.IsAbs(attachment.StoragePath) {
		return fmt.Errorf("storage_path must be a normalized relative path")
	}
	if cleaned == ".ccplant" || strings.HasPrefix(cleaned, ".ccplant/") {
		return fmt.Errorf("storage_path uses a reserved namespace")
	}
	attachment.StoragePath = cleaned
	return nil
}

func (state resolutionState) validate(attachment entities.StorageAttachment) error {
	for _, existing := range state.seenSources[attachment.StorageID] {
		if sourcePathsOverlap(attachment.StoragePath, existing.StoragePath) && !managedNamespaceOverlapAllowed(attachment, existing) {
			return fmt.Errorf("storage path %q overlaps %q for storage %q", attachment.StoragePath, existing.StoragePath, attachment.StorageID)
		}
	}
	for existing := range state.seenPaths {
		if pathsOverlap(attachment.MountPath, existing) {
			return fmt.Errorf("storage mount path %q overlaps %q", attachment.MountPath, existing)
		}
	}
	return nil
}

func pathsOverlap(left, right string) bool {
	return left == right || strings.HasPrefix(left+"/", right+"/") || strings.HasPrefix(right+"/", left+"/")
}

func validateStorage(storage *entities.Storage, storageID string, request Request) error {
	wrongScope := storage.Scope() != request.Scope || storage.Scope() == entities.ScopeUser && storage.OwnerID() != request.UserID || storage.Scope() == entities.ScopeTeam && storage.TeamID() != request.TeamID
	if wrongScope {
		return fmt.Errorf("storage %q does not belong to the session scope", storageID)
	}
	if storage.State() != entities.StorageStateReady {
		return fmt.Errorf("storage %q is not ready", storageID)
	}
	if storage.Pool() != "" && request.Pool != "" && storage.Pool() != request.Pool {
		return fmt.Errorf("storage %q requires pool %q, selected pool is %q", storageID, storage.Pool(), request.Pool)
	}
	return nil
}

func selectStorageFiles(files map[string]entities.StorageFile, attachment entities.StorageAttachment) []entities.StorageFile {
	selected := make(map[string]entities.StorageFile, len(files))
	paths := make([]string, 0, len(files))
	for filePath, file := range files {
		filePath, file, included := selectStorageFile(filePath, file, attachment)
		if !included || file.Deleted {
			continue
		}
		paths = append(paths, filePath)
		selected[filePath] = file
	}
	sort.Strings(paths)
	result := make([]entities.StorageFile, 0, len(paths))
	for _, filePath := range paths {
		result = append(result, selected[filePath])
	}
	return result
}

func selectStorageFile(filePath string, file entities.StorageFile, attachment entities.StorageAttachment) (string, entities.StorageFile, bool) {
	if !attachmentIncludesRemotePath(attachment, filePath) {
		return filePath, file, false
	}
	if attachment.StoragePath == "" {
		return filePath, file, true
	}
	prefix := attachment.StoragePath + "/"
	if !strings.HasPrefix(filePath, prefix) {
		return filePath, file, false
	}
	filePath = strings.TrimPrefix(filePath, prefix)
	file.Path = filePath
	return filePath, file, true
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
	keys, err := normalizeNamespaceKeys(selector.TagKeys)
	if err != nil {
		return "", err
	}
	canonical, err := canonicalNamespaceTags(keys, tags)
	if err != nil {
		return "", err
	}
	attachment.Namespace.TagKeys = keys
	digest := sha256.Sum256(canonical)
	namespaceID := strings.ToLower(base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(digest[:]))
	parts := []string{attachment.StoragePath, ".ccplant", "namespaces", "v1", namespaceID[:2], namespaceID}
	attachment.StoragePath = strings.TrimLeft(filepath.ToSlash(filepath.Join(parts...)), "/")
	return namespaceID, nil
}

func normalizeNamespaceKeys(tagKeys []string) ([]string, error) {
	keys := append([]string(nil), tagKeys...)
	for index := range keys {
		keys[index] = strings.TrimSpace(keys[index])
	}
	sort.Strings(keys)
	for index, key := range keys {
		if key == "" || len(key) > maxNamespaceTagKeyBytes {
			return nil, fmt.Errorf("storage namespace contains an invalid tag key")
		}
		if index > 0 && key == keys[index-1] {
			return nil, fmt.Errorf("storage namespace tag keys must be unique")
		}
	}
	return keys, nil
}

func canonicalNamespaceTags(keys []string, tags map[string]string) ([]byte, error) {
	canonical := make([]byte, 0, 256)
	for _, key := range keys {
		value, exists := tags[key]
		if !exists || strings.TrimSpace(value) == "" {
			return nil, fmt.Errorf("storage namespace tag %q is required", key)
		}
		if len(value) > maxNamespaceTagValueBytes {
			return nil, fmt.Errorf("storage namespace tag %q exceeds %d bytes", key, maxNamespaceTagValueBytes)
		}
		canonical = appendLengthPrefixed(canonical, key)
		canonical = appendLengthPrefixed(canonical, value)
		if len(canonical) > maxNamespaceCanonicalSize {
			return nil, fmt.Errorf("storage namespace tags exceed %d bytes", maxNamespaceCanonicalSize)
		}
	}
	return canonical, nil
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
