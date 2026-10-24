package provisioner

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/takutakahashi/agentapi-proxy/pkg/sessionsettings"
)

const storageSyncInterval = 10 * time.Second
const storageSyncMaxFileBytes = 64 << 20

type storageManifestResponse struct {
	Revision int64                 `json:"revision"`
	Files    []storageManifestFile `json:"files"`
}

type storageManifestFile struct {
	Path      string    `json:"path"`
	Digest    string    `json:"digest,omitempty"`
	Size      int64     `json:"size,omitempty"`
	Mode      uint32    `json:"mode,omitempty"`
	Deleted   bool      `json:"deleted,omitempty"`
	Revision  int64     `json:"revision"`
	UpdatedAt time.Time `json:"updated_at"`
}

type storageLocalFile struct {
	digest string
	mode   uint32
}

func storageSyncConnection(settings *sessionsettings.SessionSettings) (string, string) {
	endpoint := ""
	if settings.ParentRuntime != nil {
		endpoint = settings.ParentRuntime.Endpoint
	}
	if endpoint == "" {
		endpoint = os.Getenv("AGENTAPI_PROXY_ENDPOINT")
	}
	if endpoint == "" {
		endpoint = os.Getenv("PROVISIONER_PROXY_URL")
	}
	apiKey := ""
	if settings.Env != nil {
		apiKey = settings.Env["AGENTAPI_KEY"]
	}
	return strings.TrimRight(endpoint, "/"), apiKey
}

func restoreObjectStorages(ctx context.Context, settings *sessionsettings.SessionSettings) error {
	return restoreObjectStoragesMatching(ctx, settings, nil)
}

func restoreObjectStoragesMatching(ctx context.Context, settings *sessionsettings.SessionSettings, managed *bool) error {
	endpoint, apiKey := storageSyncConnection(settings)
	for _, storage := range settings.Storages {
		if storage.Kind != "object_snapshot" {
			continue
		}
		if managed != nil && storage.Managed != *managed {
			continue
		}
		if endpoint == "" || apiKey == "" {
			return fmt.Errorf("storage %s requires parent endpoint and API key", storage.StorageID)
		}
		if err := os.MkdirAll(storage.MountPath, 0o755); err != nil {
			return fmt.Errorf("create storage mount %s: %w", storage.MountPath, err)
		}
		for _, file := range storage.Files {
			if err := restoreStorageFile(ctx, http.DefaultClient, endpoint, apiKey, storage, file); err != nil {
				return err
			}
		}
	}
	return nil
}

func restoreStorageFile(ctx context.Context, client *http.Client, endpoint, apiKey string, storage sessionsettings.StorageAttachment, file sessionsettings.StorageFile) error {
	if storage.Managed && !managedStoragePathAllowed(storage.MountPath, file.Path) {
		return nil
	}
	target, err := safeStorageTarget(storage.MountPath, file.Path)
	if err != nil {
		return err
	}
	body, err := storageRequest(ctx, client, endpoint, apiKey, http.MethodGet, storage.StorageID, storageRemotePath(storage, file.Path), nil, 0)
	if err != nil {
		return fmt.Errorf("restore storage %s path %s: %w", storage.StorageID, file.Path, err)
	}
	defer func() { _ = body.Close() }()
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(target), ".ccplant-storage-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer func() { _ = os.Remove(tmpName) }()
	hash := sha256.New()
	if _, err := io.Copy(io.MultiWriter(tmp, hash), io.LimitReader(body, storageSyncMaxFileBytes+1)); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if file.Digest != "" && hex.EncodeToString(hash.Sum(nil)) != file.Digest {
		return fmt.Errorf("storage file checksum mismatch: %s", file.Path)
	}
	mode := os.FileMode(file.Mode)
	if mode == 0 {
		mode = 0o644
	}
	if err := os.Chmod(tmpName, mode.Perm()); err != nil {
		return err
	}
	return os.Rename(tmpName, target)
}

func (s *Server) runStorageSync(ctx context.Context, settings *sessionsettings.SessionSettings) {
	endpoint, apiKey := storageSyncConnection(settings)
	if endpoint == "" || apiKey == "" {
		if len(settings.Storages) > 0 {
			log.Printf("[STORAGE_SYNC] Parent endpoint or API key missing; object storage sync disabled")
		}
		return
	}
	client := &http.Client{Timeout: 60 * time.Second}
	for _, storage := range settings.Storages {
		if storage.Kind != "object_snapshot" {
			continue
		}
		storage := storage
		s.agentWorkers.Add(1)
		go func() {
			defer s.agentWorkers.Done()
			runOneStorageSync(ctx, client, endpoint, apiKey, storage)
		}()
	}
}

func runOneStorageSync(ctx context.Context, client *http.Client, endpoint, apiKey string, storage sessionsettings.StorageAttachment) {
	last, err := scanStorageTree(storage.MountPath)
	if err != nil {
		log.Printf("[STORAGE_SYNC] Initial scan failed for %s: %v", storage.StorageID, err)
		return
	}
	knownRevision := storage.Revision
	syncOnce := func(syncCtx context.Context) {
		manifest, listErr := fetchStorageManifest(syncCtx, client, endpoint, apiKey, storage, knownRevision)
		if listErr != nil {
			log.Printf("[STORAGE_SYNC] Manifest fetch failed for %s: %v", storage.StorageID, listErr)
			return
		}
		current, scanErr := scanStorageTree(storage.MountPath)
		if scanErr != nil {
			log.Printf("[STORAGE_SYNC] Scan failed for %s: %v", storage.StorageID, scanErr)
			return
		}
		remoteTouched := make(map[string]struct{})
		for _, remote := range manifest.Files {
			if remote.Revision <= knownRevision {
				continue
			}
			previous, existed := last[remote.Path]
			local, existsNow := current[remote.Path]
			dirty := existed != existsNow || (existed && previous.digest != local.digest)
			if dirty {
				continue
			}
			target, targetErr := safeStorageTarget(storage.MountPath, remote.Path)
			if targetErr != nil {
				continue
			}
			if remote.Deleted {
				_ = os.Remove(target)
			} else {
				file := sessionsettings.StorageFile{Path: remote.Path, Digest: remote.Digest, Size: remote.Size, Mode: remote.Mode, Revision: remote.Revision}
				if restoreErr := restoreStorageFile(syncCtx, client, endpoint, apiKey, storage, file); restoreErr != nil {
					log.Printf("[STORAGE_SYNC] Pull failed for %s/%s: %v", storage.StorageID, remote.Path, restoreErr)
					continue
				}
			}
			remoteTouched[remote.Path] = struct{}{}
		}
		if len(remoteTouched) > 0 {
			current, _ = scanStorageTree(storage.MountPath)
			for filePath := range remoteTouched {
				if value, ok := current[filePath]; ok {
					last[filePath] = value
				} else {
					delete(last, filePath)
				}
			}
		}
		knownRevision = manifest.Revision
		if storage.ReadOnly {
			return
		}
		current, scanErr = scanStorageTree(storage.MountPath)
		if scanErr != nil {
			return
		}
		paths := make([]string, 0, len(current))
		for filePath := range current {
			paths = append(paths, filePath)
		}
		sort.Strings(paths)
		for _, filePath := range paths {
			file := current[filePath]
			if previous, ok := last[filePath]; ok && previous == file {
				continue
			}
			data, readErr := os.ReadFile(filepath.Join(storage.MountPath, filepath.FromSlash(filePath)))
			if readErr != nil || len(data) > storageSyncMaxFileBytes {
				continue
			}
			body, putErr := storageRequest(syncCtx, client, endpoint, apiKey, http.MethodPut, storage.StorageID, storageRemotePath(storage, filePath), data, file.mode)
			if body != nil {
				_ = body.Close()
			}
			if putErr != nil {
				log.Printf("[STORAGE_SYNC] Push failed for %s/%s: %v", storage.StorageID, filePath, putErr)
				continue
			}
			last[filePath] = file
		}
		for filePath := range last {
			if _, ok := current[filePath]; ok {
				continue
			}
			body, deleteErr := storageRequest(syncCtx, client, endpoint, apiKey, http.MethodDelete, storage.StorageID, storageRemotePath(storage, filePath), nil, 0)
			if body != nil {
				_ = body.Close()
			}
			if deleteErr == nil {
				delete(last, filePath)
			}
		}
	}

	ticker := time.NewTicker(storageSyncInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			flushCtx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
			syncOnce(flushCtx)
			cancel()
			return
		case <-ticker.C:
			syncOnce(ctx)
		}
	}
}

func fetchStorageManifest(ctx context.Context, client *http.Client, endpoint, apiKey string, storage sessionsettings.StorageAttachment, _ int64) (*storageManifestResponse, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint+"/storages/"+url.PathEscape(storage.StorageID)+"/files?include_deleted=true", nil)
	if err != nil {
		return nil, err
	}
	request.Header.Set("Authorization", "Bearer "+apiKey)
	response, err := client.Do(request)
	if err != nil {
		return nil, err
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode/100 != 2 {
		message, _ := io.ReadAll(io.LimitReader(response.Body, 4096))
		return nil, fmt.Errorf("HTTP %d: %s", response.StatusCode, strings.TrimSpace(string(message)))
	}
	var manifest storageManifestResponse
	if err := json.NewDecoder(response.Body).Decode(&manifest); err != nil {
		return nil, err
	}
	if storage.StoragePath != "" {
		prefix := strings.Trim(storage.StoragePath, "/") + "/"
		filtered := manifest.Files[:0]
		for _, file := range manifest.Files {
			if strings.HasPrefix(file.Path, prefix) {
				file.Path = strings.TrimPrefix(file.Path, prefix)
				filtered = append(filtered, file)
			}
		}
		manifest.Files = filtered
	}
	return &manifest, nil
}

func storageRemotePath(storage sessionsettings.StorageAttachment, filePath string) string {
	if storage.StoragePath == "" {
		return filePath
	}
	return strings.Trim(storage.StoragePath, "/") + "/" + strings.TrimLeft(filePath, "/")
}

func storageRequest(ctx context.Context, client *http.Client, endpoint, apiKey, method, storageID, filePath string, content []byte, mode uint32) (io.ReadCloser, error) {
	segments := strings.Split(filepath.ToSlash(filePath), "/")
	for index := range segments {
		segments[index] = url.PathEscape(segments[index])
	}
	requestURL := endpoint + "/storages/" + url.PathEscape(storageID) + "/files/" + strings.Join(segments, "/")
	if method == http.MethodPut {
		requestURL += "?mode=" + strconv.FormatUint(uint64(mode), 8)
	}
	request, err := http.NewRequestWithContext(ctx, method, requestURL, bytes.NewReader(content))
	if err != nil {
		return nil, err
	}
	request.Header.Set("Authorization", "Bearer "+apiKey)
	if method == http.MethodPut {
		request.Header.Set("Content-Type", "application/octet-stream")
	}
	response, err := client.Do(request)
	if err != nil {
		return nil, err
	}
	if response.StatusCode/100 != 2 {
		message, _ := io.ReadAll(io.LimitReader(response.Body, 4096))
		_ = response.Body.Close()
		return nil, fmt.Errorf("HTTP %d: %s", response.StatusCode, strings.TrimSpace(string(message)))
	}
	return response.Body, nil
}

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
