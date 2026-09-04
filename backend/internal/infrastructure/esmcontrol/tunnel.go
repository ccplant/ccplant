package esmcontrol

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"sync"
	"time"

	"github.com/google/uuid"
	core "github.com/takutakahashi/agentapi-proxy/internal/core/esmcontrol"
	"github.com/takutakahashi/agentapi-proxy/pkg/telemetry"
)

type Tunnel struct{ store core.Store }

func NewTunnel(store core.Store) *Tunnel { return &Tunnel{store: store} }

func (t *Tunnel) IsConnected(ctx context.Context, managerID string) bool {
	if t == nil || t.store == nil || managerID == "" {
		return false
	}
	ok, err := t.store.IsManagerConnected(ctx, managerID)
	return err == nil && ok
}

func (t *Tunnel) Do(ctx context.Context, managerID, sessionID, remoteSessionID string, req *http.Request) (*http.Response, error) {
	if !t.IsConnected(ctx, managerID) {
		return nil, fmt.Errorf("external session manager %s has no outbound control lease", managerID)
	}
	requestID, err := telemetry.Operation(ctx, "esmcontrol.Tunnel.Enqueue", func(operationCtx context.Context) (string, error) {
		return t.Enqueue(operationCtx, managerID, sessionID, remoteSessionID, req)
	}, telemetry.String("http.request.method", req.Method))
	if err != nil {
		return nil, err
	}

	type firstFrameResult struct {
		frames []core.ResponseFrame
		cursor string
	}
	firstResult, err := telemetry.Operation(ctx, "esmcontrol.Tunnel.WaitForFirstFrame", func(operationCtx context.Context) (firstFrameResult, error) {
		frames, cursor, waitErr := t.waitForFirstFrame(operationCtx, requestID)
		return firstFrameResult{frames: frames, cursor: cursor}, waitErr
	})
	if err != nil {
		return nil, err
	}
	frames, cursor := firstResult.frames, firstResult.cursor
	first := frames[0]
	if first.Error != "" {
		return nil, fmt.Errorf("external session manager: %s", first.Error)
	}
	pipeReader, pipeWriter := io.Pipe()
	go telemetry.OperationVoid(ctx, "esmcontrol.Tunnel.StreamResponseBody", func(operationCtx context.Context) {
		t.streamBody(operationCtx, requestID, cursor, frames, pipeWriter)
	})
	return &http.Response{
		StatusCode: first.Status, Status: fmt.Sprintf("%d %s", first.Status, http.StatusText(first.Status)),
		Header: http.Header(first.Headers), Body: &cancelBody{ReadCloser: pipeReader, cancel: func() {
			cancelCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			_, _ = t.store.EnqueueCommand(cancelCtx, managerID, core.Command{
				ID: uuid.NewString(), CancelRequestID: requestID, ManagerID: managerID,
				Deadline: time.Now().Add(30 * time.Second), CreatedAt: time.Now().UTC(),
			})
		}}, ContentLength: -1, Request: req,
	}, nil
}

// Enqueue persists a manager command without waiting for its response. Lifecycle
// operations use this so slow Kubernetes termination cannot be cut off by an HTTP
// proxy timeout; the manager resumes the durable command after reconnecting.
func (t *Tunnel) Enqueue(ctx context.Context, managerID, sessionID, remoteSessionID string, req *http.Request) (string, error) {
	var body []byte
	if req.Body != nil {
		var err error
		body, err = io.ReadAll(req.Body)
		if err != nil {
			return "", err
		}
	}
	deadline := time.Now().Add(30 * time.Minute)
	if value, ok := ctx.Deadline(); ok {
		deadline = value
	}
	command := core.Command{
		ID: uuid.NewString(), ManagerID: managerID, SessionID: sessionID,
		RemoteSessionID: remoteSessionID, Method: req.Method, Path: req.URL.Path,
		RawQuery: req.URL.RawQuery, Headers: cloneHeaders(req.Header), Body: body,
		Deadline: deadline, CreatedAt: time.Now().UTC(),
	}
	if _, err := t.store.EnqueueCommand(ctx, managerID, command); err != nil {
		return "", err
	}
	return command.ID, nil
}

// CommandResult reports whether an asynchronously enqueued command has reached
// a terminal response. Response frames are durable, so callers can resume this
// check after a proxy restart.
func (t *Tunnel) CommandResult(ctx context.Context, requestID string) (bool, int, error) {
	frames, err := t.store.ReadFrames(ctx, requestID, "0-0", 0, 100)
	if err != nil {
		return false, 0, err
	}
	status := 0
	for _, frame := range frames {
		if frame.Status != 0 {
			status = frame.Status
		}
		if frame.Error != "" {
			return true, status, fmt.Errorf("external session manager: %s", frame.Error)
		}
		if frame.Done {
			return true, status, nil
		}
	}
	return false, status, nil
}

type cancelBody struct {
	io.ReadCloser
	once   sync.Once
	cancel func()
}

func (b *cancelBody) Close() error {
	err := b.ReadCloser.Close()
	b.once.Do(b.cancel)
	return err
}

func (t *Tunnel) waitForFirstFrame(ctx context.Context, requestID string) ([]core.ResponseFrame, string, error) {
	cursor := "0-0"
	for {
		frames, err := t.store.ReadFrames(ctx, requestID, cursor, 30*time.Second, 100)
		if err != nil {
			return nil, cursor, err
		}
		if len(frames) > 0 {
			return frames, frames[len(frames)-1].StreamID, nil
		}
		if err := ctx.Err(); err != nil {
			return nil, cursor, err
		}
	}
}

func (t *Tunnel) streamBody(ctx context.Context, requestID, cursor string, initial []core.ResponseFrame, writer *io.PipeWriter) {
	defer func() { _ = writer.Close() }()
	frameCount := int64(len(initial))
	byteCount := frameBytes(initial)
	upstreamTTFBMS, upstreamReadMS := frameTimings(initial)
	defer func() {
		telemetry.SetAttributes(ctx, telemetry.Int64("esm.response.frame_count", frameCount), telemetry.Int64("esm.response.body.size", byteCount), telemetry.Int64("esm.upstream.ttfb_ms", upstreamTTFBMS), telemetry.Int64("esm.upstream.read_ms", upstreamReadMS))
	}()
	if writeFrames(writer, initial) {
		return
	}
	for {
		frames, err := telemetry.Operation(ctx, "esmcontrol.Store.ReadFrames", func(operationCtx context.Context) ([]core.ResponseFrame, error) {
			return t.store.ReadFrames(operationCtx, requestID, cursor, 30*time.Second, 100)
		}, telemetry.Bool("esm.store.blocking", true))
		if err != nil {
			_ = writer.CloseWithError(err)
			return
		}
		if len(frames) > 0 {
			frameCount += int64(len(frames))
			byteCount += frameBytes(frames)
			batchTTFBMS, batchReadMS := frameTimings(frames)
			upstreamTTFBMS = max(upstreamTTFBMS, batchTTFBMS)
			upstreamReadMS = max(upstreamReadMS, batchReadMS)
			cursor = frames[len(frames)-1].StreamID
			if writeFrames(writer, frames) {
				return
			}
		}
		if err := ctx.Err(); err != nil {
			_ = writer.CloseWithError(err)
			return
		}
	}
}

func frameTimings(frames []core.ResponseFrame) (int64, int64) {
	var ttfbMS, readMS int64
	for _, frame := range frames {
		ttfbMS = max(ttfbMS, frame.UpstreamTTFBMS)
		readMS = max(readMS, frame.UpstreamReadMS)
	}
	return ttfbMS, readMS
}

func frameBytes(frames []core.ResponseFrame) int64 {
	var total int64
	for _, frame := range frames {
		total += int64(len(frame.Body))
	}
	return total
}

func writeFrames(writer io.Writer, frames []core.ResponseFrame) bool {
	for _, frame := range frames {
		if len(frame.Body) > 0 {
			_, _ = io.Copy(writer, bytes.NewReader(frame.Body))
		}
		if frame.Done {
			return true
		}
	}
	return false
}

func cloneHeaders(source http.Header) map[string][]string {
	result := make(map[string][]string, len(source))
	for key, values := range source {
		switch http.CanonicalHeaderKey(key) {
		case "Authorization", "Cookie", "X-Api-Key", "X-Hub-Signature-256", "X-Timestamp", "X-Session-Manager-Token":
			continue
		}
		result[key] = append([]string(nil), values...)
	}
	return result
}
