package controllers

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/labstack/echo/v4"
	portrepos "github.com/takutakahashi/agentapi-proxy/internal/usecases/ports/repositories"
	"github.com/takutakahashi/agentapi-proxy/pkg/hmacutil"
	closeutils "github.com/takutakahashi/agentapi-proxy/pkg/utils"
)

func (c *ACPController) runtimeRequiresOutbound() bool {
	manager, ok := c.sessionManagerProvider.GetSessionManager().(interface{ RuntimeTrafficRequiresOutbound() bool })
	return ok && manager.RuntimeTrafficRequiresOutbound()
}

func (c *ACPController) handleRemoteSessionSSE(ctx echo.Context, route *portrepos.SessionRoute) error {
	w := ctx.Response()
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)

	flusher, hasFlusher := w.Writer.(http.Flusher)
	if hasFlusher {
		flusher.Flush()
	}
	lastEventID := ctx.Request().Header.Get("Last-Event-ID")
	if lastEventID == "" {
		lastEventID = ctx.QueryParam("lastEventId")
	}
	c.streamRemoteBridgeSSE(ctx.Request().Context(), route, w, flusher, hasFlusher, lastEventID)
	return nil
}

// sseEvent holds the fields of a parsed SSE event.
type sseEvent struct {
	id   string
	data string
	err  error
}

// streamBridgeSSE proxies the ACP bridge's /sse stream to the current response writer.
//
// When the bridge-side SSE connection drops (e.g. transient network hiccup or bridge
// restart), this function immediately reconnects to the bridge and resumes from the
// last forwarded event ID — keeping the client-side SSE stream alive the whole time.
// History replay is handled by the bridge's Last-Event-ID support, so no messages are
// lost during the reconnection window.
//
// A keepalive comment is sent every 15 seconds so that NGINX proxy_read_timeout does not
// drop the client connection while the agent is idle between messages.
func (c *ACPController) streamBridgeSSE(
	ctx context.Context,
	addr string,
	w io.Writer,
	flusher http.Flusher,
	hasFlusher bool,
	lastEventID string,
) {
	c.streamSSE(ctx, w, flusher, hasFlusher, lastEventID, func(ctx context.Context, lastEventID string) (<-chan sseEvent, func()) {
		return c.dialBridgeSSE(ctx, addr, lastEventID)
	})
}

func (c *ACPController) streamRemoteBridgeSSE(
	ctx context.Context,
	route *portrepos.SessionRoute,
	w io.Writer,
	flusher http.Flusher,
	hasFlusher bool,
	lastEventID string,
) {
	c.streamSSE(ctx, w, flusher, hasFlusher, lastEventID, func(ctx context.Context, lastEventID string) (<-chan sseEvent, func()) {
		return c.dialRemoteBridgeSSE(ctx, route, lastEventID)
	})
}

func (c *ACPController) streamSSE(
	ctx context.Context,
	w io.Writer,
	flusher http.Flusher,
	hasFlusher bool,
	lastEventID string,
	dial func(context.Context, string) (<-chan sseEvent, func()),
) {
	keepaliveTicker := time.NewTicker(15 * time.Second)
	defer keepaliveTicker.Stop()

	const (
		backoffMin = time.Second
		backoffMax = 30 * time.Second
	)
	backoff := backoffMin

	for {
		// Check client disconnection before each (re)connection attempt.
		select {
		case <-ctx.Done():
			return
		default:
		}

		connStart := time.Now()
		eventCh, cleanup := dial(ctx, lastEventID)

		gotEvent := false

		// Drain events from this bridge connection until it drops or the client leaves.
	drainLoop:
		for {
			select {
			case <-ctx.Done():
				cleanup()
				return
			case ev, ok := <-eventCh:
				if !ok {
					cleanup()
					log.Printf("[ACP] SSE: bridge SSE dropped (lastEventID=%s), reconnecting...", lastEventID)
					break drainLoop
				}
				if ev.err != nil {
					cleanup()
					log.Printf("[ACP] SSE: bridge read error (lastEventID=%s): %v", lastEventID, ev.err)
					break drainLoop
				}
				gotEvent = true
				if ev.id != "" {
					lastEventID = ev.id
					_, _ = fmt.Fprintf(w, "id: %s\nevent: message\ndata: %s\n\n", ev.id, ev.data)
				} else {
					_, _ = fmt.Fprintf(w, "event: message\ndata: %s\n\n", ev.data)
				}
				if hasFlusher {
					flusher.Flush()
				}
			case <-keepaliveTicker.C:
				_, _ = fmt.Fprintf(w, ": keepalive\n\n")
				if hasFlusher {
					flusher.Flush()
				}
			}
		}

		// Reset backoff when the connection was healthy (delivered at least one event
		// or stayed alive for over 5 seconds). Otherwise use exponential backoff so
		// permanently unreachable bridges (deleted sessions) don't spin-log.
		elapsed := time.Since(connStart)
		if gotEvent || elapsed >= 5*time.Second {
			backoff = backoffMin
		}

		wait := backoff - elapsed
		if wait > 0 {
			select {
			case <-ctx.Done():
				return
			case <-time.After(wait):
			}
		}

		// Double backoff for next attempt, capped at backoffMax.
		backoff *= 2
		if backoff > backoffMax {
			backoff = backoffMax
		}
	}
}

// dialBridgeSSE opens a single SSE connection to the bridge and returns a channel of
// parsed events plus a cleanup function. The goroutine exits when the response body
// closes (bridge dropped or ctx cancelled via cleanup).
func (c *ACPController) dialBridgeSSE(
	ctx context.Context,
	addr, lastEventID string,
) (<-chan sseEvent, func()) {
	eventCh := make(chan sseEvent, 8)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://"+addr+"/sse", nil)
	if err != nil {
		log.Printf("[ACP] SSE: failed to create bridge request: %v", err)
		close(eventCh)
		return eventCh, func() {}
	}
	req.Header.Set("Accept", "text/event-stream")
	if lastEventID != "" {
		req.Header.Set("Last-Event-ID", lastEventID)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		log.Printf("[ACP] SSE: failed to connect to bridge: %v", err)
		close(eventCh)
		return eventCh, func() {}
	}

	// cleanup closes the response body, which unblocks the scanner goroutine.
	cleanup := func() { closeutils.SafeCloseResponseBody(resp.Body) }

	go func() {
		defer close(eventCh)
		scanner := bufio.NewScanner(resp.Body)
		var cur sseEvent
		for scanner.Scan() {
			line := scanner.Text()
			switch {
			case strings.HasPrefix(line, "id: "):
				cur.id = strings.TrimPrefix(line, "id: ")
			case strings.HasPrefix(line, "data: "):
				cur.data = strings.TrimPrefix(line, "data: ")
			case line == "": // blank line = event boundary
				if cur.data != "" {
					select {
					case eventCh <- cur:
					case <-ctx.Done():
						return
					}
				}
				cur = sseEvent{}
			}
		}
		if scanErr := scanner.Err(); scanErr != nil {
			select {
			case eventCh <- sseEvent{err: scanErr}:
			case <-ctx.Done():
			}
		}
	}()

	return eventCh, cleanup
}

func (c *ACPController) dialRemoteBridgeSSE(
	ctx context.Context,
	route *portrepos.SessionRoute,
	lastEventID string,
) (<-chan sseEvent, func()) {
	eventCh := make(chan sseEvent, 8)
	targetURL := ""
	directRuntime := route.Transport == portrepos.SessionRouteTransportDirectRuntime
	tunnelKey := route.ManagerID
	if directRuntime {
		tunnelKey = route.SessionID
	}
	useTunnel := c.esmControlTunnel != nil && c.esmControlTunnel.IsConnected(ctx, tunnelKey)
	if useTunnel {
		if directRuntime {
			targetURL = "http://session.local/sse"
		} else {
			targetURL = "http://esm.local/" + route.RemoteSessionID + "/sse"
		}
	} else {
		log.Printf("[ACP] SSE: outbound ESM control connection is unavailable")
		close(eventCh)
		return eventCh, func() {}
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, targetURL, nil)
	if err != nil {
		log.Printf("[ACP] SSE: failed to create remote bridge request: %v", err)
		close(eventCh)
		return eventCh, func() {}
	}
	req.Header.Set("Accept", "text/event-stream")
	if lastEventID != "" {
		req.Header.Set("Last-Event-ID", lastEventID)
	}
	ts := hmacutil.NowTimestamp()
	msg := hmacutil.BuildMessage(http.MethodGet, req.URL.RequestURI(), ts, nil)
	req.Header.Set("X-Hub-Signature-256", hmacutil.Sign([]byte(route.HMACSecret), msg))
	req.Header.Set(hmacutil.TimestampHeader, ts)

	resp, err := c.esmControlTunnel.Do(ctx, tunnelKey, route.SessionID, route.RemoteSessionID, req)
	if err != nil {
		log.Printf("[ACP] SSE: failed to connect to remote bridge: %v", err)
		close(eventCh)
		return eventCh, func() {}
	}
	if resp.StatusCode != http.StatusOK {
		closeutils.SafeCloseResponseBody(resp.Body)
		log.Printf("[ACP] SSE: remote bridge returned HTTP %d", resp.StatusCode)
		close(eventCh)
		return eventCh, func() {}
	}

	cleanup := func() { closeutils.SafeCloseResponseBody(resp.Body) }
	go scanSSE(ctx, resp.Body, eventCh)
	return eventCh, cleanup
}

func scanSSE(ctx context.Context, body io.Reader, eventCh chan<- sseEvent) {
	defer close(eventCh)
	scanner := bufio.NewScanner(body)
	var cur sseEvent
	for scanner.Scan() {
		line := scanner.Text()
		switch {
		case strings.HasPrefix(line, "id: "):
			cur.id = strings.TrimPrefix(line, "id: ")
		case strings.HasPrefix(line, "data: "):
			cur.data = strings.TrimPrefix(line, "data: ")
		case line == "":
			if cur.data != "" {
				select {
				case eventCh <- cur:
				case <-ctx.Done():
					return
				}
			}
			cur = sseEvent{}
		}
	}
	if err := scanner.Err(); err != nil {
		select {
		case eventCh <- sseEvent{err: err}:
		case <-ctx.Done():
		}
	}
}
