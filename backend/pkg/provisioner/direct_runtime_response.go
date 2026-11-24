package provisioner

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
	core "github.com/takutakahashi/agentapi-proxy/internal/core/esmcontrol"
)

type directRuntimeResponseTiming struct{ started, responded time.Time }

type directRuntimeFrameBatch struct {
	command  core.Command
	timing   directRuntimeResponseTiming
	sequence int64
	frames   []core.ResponseFrame
	bytes    int
}

func newDirectRuntimeFrameBatch(command core.Command, response *http.Response, timing directRuntimeResponseTiming) *directRuntimeFrameBatch {
	start := core.ResponseFrame{ID: uuid.NewString(), RequestID: command.ID, Status: response.StatusCode,
		Headers: map[string][]string(response.Header.Clone()), CreatedAt: time.Now().UTC()}
	return &directRuntimeFrameBatch{command: command, timing: timing, frames: []core.ResponseFrame{start}}
}

func (w *directRuntimeWorker) forwardDirectRuntimeResponse(ctx context.Context, command core.Command, response *http.Response, timing directRuntimeResponseTiming) {
	batch := newDirectRuntimeFrameBatch(command, response, timing)
	stream := acceptsRuntimeEventStream(command.Headers) || strings.HasPrefix(response.Header.Get("Content-Type"), "text/event-stream")
	frameSize := 512 * 1024
	if stream {
		frameSize = 64 * 1024
		batch.frames[0].CommandStreamID = command.StreamID
		if err := batch.flush(ctx, w); err != nil {
			return
		}
	}
	buffer := make([]byte, frameSize)
	for {
		n, readErr := readDirectRuntimeFrame(response.Body, buffer, stream)
		if n > 0 {
			batch.appendBody(buffer[:n])
		}
		if readErr != nil {
			batch.finish(ctx, w, readErr)
			return
		}
		// Batch ordinary responses, but forward SSE chunks immediately.
		if stream || batch.bytes >= 512*1024 {
			if err := batch.flush(ctx, w); err != nil {
				return
			}
		}
	}
}

func readDirectRuntimeFrame(body io.Reader, buffer []byte, stream bool) (int, error) {
	n, err := body.Read(buffer)
	if stream || err != nil || n == len(buffer) {
		return n, err
	}
	additional, err := io.ReadFull(body, buffer[n:])
	if errors.Is(err, io.ErrUnexpectedEOF) {
		err = io.EOF
	}
	return n + additional, err
}

func (b *directRuntimeFrameBatch) appendBody(body []byte) {
	b.sequence++
	b.frames = append(b.frames, core.ResponseFrame{ID: uuid.NewString(), RequestID: b.command.ID,
		Sequence: b.sequence, Body: append([]byte(nil), body...), CreatedAt: time.Now().UTC()})
	b.bytes += len(body)
}

func (b *directRuntimeFrameBatch) flush(ctx context.Context, w *directRuntimeWorker) error {
	if err := w.postFrames(ctx, b.frames); err != nil {
		return err
	}
	b.frames = nil
	b.bytes = 0
	return nil
}

func (b *directRuntimeFrameBatch) finish(ctx context.Context, w *directRuntimeWorker, readErr error) {
	completed := time.Now()
	b.sequence++
	frame := core.ResponseFrame{ID: uuid.NewString(), RequestID: b.command.ID, CommandStreamID: b.command.StreamID,
		Sequence: b.sequence, Done: true, UpstreamTTFBMS: b.timing.responded.Sub(b.timing.started).Milliseconds(),
		UpstreamReadMS: completed.Sub(b.timing.responded).Milliseconds(), CreatedAt: time.Now().UTC()}
	if readErr != io.EOF {
		frame.Error = readErr.Error()
	}
	b.frames = append(b.frames, frame)
	// Completion frames must survive cancellation of the proxied request.
	uploadCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 2*time.Minute)
	defer cancel()
	w.postFramesBestEffort(uploadCtx, b.frames)
}
