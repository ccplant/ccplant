package bridge

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strconv"

	"github.com/labstack/echo/v4"
)

func (s *Server) handleSSE(c echo.Context) error {
	w := c.Response()
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)

	lastEventIDStr := c.Request().Header.Get("Last-Event-ID")
	// Default: subscribe from current position (no history replay).
	// Clients that want history should call GET /messages first, then reconnect
	// with Last-Event-ID to resume from a known point.
	lastEventID := parseLastEventID(lastEventIDStr)
	log.Printf("[acp-server] GET /sse connected (remoteAddr=%s, lastEventID=%d)", c.RealIP(), lastEventID)

	ch, history, nextIdx, cancel := s.bridge.SubscribeFrom(lastEventID)
	defer func() {
		cancel()
		log.Printf("[acp-server] GET /sse disconnected (remoteAddr=%s)", c.RealIP())
	}()

	flusher, hasFlusher := w.Writer.(http.Flusher)

	// Replay history events that the client missed (or all of them on first connect).
	historyStartIdx := lastEventID + 1
	if historyStartIdx < 0 {
		historyStartIdx = 0
	}
	if err := replaySSEHistory(w, historyStartIdx, history); err != nil {
		return err
	}
	if hasFlusher {
		flusher.Flush()
	}

	ctx := c.Request().Context()
	for {
		select {
		case <-ctx.Done():
			return nil
		case raw, open := <-ch:
			if !open {
				return nil
			}
			if err := writeSSE(w, "id: %d\nevent: message\ndata: %s\n\n", nextIdx, raw); err != nil {
				return err
			}
			nextIdx++
			if hasFlusher {
				flusher.Flush()
			}
		}
	}
}

func parseLastEventID(value string) int {
	if value == "" {
		return SubscribeFromCurrent
	}
	id, err := strconv.Atoi(value)
	if err != nil {
		return SubscribeFromCurrent
	}
	return id
}

func replaySSEHistory(writer io.Writer, start int, history []json.RawMessage) error {
	for index, message := range history {
		if err := writeSSE(writer, "id: %d\nevent: message\ndata: %s\n\n", start+index, message); err != nil {
			return err
		}
	}
	return nil
}

// GET /events
//
// agentapi-compatible SSE stream consumed by the proxy's watchAgentAPIStatus goroutine.
// On connect it immediately emits a status_change event with the current status so
// reconnecting consumers always get a consistent starting point, then streams every
// subsequent status transition.
func (s *Server) handleEvents(c echo.Context) error {
	w := c.Response()
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)

	flusher, hasFlusher := w.Writer.(http.Flusher)

	// Emit the current status immediately so the consumer doesn't have to wait
	// for the next transition.
	if err := writeSSE(w, "event: status_change\ndata: {\"status\":%q}\n\n", s.bridge.Status()); err != nil {
		return err
	}
	if hasFlusher {
		flusher.Flush()
	}

	ch, cancel := s.bridge.SubscribeStatus()
	defer cancel()

	ctx := c.Request().Context()
	for {
		select {
		case <-ctx.Done():
			return nil
		case status, open := <-ch:
			if !open {
				return nil
			}
			if err := writeSSE(w, "event: status_change\ndata: {\"status\":%q}\n\n", status); err != nil {
				return err
			}
			if hasFlusher {
				flusher.Flush()
			}
		}
	}
}

func writeSSE(writer io.Writer, format string, args ...interface{}) error {
	written, err := fmt.Fprintf(writer, format, args...)
	if err != nil {
		return err
	}
	if written == 0 {
		return io.ErrShortWrite
	}
	return nil
}

// ----------------------------------------------------------------------------
// Helpers
// ----------------------------------------------------------------------------

// rpcErrorResp builds a JSON-RPC 2.0 error response object.
func rpcErrorResp(id *json.RawMessage, code int, message string) map[string]interface{} {
	return map[string]interface{}{
		"jsonrpc": "2.0",
		"id":      id,
		"error": map[string]interface{}{
			"code":    code,
			"message": message,
		},
	}
}
