package provisioner

import (
	"compress/gzip"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	core "github.com/takutakahashi/agentapi-proxy/internal/core/esmcontrol"
	"github.com/takutakahashi/agentapi-proxy/pkg/sessionsettings"
)

func TestDirectRuntimeFrameUploadRetriesWithoutGzipForOlderParent(t *testing.T) {
	var attempts atomic.Int32
	parent := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempt := attempts.Add(1)
		if attempt == 1 {
			if r.Header.Get("Content-Encoding") != "gzip" {
				t.Error("first upload must be compressed")
			}
			reader, err := gzip.NewReader(r.Body)
			if err != nil {
				t.Error(err)
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			if _, err := io.Copy(io.Discard, reader); err != nil {
				t.Error(err)
			}
			if err := reader.Close(); err != nil {
				t.Error(err)
			}
			w.WriteHeader(http.StatusUnsupportedMediaType)
			return
		}
		if r.Header.Get("Content-Encoding") != "" {
			t.Error("retry must not be compressed")
		}
		var body struct {
			Frames []core.ResponseFrame `json:"frames"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
		}
		if len(body.Frames) != 1 || body.Frames[0].ID != "same-frame" || len(body.Frames[0].Body) != 128*1024 {
			t.Errorf("retry did not preserve original frames: count=%d", len(body.Frames))
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer parent.Close()
	worker := &directRuntimeWorker{cfg: &sessionsettings.ParentRuntimeConfig{Endpoint: parent.URL, SessionID: "session", Generation: 1}, client: parent.Client()}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := worker.postFrames(ctx, []core.ResponseFrame{{ID: "same-frame", Body: make([]byte, 128*1024)}}); err != nil {
		t.Fatal(err)
	}
	if attempts.Load() != 2 {
		t.Fatalf("attempts = %d, want 2", attempts.Load())
	}
}
