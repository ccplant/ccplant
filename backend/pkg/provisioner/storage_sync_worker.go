package provisioner

import (
	"context"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/takutakahashi/agentapi-proxy/pkg/sessionsettings"
)

func runOneStorageSync(ctx context.Context, worker storageSyncWorker) {
	storage := worker.storage
	if _, err := scanStorageTree(storage.MountPath); err != nil {
		log.Printf("[STORAGE_SYNC] Initial scan failed for %s: %v", storage.StorageID, err)
		return
	}
	worker.last = make(map[string]storageLocalFile, len(storage.Files))
	for _, file := range storage.Files {
		if storageSyncPathAllowed(storage, file.Path) {
			worker.last[file.Path] = storageLocalFile{digest: file.Digest, mode: file.Mode}
		}
	}
	worker.knownRevision = storage.Revision
	ticker := time.NewTicker(storageSyncInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			flushCtx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
			worker.syncOnce(flushCtx)
			cancel()
			return
		case <-ticker.C:
			worker.syncOnce(ctx)
		}
	}
}

func (worker *storageSyncWorker) syncOnce(ctx context.Context) {
	manifest, err := fetchStorageManifest(ctx, worker.connection, worker.storage)
	if err != nil {
		log.Printf("[STORAGE_SYNC] Manifest fetch failed for %s: %v", worker.storage.StorageID, err)
		return
	}
	current, err := scanStorageTree(worker.storage.MountPath)
	if err != nil {
		log.Printf("[STORAGE_SYNC] Scan failed for %s: %v", worker.storage.StorageID, err)
		return
	}
	current = filterStorageSyncFiles(worker.storage, current)
	if err := worker.pullRemoteChanges(ctx, manifest, current); err != nil {
		log.Printf("[STORAGE_SYNC] Rescan failed for %s: %v", worker.storage.StorageID, err)
		return
	}
	worker.knownRevision = manifest.Revision
	if worker.storage.ReadOnly {
		return
	}
	current, err = scanStorageTree(worker.storage.MountPath)
	if err != nil {
		log.Printf("[STORAGE_SYNC] Scan before push failed for %s: %v", worker.storage.StorageID, err)
		return
	}
	current = filterStorageSyncFiles(worker.storage, current)
	worker.pushLocalChanges(ctx, current)
	worker.deleteRemoteFiles(ctx, current)
}

func (worker *storageSyncWorker) pullRemoteChanges(ctx context.Context, manifest *storageManifestResponse, current map[string]storageLocalFile) error {
	touched := make(map[string]struct{})
	for _, remote := range manifest.Files {
		if remote.Revision <= worker.knownRevision || worker.localFileChanged(remote.Path, current) {
			continue
		}
		if worker.applyRemoteFile(ctx, remote) {
			touched[remote.Path] = struct{}{}
		}
	}
	if len(touched) == 0 {
		return nil
	}
	rescanned, err := scanStorageTree(worker.storage.MountPath)
	if err != nil {
		return err
	}
	for filePath := range touched {
		if value, ok := rescanned[filePath]; ok {
			worker.last[filePath] = value
		} else {
			delete(worker.last, filePath)
		}
	}
	return nil
}

func (worker *storageSyncWorker) localFileChanged(path string, current map[string]storageLocalFile) bool {
	previous, existed := worker.last[path]
	local, existsNow := current[path]
	return existed != existsNow || (existed && previous.digest != local.digest)
}

func (worker *storageSyncWorker) applyRemoteFile(ctx context.Context, remote storageManifestFile) bool {
	target, err := safeStorageTarget(worker.storage.MountPath, remote.Path)
	if err != nil {
		return false
	}
	if remote.Deleted {
		if err := os.Remove(target); err != nil && !os.IsNotExist(err) {
			log.Printf("[STORAGE_SYNC] Delete local file %s/%s: %v", worker.storage.StorageID, remote.Path, err)
			return false
		}
		return true
	}
	file := sessionsettings.StorageFile{Path: remote.Path, Digest: remote.Digest, Size: remote.Size, Mode: remote.Mode, Revision: remote.Revision}
	if err := restoreStorageFile(ctx, storageFileTransfer{connection: worker.connection, storage: worker.storage, file: file}); err != nil {
		log.Printf("[STORAGE_SYNC] Pull failed for %s/%s: %v", worker.storage.StorageID, remote.Path, err)
		return false
	}
	return true
}

func (worker *storageSyncWorker) pushLocalChanges(ctx context.Context, current map[string]storageLocalFile) {
	paths := make([]string, 0, len(current))
	for filePath := range current {
		paths = append(paths, filePath)
	}
	sort.Strings(paths)
	for _, filePath := range paths {
		worker.pushLocalFile(ctx, filePath, current[filePath])
	}
}

func (worker *storageSyncWorker) pushLocalFile(ctx context.Context, filePath string, file storageLocalFile) {
	if previous, ok := worker.last[filePath]; ok && previous == file {
		return
	}
	data, err := os.ReadFile(filepath.Join(worker.storage.MountPath, filepath.FromSlash(filePath)))
	if err != nil || len(data) > storageSyncMaxFileBytes {
		return
	}
	spec := storageHTTPRequest{connection: worker.connection, method: http.MethodPut, storageID: worker.storage.StorageID, filePath: storageRemotePath(worker.storage, filePath), content: data, mode: file.mode}
	body, err := storageRequest(ctx, spec)
	if body != nil {
		if closeErr := body.Close(); closeErr != nil && err == nil {
			err = closeErr
		}
	}
	if err != nil {
		log.Printf("[STORAGE_SYNC] Push failed for %s/%s: %v", worker.storage.StorageID, filePath, err)
		return
	}
	worker.last[filePath] = file
}

func (worker *storageSyncWorker) deleteRemoteFiles(ctx context.Context, current map[string]storageLocalFile) {
	for filePath := range worker.last {
		if _, ok := current[filePath]; ok {
			continue
		}
		spec := storageHTTPRequest{connection: worker.connection, method: http.MethodDelete, storageID: worker.storage.StorageID, filePath: storageRemotePath(worker.storage, filePath)}
		body, err := storageRequest(ctx, spec)
		if body != nil {
			if closeErr := body.Close(); closeErr != nil && err == nil {
				err = closeErr
			}
		}
		if err == nil {
			delete(worker.last, filePath)
		}
	}
}
