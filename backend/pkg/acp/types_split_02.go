package acp

import (
	"encoding/json"
)

type RequestPermissionToolCall struct {
	ToolCallId string          `json:"toolCallId"`
	Kind       string          `json:"kind,omitempty"`     // e.g. "switch_mode" for ExitPlanMode
	RawInput   json.RawMessage `json:"rawInput,omitempty"` // raw tool input JSON
}

// RequestPermissionParams is the params for "session/request_permission"
// (agent→client request).
type RequestPermissionParams struct {
	SessionId string                    `json:"sessionId"`
	Options   []PermissionOption        `json:"options"`
	ToolCall  RequestPermissionToolCall `json:"toolCall"`
}

// RequestPermissionOutcome is the inner outcome object of RequestPermissionResult.
type RequestPermissionOutcome struct {
	Outcome  string `json:"outcome"`            // "selected" or "cancelled"
	OptionId string `json:"optionId,omitempty"` // set when outcome="selected"
}

// RequestPermissionResult is the response to "session/request_permission".
// The ACP spec requires: {outcome: {outcome: "selected", optionId: "<id>"}}
type RequestPermissionResult struct {
	Outcome RequestPermissionOutcome `json:"outcome"`
}

// ----------------------------------------------------------------------------
// Filesystem requests (agent→client bidirectional RPC)
// ----------------------------------------------------------------------------

// FsReadTextFileParams is the params for "fs/read_text_file".
type FsReadTextFileParams struct {
	Path string `json:"path"`
}

// FsReadTextFileResult is the result of "fs/read_text_file".
type FsReadTextFileResult struct {
	Text string `json:"text"`
}

// FsWriteTextFileParams is the params for "fs/write_text_file".
type FsWriteTextFileParams struct {
	Path string `json:"path"`
	Text string `json:"text"`
}

// FsWriteTextFileResult is the result of "fs/write_text_file".
type FsWriteTextFileResult struct{}
