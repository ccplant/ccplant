package client

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/takutakahashi/agentapi-proxy/internal/domain/entities"
	"github.com/takutakahashi/agentapi-proxy/pkg/utils"
)

// HTTPClient defines the interface for making HTTP requests
type HTTPClient interface {
	Do(req *http.Request) (*http.Response, error)
}

// ReportUsage sends response-level usage events collected by a session Stop hook.
func (c *Client) ReportUsage(ctx context.Context, batch *entities.UsageEventBatch) (*entities.UsageInsertResult, error) {
	body, err := json.Marshal(batch)
	if err != nil {
		return nil, fmt.Errorf("marshal usage events: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/internal/usage-events", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	if err := c.applyMiddlewares(req); err != nil {
		return nil, err
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { utils.SafeCloseResponseBody(resp.Body) }()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		data, readErr := readResponseBody(resp.Body)
		if readErr != nil {
			return nil, readErr
		}
		return nil, fmt.Errorf("report usage failed: status %d: %s", resp.StatusCode, strings.TrimSpace(data))
	}
	var result entities.UsageInsertResult
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, err
	}
	return &result, nil
}

// RequestMiddleware is a function that modifies an HTTP request
type RequestMiddleware func(*http.Request) error

// Client represents an ccplant client
type Client struct {
	baseURL     string
	httpClient  HTTPClient
	middlewares []RequestMiddleware
}

// ClientOption is a function that configures a Client
type ClientOption func(*Client)

// NewClient creates a new ccplant client with options
func NewClient(baseURL string, opts ...ClientOption) *Client {
	httpClient := utils.NewDefaultHTTPClient()
	if socketPath, ok := unixSocketPath(baseURL); ok {
		transport := http.DefaultTransport.(*http.Transport).Clone()
		transport.Proxy = nil
		transport.DialContext = func(ctx context.Context, _, _ string) (net.Conn, error) {
			return (&net.Dialer{}).DialContext(ctx, "unix", socketPath)
		}
		httpClient = &http.Client{Timeout: 30 * time.Second, Transport: transport}
		baseURL = "http://localhost"
	}
	c := &Client{
		baseURL:     baseURL,
		httpClient:  httpClient,
		middlewares: []RequestMiddleware{},
	}

	for _, opt := range opts {
		opt(c)
	}

	return c
}

// unixSocketPath resolves the local endpoint forms accepted by the CLI.
// http+unix uses a percent-encoded socket path in the authority, while unix
// accepts the more familiar unix:///absolute/path spelling.
func unixSocketPath(endpoint string) (string, bool) {
	if encoded, ok := strings.CutPrefix(endpoint, "http+unix://"); ok {
		path, err := url.PathUnescape(encoded)
		return path, err == nil && strings.HasPrefix(path, "/")
	}
	u, err := url.Parse(endpoint)
	if err != nil {
		return "", false
	}
	switch u.Scheme {
	case "unix":
		return u.Path, u.Path != ""
	default:
		return "", false
	}
}

// WithHTTPClient sets a custom HTTP client
func WithHTTPClient(httpClient HTTPClient) ClientOption {
	return func(c *Client) {
		c.httpClient = httpClient
	}
}

// WithAPIKeyAuth adds API key authentication middleware
func WithAPIKeyAuth(apiKey string) ClientOption {
	return func(c *Client) {
		c.middlewares = append(c.middlewares, func(req *http.Request) error {
			if apiKey != "" {
				req.Header.Set("Authorization", "Bearer "+apiKey)
			}
			return nil
		})
	}
}

// WithRequestMiddleware adds a custom request middleware
func WithRequestMiddleware(middleware RequestMiddleware) ClientOption {
	return func(c *Client) {
		c.middlewares = append(c.middlewares, middleware)
	}
}

// applyMiddlewares applies all registered middlewares to the request
func (c *Client) applyMiddlewares(req *http.Request) error {
	for _, middleware := range c.middlewares {
		if err := middleware(req); err != nil {
			return fmt.Errorf("middleware error: %w", err)
		}
	}
	return nil
}

// StartParams contains optional session startup parameters.
type StartParams struct {
	Message      string `json:"message,omitempty"`
	Oneshot      bool   `json:"oneshot,omitempty"`
	AgentType    string `json:"agent_type,omitempty"`
	AuthProxy    *bool  `json:"auth_proxy,omitempty"`
	ConnectionID string `json:"connection_id,omitempty"`
}

// StartRequest represents the request body for starting a new agentapi server
type StartRequest struct {
	ContextTemplateID string            `json:"context_template_id,omitempty"`
	Environment       map[string]string `json:"environment,omitempty"`
	Tags              map[string]string `json:"tags,omitempty"`
	Params            *StartParams      `json:"params,omitempty"`
	Scope             string            `json:"scope,omitempty"`
	TeamID            string            `json:"team_id,omitempty"`
}

// StartResponse represents the response from starting a new agentapi server
type StartResponse struct {
	SessionID string `json:"session_id"`
}

// StartDryRunResponse describes a resolved start without creating a session.
type StartDryRunResponse struct {
	DryRun                   bool                     `json:"dry_run"`
	SessionID                string                   `json:"session_id"`
	Decision                 string                   `json:"decision"`
	EffectiveRequest         map[string]interface{}   `json:"effective_request,omitempty"`
	ResolvedSessionProfileID string                   `json:"resolved_session_profile_id,omitempty"`
	Placement                map[string]interface{}   `json:"placement,omitempty"`
	Settings                 map[string]interface{}   `json:"settings,omitempty"`
	Reuse                    map[string]interface{}   `json:"reuse,omitempty"`
	Effects                  []map[string]interface{} `json:"effects,omitempty"`
	Redactions               []string                 `json:"redactions,omitempty"`
	Warnings                 []string                 `json:"warnings,omitempty"`
}

// SessionInfo represents information about a session
type SessionInfo struct {
	SessionID          string             `json:"session_id"`
	AllocatedSessionID string             `json:"allocated_session_id,omitempty"`
	UserID             string             `json:"user_id"`
	Status             string             `json:"status"`
	StartedAt          time.Time          `json:"started_at"`
	Port               int                `json:"port"`
	Tags               map[string]string  `json:"tags,omitempty"`
	Annotations        SessionAnnotations `json:"annotations,omitempty"`
	Metadata           SessionMetadata    `json:"metadata,omitempty"`
}

// SearchResponse represents the response from searching sessions
type SearchResponse struct {
	Sessions []SessionInfo `json:"sessions"`
}

// SessionAnnotations contains user-managed annotations attached to a session.
type SessionAnnotations struct {
	PRURL       string `json:"pr_url,omitempty"`
	IssueURL    string `json:"issue_url,omitempty"`
	Description string `json:"description,omitempty"`
	RunningTask string `json:"running_task,omitempty"`
}

// SessionMetadata contains additional session metadata returned by /search.
type SessionMetadata struct {
	Description string `json:"description,omitempty"`
}

// UpdateSessionAnnotationsRequest partially updates user-managed session annotations.
// Nil fields are left unchanged; an explicit empty string clears that annotation.
type UpdateSessionAnnotationsRequest struct {
	PRURL       *string `json:"pr_url,omitempty"`
	IssueURL    *string `json:"issue_url,omitempty"`
	Description *string `json:"description,omitempty"`
	RunningTask *string `json:"running_task,omitempty"`
}

// UpdateSessionAnnotationsResponse is returned after updating session annotations.
type UpdateSessionAnnotationsResponse struct {
	SessionID   string             `json:"session_id"`
	Annotations SessionAnnotations `json:"annotations"`
	Metadata    SessionMetadata    `json:"metadata,omitempty"`
}

// Message represents an agentapi message
type Message struct {
	Content   string          `json:"content"`
	Type      string          `json:"type"` // "user" or "raw"
	Role      string          `json:"role,omitempty"`
	Timestamp *time.Time      `json:"timestamp,omitempty"`
	Time      *time.Time      `json:"time,omitempty"`
	ID        json.RawMessage `json:"id,omitempty"`
}

// GetTimestamp returns the message timestamp, checking both Time and Timestamp fields.
func (m *Message) GetTimestamp() *time.Time {
	if m.Time != nil {
		return m.Time
	}
	return m.Timestamp
}

// MessageResponse represents the response from sending a message
type MessageResponse struct {
	OK bool `json:"ok"`
}

// MessagesResponse represents the response from getting messages
type MessagesResponse struct {
	Messages []Message `json:"messages"`
}

// StatusResponse represents the agent status
type StatusResponse struct {
	Status string `json:"status"` // "stable" or "running"
}

// DeleteResponse represents the response from deleting a session
type DeleteResponse struct {
	Message   string `json:"message"`
	SessionID string `json:"session_id"`
}

// Start creates a new agentapi session
func (c *Client) Start(ctx context.Context, req *StartRequest) (*StartResponse, error) {
	jsonData, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, "POST", c.baseURL+"/start", bytes.NewBuffer(jsonData))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")

	if err := c.applyMiddlewares(httpReq); err != nil {
		return nil, err
	}

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("failed to send request: %w", err)
	}
	defer func() {
		utils.SafeCloseResponseBody(resp.Body)
	}()

	if resp.StatusCode != http.StatusOK {
		body, readErr := readResponseBody(resp.Body)
		if readErr != nil {
			return nil, readErr
		}
		return nil, fmt.Errorf("server returned status %d: %s", resp.StatusCode, body)
	}

	var startResp StartResponse
	if err := json.NewDecoder(resp.Body).Decode(&startResp); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	return &startResp, nil
}

// DryRunStart resolves a session start request without creating a session.
func (c *Client) DryRunStart(ctx context.Context, req *StartRequest) (*StartDryRunResponse, error) {
	jsonData, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/start?dry_run=true", bytes.NewBuffer(jsonData))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	if err := c.applyMiddlewares(httpReq); err != nil {
		return nil, err
	}
	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("failed to send request: %w", err)
	}
	defer func() { utils.SafeCloseResponseBody(resp.Body) }()
	if resp.StatusCode != http.StatusOK {
		body, readErr := readResponseBody(resp.Body)
		if readErr != nil {
			return nil, readErr
		}
		return nil, fmt.Errorf("server returned status %d: %s", resp.StatusCode, body)
	}
	var preview StartDryRunResponse
	if err := json.NewDecoder(resp.Body).Decode(&preview); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}
	return &preview, nil
}

// Search lists and filters sessions
func (c *Client) Search(ctx context.Context, status string) (*SearchResponse, error) {
	return c.SearchWithTags(ctx, status, nil)
}

// SearchWithTags lists and filters sessions with tag support
func (c *Client) SearchWithTags(ctx context.Context, status string, tags map[string]string) (*SearchResponse, error) {
	u, err := url.Parse(c.baseURL + "/search")
	if err != nil {
		return nil, fmt.Errorf("failed to parse URL: %w", err)
	}

	q := u.Query()
	if status != "" {
		q.Set("status", status)
	}
	// Add tag filters
	for key, value := range tags {
		q.Set("tag."+key, value)
	}
	u.RawQuery = q.Encode()

	httpReq, err := http.NewRequestWithContext(ctx, "GET", u.String(), nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	if err := c.applyMiddlewares(httpReq); err != nil {
		return nil, err
	}

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("failed to send request: %w", err)
	}
	defer func() {
		utils.SafeCloseResponseBody(resp.Body)
	}()

	if resp.StatusCode != http.StatusOK {
		body, readErr := readResponseBody(resp.Body)
		if readErr != nil {
			return nil, readErr
		}
		return nil, fmt.Errorf("server returned status %d: %s", resp.StatusCode, body)
	}

	var searchResp SearchResponse
	if err := json.NewDecoder(resp.Body).Decode(&searchResp); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	return &searchResp, nil
}

// GetSessionByID retrieves a single session by its ID.
// It calls the search endpoint and returns the first session with a matching SessionID.
// Returns an error if the session is not found.
