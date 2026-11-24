package provisioner

import (
	"bytes"
	"context"
	"encoding/json"
	"log"
	"net/http"
	"net/url"
	"time"

	"github.com/google/uuid"
	core "github.com/takutakahashi/agentapi-proxy/internal/core/esmcontrol"
	"github.com/takutakahashi/agentapi-proxy/pkg/telemetry"
)

func (w *directRuntimeWorker) execute(ctx context.Context, command core.Command) {
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
	queueDelay := time.Since(command.CreatedAt)
	if err := telemetry.LoggedOperationErr(commandCtx, "provisioner.DirectRuntime.ExecuteCommand", func(operationCtx context.Context) error {
		w.executeRequest(operationCtx, command)
		return nil
	}, telemetry.String("session.id", command.SessionID), telemetry.String("session.command_id", command.ID),
		telemetry.String("session.command_path", command.Path), telemetry.Int64("session.queue_delay_ms", queueDelay.Milliseconds())); err != nil {
		log.Printf("[DIRECT_RUNTIME] command execution failed: %v", err)
	}
}

func (w *directRuntimeWorker) executeRequest(commandCtx context.Context, command core.Command) {
	if command.Method == http.MethodPost && (command.Path == "/internal/session-prompt" || command.Path == "/internal/session-interrupt-prompt") {
		w.executePromptCommand(commandCtx, command)
		return
	}
	if command.Method == http.MethodPost && command.Path == "/internal/checkpoint-session-state" {
		w.executeCheckpointCommand(commandCtx, command)
		return
	}
	targetBase := w.agentURL
	if command.Path == "/herdr/rpc" {
		targetBase = w.provisionerURL
	}
	target := targetBase + command.Path
	if command.RawQuery != "" {
		target += "?" + command.RawQuery
	}
	req, err := http.NewRequestWithContext(commandCtx, command.Method, target, bytes.NewReader(command.Body))
	if err != nil {
		w.postExecutionError(commandCtx, command, err)
		return
	}
	req.Header = http.Header(command.Headers).Clone()
	telemetry.InjectHTTP(commandCtx, req)
	upstreamStartedAt := time.Now()
	resp, err := w.client.Do(req)
	if err != nil {
		w.postExecutionError(commandCtx, command, err)
		return
	}
	upstreamRespondedAt := time.Now()
	defer closeDirectRuntimeResponse(resp.Body)

	w.forwardDirectRuntimeResponse(commandCtx, command, resp, directRuntimeResponseTiming{started: upstreamStartedAt, responded: upstreamRespondedAt})
}

func (w *directRuntimeWorker) executePromptCommand(commandCtx context.Context, command core.Command) {
	// Direct runtime sessions expose an ACP bridge. Force the ACP prompt path
	// here instead of guessing an HTTP endpoint from the configured agent name.
	if err := w.waitUntilAgentReady(commandCtx, command.ID); err != nil {
		log.Printf("[DIRECT_RUNTIME] prompt %s was not delivered because the agent did not become ready: %v", command.ID, err)
		return
	}
	if command.Path == "/internal/session-interrupt-prompt" {
		if err := telemetry.LoggedOperationErr(commandCtx, "provisioner.DirectRuntime.CancelAgent", func(operationCtx context.Context) error {
			return executeControlCommandAtBase(operationCtx, w.client, "acp", controlCommand{ID: command.ID + "-cancel", Type: "cancel"}, w.agentURL)
		}, telemetry.String("session.command_id", command.ID)); err != nil {
			w.postExecutionError(commandCtx, command, err)
			return
		}
	}
	err := telemetry.LoggedOperationErr(commandCtx, "provisioner.DirectRuntime.DeliverPrompt", func(operationCtx context.Context) error {
		return executeControlCommandAtBase(operationCtx, w.client, "acp", controlCommand{ID: command.ID, Type: "prompt", Payload: command.Body}, w.agentURL)
	}, telemetry.String("session.command_id", command.ID), telemetry.String("session.id", command.SessionID))
	if err != nil {
		w.postExecutionError(commandCtx, command, err)
		return
	}
	w.postFramesBestEffort(commandCtx, []core.ResponseFrame{{ID: uuid.NewString(), RequestID: command.ID, CommandStreamID: command.StreamID, Sequence: 0, Status: http.StatusNoContent, Done: true, CreatedAt: time.Now().UTC()}})
}

func (w *directRuntimeWorker) executeCheckpointCommand(commandCtx context.Context, command core.Command) {
	query, queryErr := url.ParseQuery(command.RawQuery)
	if queryErr != nil {
		log.Printf("[DIRECT_RUNTIME] checkpoint query contains invalid parameters: %v", queryErr)
	}
	payload, payloadErr := json.Marshal(map[string]interface{}{
		"snapshot_id": query.Get("snapshot_id"),
		"park_herdr":  query.Get("park_herdr") == "true",
	})
	if payloadErr != nil {
		w.postExecutionError(commandCtx, command, payloadErr)
		return
	}
	err := executeControlCommand(commandCtx, w.client, w.checkpointAgentType(commandCtx), controlCommand{Type: "checkpoint_session_state", Payload: payload})
	if status := sessionSetupCheckpointStatus(err); status != 0 {
		w.postFramesBestEffort(commandCtx, []core.ResponseFrame{{ID: uuid.NewString(), RequestID: command.ID, CommandStreamID: command.StreamID, Sequence: 0, Status: status, Done: true, CreatedAt: time.Now().UTC()}})
		return
	}
	if err != nil {
		w.postExecutionError(commandCtx, command, err)
		return
	}
	w.postFramesBestEffort(commandCtx, []core.ResponseFrame{{ID: uuid.NewString(), RequestID: command.ID, CommandStreamID: command.StreamID, Sequence: 0, Status: http.StatusNoContent, Done: true, CreatedAt: time.Now().UTC()}})
}
