package provisioner

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/takutakahashi/agentapi-proxy/pkg/sessionsettings"
	closeutils "github.com/takutakahashi/agentapi-proxy/pkg/utils"
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

type storageConnection struct {
	client           *http.Client
	endpoint, apiKey string
}

type storageFileTransfer struct {
	connection storageConnection
	storage    sessionsettings.StorageAttachment
	file       sessionsettings.StorageFile
}

type storageHTTPRequest struct {
	connection        storageConnection
	method, storageID string
	filePath          string
	content           []byte
	mode              uint32
}

func (s *Server) storageSyncConnection(settings *sessionsettings.SessionSettings) (string, string) {
	s.mu.RLock()
	endpoint, apiKey := s.brokerURL, s.brokerKey
	s.mu.RUnlock()

	// The provision payload's API key is captured by the provisioner before it
	// is removed from settings.Env. Storage synchronization runs afterwards, so
	// it must use that in-memory credential instead of requiring the key to be
	// mounted into the agent environment.
	if endpoint != "" && apiKey != "" {
		return strings.TrimRight(endpoint, "/"), apiKey
	}

	endpoint = ""
	if settings.ParentRuntime != nil {
		endpoint = settings.ParentRuntime.Endpoint
	}
	if endpoint == "" {
		endpoint = os.Getenv("AGENTAPI_PROXY_ENDPOINT")
	}
	if endpoint == "" {
		endpoint = os.Getenv("PROVISIONER_PROXY_URL")
	}
	apiKey = ""
	if settings.Env != nil {
		apiKey = settings.Env["AGENTAPI_KEY"]
	}
	return strings.TrimRight(endpoint, "/"), apiKey
}

func (s *Server) restoreObjectStorages(ctx context.Context, settings *sessionsettings.SessionSettings) error {
	return s.restoreObjectStoragesMatching(ctx, settings, nil)
}

func (s *Server) restoreObjectStoragesMatching(ctx context.Context, settings *sessionsettings.SessionSettings, managed *bool) error {
	endpoint, apiKey := s.storageSyncConnection(settings)
	connection := storageConnection{client: http.DefaultClient, endpoint: endpoint, apiKey: apiKey}
	for _, storage := range settings.Storages {
		if !storageMatchesRestore(storage, managed) {
			continue
		}
		if endpoint == "" || apiKey == "" {
			return fmt.Errorf("storage %s requires parent endpoint and API key", storage.StorageID)
		}
		if err := os.MkdirAll(storage.MountPath, 0o755); err != nil {
			return fmt.Errorf("create storage mount %s: %w", storage.MountPath, err)
		}
		for _, file := range storage.Files {
			if err := restoreStorageFile(ctx, storageFileTransfer{connection: connection, storage: storage, file: file}); err != nil {
				return err
			}
		}
	}
	return nil
}

func storageMatchesRestore(storage sessionsettings.StorageAttachment, managed *bool) bool {
	return storage.Kind == "object_snapshot" && (managed == nil || storage.Managed == *managed)
}

func restoreStorageFile(ctx context.Context, transfer storageFileTransfer) (returnErr error) {
	storage, file := transfer.storage, transfer.file
	if storage.Managed && !managedStoragePathAllowed(storage.MountPath, file.Path) {
		return nil
	}
	target, err := safeStorageTarget(storage.MountPath, file.Path)
	if err != nil {
		return err
	}
	body, err := storageRequest(ctx, storageHTTPRequest{connection: transfer.connection, method: http.MethodGet, storageID: storage.StorageID, filePath: storageRemotePath(storage, file.Path)})
	if err != nil {
		return fmt.Errorf("restore storage %s path %s: %w", storage.StorageID, file.Path, err)
	}
	defer func() { returnErr = errors.Join(returnErr, body.Close()) }()
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(target), ".ccplant-storage-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer func() {
		if removeErr := os.Remove(tmpName); removeErr != nil && !os.IsNotExist(removeErr) {
			log.Printf("[STORAGE_SYNC] Remove temporary file %s: %v", tmpName, removeErr)
		}
	}()
	hash := sha256.New()
	if _, err := io.Copy(io.MultiWriter(tmp, hash), io.LimitReader(body, storageSyncMaxFileBytes+1)); err != nil {
		return errors.Join(err, tmp.Close())
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
	endpoint, apiKey := s.storageSyncConnection(settings)
	if endpoint == "" || apiKey == "" {
		if len(settings.Storages) > 0 {
			log.Printf("[STORAGE_SYNC] Parent endpoint or API key missing; object storage sync disabled")
		}
		return
	}
	client := &http.Client{Timeout: 60 * time.Second}
	connection := storageConnection{client: client, endpoint: endpoint, apiKey: apiKey}
	for _, storage := range settings.Storages {
		if storage.Kind != "object_snapshot" {
			continue
		}
		storage := storage
		s.agentWorkers.Add(1)
		go func() {
			defer s.agentWorkers.Done()
			runOneStorageSync(ctx, storageSyncWorker{connection: connection, storage: storage})
		}()
	}
}

type storageSyncWorker struct {
	connection    storageConnection
	storage       sessionsettings.StorageAttachment
	last          map[string]storageLocalFile
	knownRevision int64
}

func fetchStorageManifest(ctx context.Context, connection storageConnection, storage sessionsettings.StorageAttachment) (*storageManifestResponse, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, connection.endpoint+"/storages/"+url.PathEscape(storage.StorageID)+"/files?include_deleted=true", nil)
	if err != nil {
		return nil, err
	}
	request.Header.Set("Authorization", "Bearer "+connection.apiKey)
	response, err := connection.client.Do(request)
	if err != nil {
		return nil, err
	}
	defer func() { closeutils.SafeCloseResponseBody(response.Body) }()
	if response.StatusCode/100 != 2 {
		message, readErr := io.ReadAll(io.LimitReader(response.Body, 4096))
		if readErr != nil {
			return nil, fmt.Errorf("HTTP %d and response could not be read: %w", response.StatusCode, readErr)
		}
		return nil, fmt.Errorf("HTTP %d: %s", response.StatusCode, strings.TrimSpace(string(message)))
	}
	var manifest storageManifestResponse
	if err := json.NewDecoder(response.Body).Decode(&manifest); err != nil {
		return nil, err
	}
	manifest.Files = filterStorageManifest(storage, manifest.Files)
	return &manifest, nil
}

func filterStorageManifest(storage sessionsettings.StorageAttachment, files []storageManifestFile) []storageManifestFile {
	filtered := files[:0]
	prefix := strings.Trim(storage.StoragePath, "/") + "/"
	for _, file := range files {
		if storage.StoragePath != "" && strings.HasPrefix(file.Path, prefix) {
			file.Path = strings.TrimPrefix(file.Path, prefix)
			filtered = append(filtered, file)
		} else if storage.StoragePath == "" && (storage.Managed || storageSyncPathAllowed(storage, file.Path)) {
			filtered = append(filtered, file)
		}
	}
	return filtered
}

func filterStorageSyncFiles(storage sessionsettings.StorageAttachment, files map[string]storageLocalFile) map[string]storageLocalFile {
	for filePath := range files {
		if !storageSyncPathAllowed(storage, filePath) {
			delete(files, filePath)
		}
	}
	return files
}

func storageSyncPathAllowed(storage sessionsettings.StorageAttachment, filePath string) bool {
	if !managedStoragePathAllowed(storage.MountPath, filePath) {
		return false
	}
	if storage.Managed || storage.StoragePath != "" {
		return true
	}
	return filePath != "workspace" && !strings.HasPrefix(filePath, "workspace/") && filePath != "session-state" && !strings.HasPrefix(filePath, "session-state/") && filePath != ".ccplant" && !strings.HasPrefix(filePath, ".ccplant/")
}

func storageRemotePath(storage sessionsettings.StorageAttachment, filePath string) string {
	if storage.StoragePath == "" {
		return filePath
	}
	return strings.Trim(storage.StoragePath, "/") + "/" + strings.TrimLeft(filePath, "/")
}

func storageRequest(ctx context.Context, spec storageHTTPRequest) (io.ReadCloser, error) {
	segments := strings.Split(filepath.ToSlash(spec.filePath), "/")
	for index := range segments {
		segments[index] = url.PathEscape(segments[index])
	}
	requestURL := spec.connection.endpoint + "/storages/" + url.PathEscape(spec.storageID) + "/files/" + strings.Join(segments, "/")
	if spec.method == http.MethodPut {
		requestURL += "?mode=" + strconv.FormatUint(uint64(spec.mode), 8)
	}
	request, err := http.NewRequestWithContext(ctx, spec.method, requestURL, bytes.NewReader(spec.content))
	if err != nil {
		return nil, err
	}
	request.Header.Set("Authorization", "Bearer "+spec.connection.apiKey)
	if spec.method == http.MethodPut {
		request.Header.Set("Content-Type", "application/octet-stream")
	}
	response, err := spec.connection.client.Do(request)
	if err != nil {
		return nil, err
	}
	if response.StatusCode/100 != 2 {
		message, readErr := io.ReadAll(io.LimitReader(response.Body, 4096))
		closeErr := response.Body.Close()
		if readErr != nil || closeErr != nil {
			return nil, errors.Join(fmt.Errorf("HTTP %d response handling failed", response.StatusCode), readErr, closeErr)
		}
		return nil, fmt.Errorf("HTTP %d: %s", response.StatusCode, strings.TrimSpace(string(message)))
	}
	return response.Body, nil
}
