package herdr

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"
	"unicode"

	"github.com/labstack/echo/v4"
)

// Register adds opt-in runtime endpoints. The enclosing session proxy is the
// authentication boundary, just as for /rpc. Enable only in an isolated Pod;
// sockets owned by this OS user are intentionally visible to its session owner.
func Register(e *echo.Echo) {
	home, err := os.UserHomeDir()
	if err != nil {
		return
	}
	state := os.Getenv("XDG_STATE_HOME")
	if state == "" {
		state = filepath.Join(home, ".local", "state")
	}
	register(e, NewClient(), filepath.Join(state, "ccplant", "herdr-input.pending"))
}

func register(e *echo.Echo, client *Client, pendingPath string) {
	var sending sync.Mutex
	var disabled atomic.Bool
	if _, err := os.Lstat(pendingPath); !os.IsNotExist(err) {
		disabled.Store(true)
	}
	e.GET("/herdr/agents", func(c echo.Context) error {
		c.Response().Header().Set("Cache-Control", "no-store")
		ctx, cancel := context.WithTimeout(c.Request().Context(), 8*time.Second)
		defer cancel()
		agents, unavailable := client.Agents(ctx)
		if disabled.Load() {
			for i := range agents {
				agents[i].CanInput = false
			}
		}
		return c.JSON(http.StatusOK, map[string]any{"agents": agents, "unavailable_servers": unavailable, "input_disabled": disabled.Load()})
	})
	e.GET("/herdr/output", func(c echo.Context) error {
		c.Response().Header().Set("Cache-Control", "no-store")
		target := Target{c.QueryParam("server_id"), c.QueryParam("pane_id"), c.QueryParam("generation")}
		text, err := client.Output(c.Request().Context(), target)
		if err != nil {
			if errors.Is(err, ErrChanged) {
				return httpError(err)
			}
			return echo.NewHTTPError(http.StatusServiceUnavailable, "Herdr output is unavailable")
		}
		return c.JSON(http.StatusOK, map[string]string{"text": text})
	})
	e.POST("/herdr/input", func(c echo.Context) error {
		c.Response().Header().Set("Cache-Control", "no-store")
		if disabled.Load() {
			return echo.NewHTTPError(http.StatusConflict, "Input disabled after uncertain delivery; terminate the target terminal or entire Pod before operator recovery")
		}
		// No queue or automatic retries: a delivery timeout may have happened
		// after input reached the TUI. The user must inspect it before retrying.
		if !sending.TryLock() {
			return echo.NewHTTPError(http.StatusConflict, "Another input is in progress")
		}
		release := true
		defer func() {
			if release {
				sending.Unlock()
			}
		}()
		var input struct {
			Target
			Text string `json:"text"`
		}
		decoder := json.NewDecoder(http.MaxBytesReader(c.Response(), c.Request().Body, 20000))
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(&input); err != nil {
			return echo.NewHTTPError(http.StatusBadRequest, "Invalid input")
		}
		if decoder.Decode(new(any)) != io.EOF || input.ServerID == "" || input.PaneID == "" || input.Generation == "" {
			return echo.NewHTTPError(http.StatusBadRequest, "A single input and complete target are required")
		}
		if strings.TrimSpace(input.Text) == "" || len(input.Text) > 16000 || strings.ContainsFunc(input.Text, func(r rune) bool { return unicode.IsControl(r) && r != '\n' && r != '\t' }) {
			return echo.NewHTTPError(http.StatusBadRequest, "Text must be 1–16000 bytes without terminal control characters")
		}
		// Write-ahead latch: bridge restart/crash must not forget a queued hc
		// send. It contains no input or transcript, and is never auto-cleared on
		// startup. O_EXCL also excludes concurrent runtime processes.
		if err := os.MkdirAll(filepath.Dir(pendingPath), 0700); err != nil {
			return echo.NewHTTPError(http.StatusServiceUnavailable, "Cannot reserve input delivery")
		}
		marker, err := os.OpenFile(pendingPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
		if err != nil {
			disabled.Store(true)
			return echo.NewHTTPError(http.StatusConflict, "Input delivery is already pending or disabled")
		}
		if err := marker.Close(); err != nil {
			disabled.Store(true)
			return httpError(err)
		}
		// Once accepted, finish observing delivery even if the browser leaves.
		// hc does not cancel queued terminal.send when its socket disconnects.
		ctx, cancel := context.WithTimeout(context.Background(), 80*time.Second)
		defer cancel()
		if err := client.Send(ctx, input.Target, input.Text); err != nil {
			if errors.Is(err, ErrChanged) {
				if removeErr := os.Remove(pendingPath); removeErr != nil {
					disabled.Store(true)
					release = false
				}
			} else {
				release = false
				disabled.Store(true)
			}
			return httpError(err)
		}
		if err := os.Remove(pendingPath); err != nil {
			disabled.Store(true)
			release = false
			return httpError(err)
		}
		return c.JSON(http.StatusOK, map[string]bool{"sent": true})
	})
}

func httpError(err error) error {
	if errors.Is(err, ErrChanged) {
		return echo.NewHTTPError(http.StatusConflict, err.Error())
	}
	return echo.NewHTTPError(http.StatusServiceUnavailable, "Herdr request failed. Delivery may still be pending; further input is disabled across runtime restarts. Terminate the target terminal or entire Pod before operator recovery")
}
