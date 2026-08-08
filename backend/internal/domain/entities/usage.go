package entities

import "time"

// UsageEvent is token usage emitted by one model response.
type UsageEvent struct {
	EventID             string    `json:"event_id"`
	SessionID           string    `json:"session_id,omitempty"`
	AgentSessionID      string    `json:"agent_session_id,omitempty"`
	TurnID              string    `json:"turn_id,omitempty"`
	ResponseID          string    `json:"response_id,omitempty"`
	UserID              string    `json:"-"`
	Scope               string    `json:"-"`
	TeamID              string    `json:"-"`
	AgentType           string    `json:"agent_type"`
	Provider            string    `json:"provider,omitempty"`
	Model               string    `json:"model"`
	InputTokens         int64     `json:"input_tokens"`
	OutputTokens        int64     `json:"output_tokens"`
	CachedInputTokens   int64     `json:"cached_input_tokens,omitempty"`
	CacheCreationTokens int64     `json:"cache_creation_tokens,omitempty"`
	ReasoningTokens     int64     `json:"reasoning_tokens,omitempty"`
	OccurredAt          time.Time `json:"occurred_at"`
}

type UsageEventBatch struct {
	SessionID string       `json:"session_id"`
	Events    []UsageEvent `json:"events"`
}

type UsageInsertResult struct {
	Accepted   int `json:"accepted"`
	Duplicates int `json:"duplicates"`
}

type UsageQuery struct {
	SessionID string
	UserID    string
	TeamID    string
	From      *time.Time
	To        *time.Time
}

type UsageSummary struct {
	Events              int64            `json:"events"`
	InputTokens         int64            `json:"input_tokens"`
	OutputTokens        int64            `json:"output_tokens"`
	CachedInputTokens   int64            `json:"cached_input_tokens"`
	CacheCreationTokens int64            `json:"cache_creation_tokens"`
	ReasoningTokens     int64            `json:"reasoning_tokens"`
	ByModel             []UsageBreakdown `json:"by_model"`
	BySession           []UsageBreakdown `json:"by_session"`
}

type UsageBreakdown struct {
	Key                 string `json:"key"`
	Events              int64  `json:"events"`
	InputTokens         int64  `json:"input_tokens"`
	OutputTokens        int64  `json:"output_tokens"`
	CachedInputTokens   int64  `json:"cached_input_tokens"`
	CacheCreationTokens int64  `json:"cache_creation_tokens"`
	ReasoningTokens     int64  `json:"reasoning_tokens"`
}
