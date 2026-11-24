package cmd

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/spf13/cobra"
	"github.com/takutakahashi/agentapi-proxy/pkg/sessionstate"
	closeutils "github.com/takutakahashi/agentapi-proxy/pkg/utils"
)

var backupSessionStateCmd = &cobra.Command{Use: "backup-session-state", Short: "Back up ACP state through the proxy", Args: cobra.NoArgs, RunE: runBackupSessionState}
var scheduleSessionSuspendCmd = &cobra.Command{Use: "schedule-session-suspend", Short: "Schedule persisted session suspension", Args: cobra.NoArgs, RunE: runScheduleSessionSuspend}

func runScheduleSessionSuspend(_ *cobra.Command, _ []string) error {
	proxy := strings.TrimRight(os.Getenv("PROVISIONER_PROXY_URL"), "/")
	token := os.Getenv("PROVISIONER_TOKEN")
	id := os.Getenv("AGENTAPI_SESSION_ID")
	if proxy == "" || token == "" || id == "" {
		return fmt.Errorf("PROVISIONER_PROXY_URL, PROVISIONER_TOKEN and AGENTAPI_SESSION_ID are required")
	}
	req, err := internalRequest(http.MethodPost, proxy+"/internal/session-state/"+id+"/suspend", token, nil)
	if err != nil {
		return err
	}
	resp, err := (&http.Client{Timeout: 30 * time.Second}).Do(req)
	if err != nil {
		return err
	}
	defer func() { closeutils.SafeCloseResponseBody(resp.Body) }()
	if resp.StatusCode != http.StatusNoContent {
		return fmt.Errorf("schedule session suspend failed: HTTP %d", resp.StatusCode)
	}
	return nil
}

func runBackupSessionState(_ *cobra.Command, _ []string) error {
	config := readSessionBackupConfig()
	if config.proxy == "" || config.token == "" || config.sessionID == "" {
		return fmt.Errorf("PROVISIONER_PROXY_URL, PROVISIONER_TOKEN and AGENTAPI_SESSION_ID are required")
	}
	markerPath := filepath.Join(os.TempDir(), "agentapi-session-restore-complete-"+config.sessionID)
	if _, err := os.Stat(markerPath); err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("session state backup rejected: restore has not completed")
		}
		return fmt.Errorf("check session restore completion: %w", err)
	}
	cwd, err := sessionStateCWD()
	if err != nil {
		return err
	}
	home := sessionBackupHome()
	if volumePath := strings.TrimSpace(os.Getenv("AGENTAPI_SESSION_STATE_VOLUME_PATH")); volumePath != "" {
		volume := sessionStateVolumeRequest{path: volumePath, agentType: config.agentType, acpSessionID: readACPSessionID(cwd), home: home, cwd: cwd}
		if err := writeSessionStateVolume(volume); err != nil {
			return err
		}
		if snapshotID := strings.TrimSpace(os.Getenv("AGENTAPI_SESSION_TEMPLATE_SNAPSHOT_ID")); snapshotID != "" {
			return uploadVolumeTemplateSnapshot(config.proxy, config.token, snapshotID, volumePath)
		}
		return nil
	}
	return backupSessionStateRemote(config, home, cwd)
}

type sessionBackupConfig struct {
	proxy, token, sessionID, agentType string
	strict                             bool
}

func readSessionBackupConfig() sessionBackupConfig {
	proxy := strings.TrimRight(os.Getenv("SESSION_STATE_PROXY_URL"), "/")
	if proxy == "" {
		proxy = strings.TrimRight(os.Getenv("PROVISIONER_PROXY_URL"), "/")
	}
	return sessionBackupConfig{
		proxy: proxy, token: os.Getenv("PROVISIONER_TOKEN"),
		sessionID: os.Getenv("AGENTAPI_SESSION_ID"), agentType: os.Getenv("AGENTAPI_AGENT_TYPE"),
		strict: os.Getenv("AGENTAPI_REQUIRE_SESSION_STATE_BACKUP") == "1",
	}
}

func sessionBackupHome() string {
	home := os.Getenv("HOME")
	if home == "" {
		return "/home/agentapi"
	}
	return home
}

func backupSessionStateRemote(config sessionBackupConfig, home, cwd string) error {
	client := &http.Client{Timeout: 10 * time.Minute}
	direct, err := beginDirectUpload(client, config.proxy, config.token, config.sessionID)
	if err == nil && direct != nil {
		upload := directUploadRequest{client: client, proxy: config.proxy, token: config.token, sessionID: config.sessionID, agentType: config.agentType, acpID: readACPSessionID(cwd), home: home, cwd: cwd, upload: *direct}
		return backupSessionStateDirect(config.strict, upload)
	}
	if err != nil {
		return handleUnavailableSessionBackup(config.strict, err)
	}
	return streamSessionStateBackup(config, client, home, cwd)
}

func backupSessionStateDirect(strict bool, upload directUploadRequest) error {
	if err := uploadSessionStateDirect(context.Background(), upload); err != nil {
		if strict {
			return fmt.Errorf("session state backup required before suspend: %w", err)
		}
		return writeBackupWarning("session state backup skipped: direct persistence transfer failed: %v\n", err)
	}
	return nil
}

func handleUnavailableSessionBackup(strict bool, cause error) error {
	if strict {
		return fmt.Errorf("session state backup required before suspend: %w", cause)
	}
	return writeBackupWarning("session state backup skipped: persistence backend is unavailable: %v\n", cause)
}

func streamSessionStateBackup(config sessionBackupConfig, client *http.Client, home, cwd string) error {
	bodyReader, bodyWriter := io.Pipe()
	go func() {
		if closeErr := bodyWriter.CloseWithError(sessionstate.Pack(bodyWriter, config.agentType, readACPSessionID(cwd), home, cwd)); closeErr != nil {
			return
		}
	}()
	req, err := http.NewRequest(http.MethodPut, config.proxy+"/internal/session-state/"+config.sessionID, bodyReader)
	if err != nil {
		if closeErr := bodyReader.Close(); closeErr != nil {
			return fmt.Errorf("create backup request: %w (close stream: %v)", err, closeErr)
		}
		return err
	}
	req.Header.Set("Authorization", "Bearer "+config.token)
	req.Header.Set("Content-Type", "application/zstd")
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer func() { closeutils.SafeCloseResponseBody(resp.Body) }()
	if resp.StatusCode != http.StatusNoContent {
		if resp.StatusCode != http.StatusServiceUnavailable {
			return fmt.Errorf("session state backup failed: HTTP %d", resp.StatusCode)
		}
		return handleUnavailableSessionBackup(config.strict, fmt.Errorf("HTTP %d", resp.StatusCode))
	}
	return nil
}

func writeBackupWarning(format string, args ...any) error {
	_, err := fmt.Fprintf(os.Stderr, format, args...)
	return err
}

func uploadVolumeTemplateSnapshot(proxy, token, snapshotID, path string) error {
	if !strings.HasPrefix(snapshotID, "tpl_") {
		return fmt.Errorf("invalid template snapshot ID")
	}
	archive, err := os.Open(path)
	if err != nil {
		return err
	}
	defer func() {
		if closeErr := archive.Close(); closeErr != nil {
			return
		}
	}()
	req, err := internalRequest(http.MethodPut, proxy+"/internal/session-state/"+url.PathEscape(snapshotID), token, archive)
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/zstd")
	resp, err := (&http.Client{Timeout: 10 * time.Minute}).Do(req)
	if err != nil {
		return err
	}
	defer func() { closeutils.SafeCloseResponseBody(resp.Body) }()
	if resp.StatusCode != http.StatusNoContent {
		return fmt.Errorf("template snapshot upload failed: HTTP %d", resp.StatusCode)
	}
	return nil
}

type sessionStateVolumeRequest struct {
	path, agentType, acpSessionID, home, cwd string
}

func writeSessionStateVolume(request sessionStateVolumeRequest) error {
	dir := filepath.Dir(request.path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	release, err := sessionstate.LockVolume(dir)
	if err != nil {
		return err
	}
	defer release()
	if err := sessionstate.RemoveStaleVolumeTemps(dir); err != nil {
		if writeErr := writeBackupWarning("remove stale session state temporary archives: %v\n", err); writeErr != nil {
			return writeErr
		}
	}
	tmp, err := os.CreateTemp(dir, sessionstate.VolumeTempPattern)
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer func() {
		if removeErr := os.Remove(tmpName); removeErr != nil && !os.IsNotExist(removeErr) {
			return
		}
	}()
	pack := sessionstate.Pack
	if volumeHoldsWorkspace(request.path, request.cwd) && strings.TrimSpace(os.Getenv("AGENTAPI_SESSION_TEMPLATE_SNAPSHOT_ID")) == "" {
		pack = sessionstate.PackConversation
	}
	if err := pack(tmp, request.agentType, request.acpSessionID, request.home, request.cwd); err != nil {
		return errors.Join(err, tmp.Close())
	}
	if err := tmp.Sync(); err != nil {
		return errors.Join(err, tmp.Close())
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Chmod(tmpName, 0o600); err != nil {
		return err
	}
	return os.Rename(tmpName, request.path)
}

func volumeHoldsWorkspace(archivePath, cwd string) bool {
	dir := filepath.Dir(archivePath)
	if filepath.Base(dir) != ".agentapi" {
		return false
	}
	rel, err := filepath.Rel(filepath.Dir(dir), cwd)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) && !filepath.IsAbs(rel)
}

// sessionStateCWD prefers the provisioner's canonical clone path over the
// hook runner's process directory. ACP hook implementations do not all invoke
// commands from the session workspace.
func sessionStateCWD() (string, error) {
	cwd, err := os.Getwd()
	if err != nil {
		return "", err
	}
	cloneDir := strings.TrimSpace(os.Getenv("AGENTAPI_CLONE_DIR"))
	if cloneDir == "" {
		return cwd, nil
	}
	if info, statErr := os.Stat(filepath.Join(cloneDir, ".git")); statErr == nil && (info.IsDir() || info.Mode().IsRegular()) {
		return cloneDir, nil
	}
	return cwd, nil
}

type directUpload struct {
	UploadID string `json:"upload_id"`
	PartSize int64  `json:"part_size"`
}
type uploadedPart struct {
	Number int32  `json:"number"`
	ETag   string `json:"etag"`
}

type directUploadRequest struct {
	client                      *http.Client
	proxy, token, sessionID     string
	agentType, acpID, home, cwd string
	upload                      directUpload
}

type directPartRequest struct {
	client                  *http.Client
	proxy, token, sessionID string
	uploadID                string
	number                  int32
	data                    []byte
}

type directUploadJob struct {
	number int32
	data   []byte
}

type directUploadProgress struct {
	mu       sync.Mutex
	parts    []uploadedPart
	firstErr error
}

type directUploadWorker struct {
	ctx      context.Context
	request  directUploadRequest
	jobs     <-chan directUploadJob
	progress *directUploadProgress
	cancel   context.CancelFunc
	wg       *sync.WaitGroup
}

func (progress *directUploadProgress) recordPart(number int32, etag string, err error, cancel context.CancelFunc) {
	progress.mu.Lock()
	defer progress.mu.Unlock()
	if err != nil && progress.firstErr == nil {
		progress.firstErr = err
		cancel()
	} else if err == nil {
		progress.parts = append(progress.parts, uploadedPart{Number: number, ETag: etag})
	}
}

func (progress *directUploadProgress) result() ([]uploadedPart, error) {
	progress.mu.Lock()
	defer progress.mu.Unlock()
	return progress.parts, progress.firstErr
}

func internalRequest(method, url, token string, body io.Reader) (*http.Request, error) {
	req, err := http.NewRequest(method, url, body)
	if err == nil {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	return req, err
}

func beginDirectUpload(client *http.Client, proxy, token, id string) (*directUpload, error) {
	req, err := internalRequest(http.MethodPost, proxy+"/internal/session-state/"+id+"/uploads", token, nil)
	if err != nil {
		return nil, err
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { closeutils.SafeCloseResponseBody(resp.Body) }()
	if resp.StatusCode == http.StatusNotImplemented {
		return nil, nil
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("begin upload HTTP %d", resp.StatusCode)
	}
	var result directUpload
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, err
	}
	if result.UploadID == "" || result.PartSize < 5<<20 {
		return nil, fmt.Errorf("invalid multipart response")
	}
	return &result, nil
}
