package provisioner

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"math/rand"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/uuid"
	core "github.com/takutakahashi/agentapi-proxy/internal/core/esmcontrol"
	"github.com/takutakahashi/agentapi-proxy/pkg/sessionsettings"
	"github.com/takutakahashi/agentapi-proxy/pkg/telemetry"
)

func (w *directRuntimeWorker) waitUntilAgentReady(ctx context.Context, commandID string) error {
	ticker := time.NewTicker(directRuntimeReadyPollInterval)
	defer ticker.Stop()
	held := false
	for {
		if w.readLocalStatus(ctx, w.provisionerURL+"/status", true) == string(StatusReady) {
			return nil
		}
		if !held {
			held = true
			log.Printf("[DIRECT_RUNTIME] holding prompt %s until the agent is ready", commandID)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
	}
}

func (w *directRuntimeWorker) checkpointAgentType(ctx context.Context) string {
	if agentType := strings.TrimSpace(os.Getenv("AGENTAPI_AGENT_TYPE")); agentType != "" {
		return agentType
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, w.provisionerURL+"/status", nil)
	if err != nil {
		return ""
	}
	resp, err := w.client.Do(req)
	if err != nil {
		return ""
	}
	defer closeDirectRuntimeResponse(resp.Body)
	var status struct {
		AgentType string `json:"agent_type"`
	}
	if resp.StatusCode != http.StatusOK || json.NewDecoder(resp.Body).Decode(&status) != nil {
		return ""
	}
	return status.AgentType
}

func sessionSetupCheckpointStatus(err error) int {
	switch sessionSetupCheckpointReason(err) {
	case "setup_in_progress":
		return http.StatusTooEarly
	case "setup_failed":
		return http.StatusFailedDependency
	}
	return 0
}

func (w *directRuntimeWorker) postExecutionError(ctx context.Context, command core.Command, err error) {
	w.postFramesBestEffort(ctx, []core.ResponseFrame{{ID: uuid.NewString(), RequestID: command.ID, CommandStreamID: command.StreamID, Sequence: 0, Status: http.StatusBadGateway, Error: err.Error(), Done: true, CreatedAt: time.Now().UTC()}})
}

type directRuntimeUploadPayload struct {
	body       []byte
	compressed bool
}

var errDirectRuntimeFrameRequest = errors.New("construct frame upload request")

func (p directRuntimeUploadPayload) needsUncompressedRetry(status int) bool {
	return p.compressed && (status == http.StatusBadRequest || status == http.StatusUnsupportedMediaType)
}

func (w *directRuntimeWorker) postFrames(ctx context.Context, frames []core.ResponseFrame) error {
	raw, err := json.Marshal(map[string]interface{}{"frames": frames})
	if err != nil {
		return err
	}
	u, err := url.Parse(strings.TrimRight(w.cfg.Endpoint, "/") + "/internal/session-runtime/" + url.PathEscape(w.cfg.SessionID) + "/frames")
	if err != nil {
		return err
	}
	query := u.Query()
	query.Set("generation", fmt.Sprintf("%d", w.cfg.Generation))
	query.Set("instance_id", w.instanceID)
	u.RawQuery = query.Encode()
	payload := compressDirectRuntimeFrames(raw)
	backoff := time.Second
	for ctx.Err() == nil {
		status, err := w.uploadFrameAttempt(ctx, u.String(), payload)
		if errors.Is(err, errDirectRuntimeFrameRequest) {
			return err
		}
		if err == nil {
			if status >= 200 && status < 300 {
				return nil
			}
			// Frame IDs make a retry without gzip safe during parent upgrades.
			if payload.needsUncompressedRetry(status) {
				payload = directRuntimeUploadPayload{body: raw}
				continue
			}
			err = fmt.Errorf("frame upload returned HTTP %d", status)
		}
		log.Printf("[DIRECT_RUNTIME] frame upload failed: %v", err)
		w.sleepWithJitter(ctx, backoff)
		backoff = minDuration(backoff*2, 30*time.Second)
	}
	return ctx.Err()
}

func compressDirectRuntimeFrames(raw []byte) directRuntimeUploadPayload {
	original := directRuntimeUploadPayload{body: raw}
	if len(raw) < 64*1024 {
		return original
	}
	var encoded bytes.Buffer
	writer := gzip.NewWriter(&encoded)
	_, writeErr := writer.Write(raw)
	closeErr := writer.Close()
	if writeErr != nil || closeErr != nil {
		log.Printf("[DIRECT_RUNTIME] compression failed; sending uncompressed frames: write=%v close=%v", writeErr, closeErr)
		return original
	}
	if encoded.Len() >= len(raw) {
		return original
	}
	return directRuntimeUploadPayload{body: encoded.Bytes(), compressed: true}
}

func (w *directRuntimeWorker) uploadFrameAttempt(ctx context.Context, endpoint string, payload directRuntimeUploadPayload) (int, error) {
	attemptCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(attemptCtx, http.MethodPost, endpoint, bytes.NewReader(payload.body))
	if err != nil {
		return 0, fmt.Errorf("%w: %w", errDirectRuntimeFrameRequest, err)
	}
	req.Header.Set("Content-Type", "application/json")
	if payload.compressed {
		req.Header.Set("Content-Encoding", "gzip")
	}
	w.authorize(req)
	telemetry.InjectHTTP(ctx, req)
	response, err := w.client.Do(req)
	if err != nil {
		return 0, err
	}
	closeDirectRuntimeResponse(response.Body)
	return response.StatusCode, nil
}

func (w *directRuntimeWorker) authorize(req *http.Request) {
	req.Header.Set("Authorization", "Bearer "+w.cfg.Token)
}

func (w *directRuntimeWorker) sleepWithJitter(ctx context.Context, max time.Duration) {
	if max <= 0 {
		return
	}
	delay := time.Duration(rand.Int63n(int64(max)))
	sleepOrDone(ctx, delay)
}

func persistDirectRuntimeCursor(path, cursor string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, []byte(cursor+"\n"), 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

func acceptsRuntimeEventStream(headers map[string][]string) bool {
	for key, values := range headers {
		if !strings.EqualFold(key, "Accept") {
			continue
		}
		for _, value := range values {
			if strings.Contains(strings.ToLower(value), "text/event-stream") {
				return true
			}
		}
	}
	return false
}

func minDuration(left, right time.Duration) time.Duration {
	if left < right {
		return left
	}
	return right
}

func confirmRuntimeStart(ctx context.Context, cfg *sessionsettings.ParentRuntimeConfig) error {
	if cfg == nil || !cfg.Enabled {
		return nil
	}
	client, err := newPullHTTPClient(ctx, os.Getenv("NODE_EXTRA_CA_CERTS"))
	if err != nil {
		return err
	}
	requestCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	worker := &directRuntimeWorker{cfg: cfg, client: client}
	return worker.postStatus(requestCtx, "starting")
}
