package provisioner

import (
	"context"
	"io"
	"log"

	core "github.com/takutakahashi/agentapi-proxy/internal/core/esmcontrol"
)

func closeDirectRuntimeResponse(body io.Closer) {
	if err := body.Close(); err != nil {
		log.Printf("[DIRECT_RUNTIME] response close failed: %v", err)
	}
}

func (w *directRuntimeWorker) postFramesBestEffort(ctx context.Context, frames []core.ResponseFrame) {
	if err := w.postFrames(ctx, frames); err != nil {
		log.Printf("[DIRECT_RUNTIME] response frame upload failed: %v", err)
	}
}
