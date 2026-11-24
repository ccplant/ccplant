package sessionmanager

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	core "github.com/takutakahashi/agentapi-proxy/internal/core/esmcontrol"
	"github.com/takutakahashi/agentapi-proxy/pkg/hmacutil"
	"github.com/takutakahashi/agentapi-proxy/pkg/telemetry"
	closeutils "github.com/takutakahashi/agentapi-proxy/pkg/utils"
)

type ControlWorker struct {
	upstreamURL     string
	connectionToken string
	localHMACSecret string
	upstreamAuth    string
	localURL        string
	instanceID      string
	client          *http.Client
	active          sync.Map
	executeSlots    chan struct{}
}

const maxConcurrentControlRequests = 32

type ControlWorkerConfig struct {
	UpstreamURL, ConnectionToken, UpstreamAuth string
	LocalURL, InstanceID, LocalHMACSecret      string
}

func NewControlWorker(config ControlWorkerConfig) *ControlWorker {
	if config.LocalURL == "" {
		config.LocalURL = "http://127.0.0.1:8080"
	}
	return &ControlWorker{
		upstreamURL: strings.TrimRight(config.UpstreamURL, "/"), connectionToken: config.ConnectionToken,
		upstreamAuth: config.UpstreamAuth, localURL: strings.TrimRight(config.LocalURL, "/"), instanceID: config.InstanceID,
		localHMACSecret: config.LocalHMACSecret,
		client:          &http.Client{},
		executeSlots:    make(chan struct{}, maxConcurrentControlRequests),
	}
}

func (w *ControlWorker) Start(ctx context.Context) {
	cursor := "0-0"
	for ctx.Err() == nil {
		commands, next, err := w.poll(ctx, cursor)
		if err != nil {
			log.Printf("[ESM_CONTROL] command poll failed: %v", err)
			sleepOrDone(ctx, 5*time.Second)
			continue
		}
		for _, command := range commands {
			if !w.dispatchCommand(ctx, command) {
				return
			}
		}
		if next != "" {
			cursor = next
		}
	}
}

func (w *ControlWorker) dispatchCommand(ctx context.Context, command core.Command) bool {
	if command.CancelRequestID != "" {
		if value, ok := w.active.Load(command.CancelRequestID); ok {
			value.(context.CancelFunc)()
		}
		w.postFrameError(ctx, command, http.StatusNoContent, "")
		return true
	}
	if time.Now().After(command.Deadline) {
		w.postFrameError(ctx, command, http.StatusGatewayTimeout, "command deadline exceeded")
		return true
	}
	select {
	case w.executeSlots <- struct{}{}:
		go func() {
			defer func() { <-w.executeSlots }()
			w.execute(ctx, command)
		}()
		return true
	case <-ctx.Done():
		return false
	}
}

func (w *ControlWorker) poll(ctx context.Context, cursor string) ([]core.Command, string, error) {
	u, err := url.Parse(w.upstreamURL + "/internal/external-session-manager/control/commands")
	if err != nil {
		return nil, cursor, fmt.Errorf("parse command endpoint: %w", err)
	}
	q := u.Query()
	q.Set("after", cursor)
	q.Set("wait", "30s")
	q.Set("instance_id", w.instanceID)
	u.RawQuery = q.Encode()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, cursor, fmt.Errorf("create command request: %w", err)
	}
	w.authorize(req)
	resp, err := w.client.Do(req)
	if err != nil {
		return nil, cursor, err
	}
	defer func() { closeutils.SafeCloseResponseBody(resp.Body) }()
	if resp.StatusCode == http.StatusNoContent {
		return nil, cursor, nil
	}
	if resp.StatusCode != http.StatusOK {
		return nil, cursor, fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	var result struct {
		Commands   []core.Command `json:"commands"`
		NextCursor string         `json:"next_cursor"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, cursor, err
	}
	return result.Commands, result.NextCursor, nil
}

func (w *ControlWorker) execute(ctx context.Context, command core.Command) {
	var commandCtx context.Context
	var cancel context.CancelFunc
	if !command.Deadline.IsZero() {
		commandCtx, cancel = context.WithDeadline(ctx, command.Deadline)
	} else {
		commandCtx, cancel = context.WithCancel(ctx)
	}
	w.active.Store(command.ID, context.CancelFunc(cancel))
	defer func() {
		w.active.Delete(command.ID)
		cancel()
	}()
	commandCtx = telemetry.ExtractHTTP(commandCtx, http.Header(command.Headers))
	if err := telemetry.OperationErr(commandCtx, "sessionmanager.ControlWorker.Execute", func(operationCtx context.Context) error {
		return w.executeCommand(operationCtx, command)
	}, telemetry.String("http.request.method", command.Method)); err != nil {
		log.Printf("[ESM_CONTROL] execute command %s: %v", command.ID, err)
	}
}

func (w *ControlWorker) executeCommand(commandCtx context.Context, command core.Command) error {
	req, err := w.localCommandRequest(commandCtx, command)
	if err != nil {
		w.postExecutionError(commandCtx, command, err)
		return err
	}
	resp, err := w.client.Do(req)
	if err != nil {
		w.postExecutionError(commandCtx, command, err)
		return err
	}
	if command.Method == http.MethodPost && strings.HasSuffix(command.Path, "/suspend") {
		log.Printf("[ESM_CONTROL] suspend command response id=%s path=%s status=%d", command.ID, command.Path, resp.StatusCode)
	}
	defer func() { closeutils.SafeCloseResponseBody(resp.Body) }()
	return w.forwardCommandResponse(commandCtx, command, resp)
}

func (w *ControlWorker) localCommandRequest(commandCtx context.Context, command core.Command) (*http.Request, error) {
	target := w.localURL + command.Path
	if command.RawQuery != "" {
		target += "?" + command.RawQuery
	}
	req, err := http.NewRequestWithContext(commandCtx, command.Method, target, bytes.NewReader(command.Body))
	if err != nil {
		return nil, err
	}
	req.Header = http.Header(command.Headers).Clone()
	if req.Header == nil {
		req.Header = make(http.Header)
	}
	// The local ESM endpoint retains its HMAC middleware as defense in depth.
	ts := hmacutil.NowTimestamp()
	msg := hmacutil.BuildMessage(req.Method, req.URL.RequestURI(), ts, command.Body)
	secret := w.localHMACSecret
	if secret == "" {
		secret = w.connectionToken
	}
	req.Header.Set("X-Hub-Signature-256", hmacutil.Sign([]byte(secret), msg))
	req.Header.Set(hmacutil.TimestampHeader, ts)
	telemetry.InjectHTTP(commandCtx, req)
	return req, nil
}

func (w *ControlWorker) forwardCommandResponse(commandCtx context.Context, command core.Command, resp *http.Response) error {
	start := core.ResponseFrame{ID: uuid.NewString(), RequestID: command.ID, Status: resp.StatusCode, Headers: map[string][]string(resp.Header.Clone()), CreatedAt: time.Now().UTC()}
	isEventStream := acceptsEventStream(command.Headers) || strings.HasPrefix(resp.Header.Get("Content-Type"), "text/event-stream")
	if isEventStream {
		start.CommandStreamID = command.StreamID
	}
	forwarder := &commandResponseForwarder{worker: w, ctx: commandCtx, command: command, frames: []core.ResponseFrame{start}, eventStream: isEventStream}
	if isEventStream {
		if err := forwarder.flush(commandCtx); err != nil {
			return err
		}
	}
	buffer := make([]byte, 64*1024)
	for {
		n, readErr := resp.Body.Read(buffer)
		done, err := forwarder.consume(buffer[:n], readErr)
		if done || err != nil {
			return err
		}
	}
}

type commandResponseForwarder struct {
	worker      *ControlWorker
	ctx         context.Context
	command     core.Command
	frames      []core.ResponseFrame
	sequence    int64
	batchBytes  int
	eventStream bool
}

func (f *commandResponseForwarder) consume(data []byte, readErr error) (bool, error) {
	if len(data) > 0 {
		f.sequence++
		f.frames = append(f.frames, core.ResponseFrame{ID: uuid.NewString(), RequestID: f.command.ID, Sequence: f.sequence, Body: append([]byte(nil), data...), CreatedAt: time.Now().UTC()})
		f.batchBytes += len(data)
	}
	if readErr != nil {
		f.sequence++
		frame := core.ResponseFrame{ID: uuid.NewString(), RequestID: f.command.ID, CommandStreamID: f.command.StreamID, Sequence: f.sequence, Done: true, CreatedAt: time.Now().UTC()}
		if readErr != io.EOF {
			frame.Error = readErr.Error()
		}
		f.frames = append(f.frames, frame)
		if err := f.flush(context.WithoutCancel(f.ctx)); err != nil {
			return true, err
		}
		if readErr != io.EOF {
			return true, readErr
		}
		return true, nil
	}
	if f.eventStream || f.batchBytes >= 512*1024 {
		return false, f.flush(f.ctx)
	}
	return false, nil
}

func (f *commandResponseForwarder) flush(ctx context.Context) error {
	if err := f.worker.postFrames(ctx, f.frames); err != nil {
		return err
	}
	f.frames = nil
	f.batchBytes = 0
	return nil
}

func (w *ControlWorker) postExecutionError(ctx context.Context, command core.Command, err error) {
	w.postFrameError(ctx, command, http.StatusBadGateway, err.Error())
}

func (w *ControlWorker) postFrameError(ctx context.Context, command core.Command, status int, message string) {
	frame := core.ResponseFrame{ID: uuid.NewString(), RequestID: command.ID, CommandStreamID: command.StreamID, Sequence: 0, Status: status, Error: message, Done: true, CreatedAt: time.Now().UTC()}
	if err := w.postFrames(ctx, []core.ResponseFrame{frame}); err != nil {
		log.Printf("[ESM_CONTROL] post terminal frame for command %s: %v", command.ID, err)
	}
}

func (w *ControlWorker) postFrames(ctx context.Context, frames []core.ResponseFrame) error {
	return telemetry.OperationErr(ctx, "sessionmanager.ControlWorker.PostFrames", func(operationCtx context.Context) error {
		return w.postFramesRequest(operationCtx, frames)
	}, telemetry.Int64("esm.response.frame_count", int64(len(frames))), telemetry.Int64("esm.response.body.size", frameBytes(frames)))
}

func (w *ControlWorker) postFramesRequest(ctx context.Context, frames []core.ResponseFrame) error {
	body, err := json.Marshal(map[string]interface{}{"frames": frames})
	if err != nil {
		return fmt.Errorf("encode response frames: %w", err)
	}
	u, err := url.Parse(w.upstreamURL + "/internal/external-session-manager/control/frames")
	if err != nil {
		return fmt.Errorf("parse frame endpoint: %w", err)
	}
	q := u.Query()
	q.Set("instance_id", w.instanceID)
	u.RawQuery = q.Encode()
	for ctx.Err() == nil {
		req, requestErr := http.NewRequestWithContext(ctx, http.MethodPost, u.String(), bytes.NewReader(body))
		if requestErr != nil {
			return fmt.Errorf("create frame request: %w", requestErr)
		}
		req.Header.Set("Content-Type", "application/json")
		w.authorize(req)
		telemetry.InjectHTTP(ctx, req)
		resp, err := w.client.Do(req)
		if err == nil {
			statusCode := func() int {
				defer func() { closeutils.SafeCloseResponseBody(resp.Body) }()
				return resp.StatusCode
			}()
			if statusCode >= 200 && statusCode < 300 {
				return nil
			}
			err = fmt.Errorf("HTTP %d", statusCode)
		}
		log.Printf("[ESM_CONTROL] frame upload failed: %v", err)
		sleepOrDone(ctx, 2*time.Second)
	}
	return ctx.Err()
}

func frameBytes(frames []core.ResponseFrame) int64 {
	var total int64
	for _, frame := range frames {
		total += int64(len(frame.Body))
	}
	return total
}

func (w *ControlWorker) authorize(req *http.Request) {
	if w.upstreamAuth != "" {
		req.Header.Set("Authorization", "Bearer "+w.upstreamAuth)
		req.Header.Set("X-Session-Manager-Token", w.connectionToken)
		return
	}
	req.Header.Set("Authorization", "Bearer "+w.connectionToken)
}

func acceptsEventStream(headers map[string][]string) bool {
	for key, values := range headers {
		if strings.EqualFold(key, "Accept") {
			for _, value := range values {
				if strings.Contains(strings.ToLower(value), "text/event-stream") {
					return true
				}
			}
		}
	}
	return false
}
