package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"

	"github.com/labstack/echo/v4"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/takutakahashi/agentapi-proxy/internal/domain/entities"
)

// resourceRequester dispatches MCP resource operations through the existing
// HTTP API. This keeps validation, authorization, and response semantics in one
// place instead of duplicating each resource's business logic in MCP handlers.
type resourceRequester interface {
	Do(context.Context, string, string, url.Values, any) (any, error)
}

type echoResourceRequester struct {
	echo    *echo.Echo
	headers http.Header
	user    *entities.User
}

func newEchoResourceRequester(e *echo.Echo, headers http.Header, user *entities.User) resourceRequester {
	return &echoResourceRequester{echo: e, headers: headers.Clone(), user: user}
}

func (r *echoResourceRequester) Do(ctx context.Context, method, path string, query url.Values, body any) (any, error) {
	var reader io.Reader
	if body != nil {
		encoded, err := json.Marshal(body)
		if err != nil {
			return nil, fmt.Errorf("encode request body: %w", err)
		}
		reader = bytes.NewReader(encoded)
	}
	if len(query) > 0 {
		path += "?" + query.Encode()
	}
	req := httptest.NewRequest(method, path, reader).WithContext(ctx)
	req.Header = r.headers.Clone()
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if r.user != nil {
		req = req.WithContext(withUser(req.Context(), r.user))
	}
	recorder := httptest.NewRecorder()
	r.echo.ServeHTTP(recorder, req)
	response := recorder.Result()
	defer func() { _ = response.Body.Close() }()
	data, err := io.ReadAll(io.LimitReader(response.Body, 8<<20))
	if err != nil {
		return nil, fmt.Errorf("read API response: %w", err)
	}
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		message := strings.TrimSpace(string(data))
		if message == "" {
			message = http.StatusText(response.StatusCode)
		}
		return nil, fmt.Errorf("API returned status %d: %s", response.StatusCode, message)
	}
	if len(bytes.TrimSpace(data)) == 0 {
		return map[string]any{"success": true}, nil
	}
	var result any
	if err := json.Unmarshal(data, &result); err != nil {
		return map[string]any{"message": strings.TrimSpace(string(data))}, nil
	}
	return result, nil
}

type ResourceOutput struct {
	Result any `json:"result" jsonschema:"API response for the resource operation"`
}

type ResourceIDInput struct {
	ID string `json:"id" jsonschema:"Resource ID"`
}

type ResourceBodyInput struct {
	Body map[string]any `json:"body" jsonschema:"Resource fields using the same JSON shape as the REST API"`
}

type ResourceIDBodyInput struct {
	ID   string         `json:"id" jsonschema:"Resource ID"`
	Body map[string]any `json:"body" jsonschema:"Fields to update using merge semantics"`
}

type ResourceListInput struct {
	Status string `json:"status,omitempty" jsonschema:"Optional status filter"`
	Scope  string `json:"scope,omitempty" jsonschema:"Optional ownership scope: user or team"`
	TeamID string `json:"team_id,omitempty" jsonschema:"Team ID when filtering team-owned resources"`
}

type WebhookListInput struct {
	ResourceListInput
	Type string `json:"type,omitempty" jsonschema:"Optional webhook type filter: github or custom"`
}

type SlackbotSimulationInput struct {
	ID      string         `json:"id" jsonschema:"SlackBot ID"`
	Event   map[string]any `json:"event" jsonschema:"Synthetic Slack event request"`
	Execute bool           `json:"execute,omitempty" jsonschema:"Create or reuse a real session; defaults to dry-run"`
}

type CreateUserInput struct {
	Username    string `json:"username" jsonschema:"Unique username"`
	DisplayName string `json:"display_name,omitempty" jsonschema:"Display name"`
	Email       string `json:"email,omitempty" jsonschema:"Email address"`
	Role        string `json:"role,omitempty" jsonschema:"Role: user or admin"`
}

type CreateUserTokenInput struct {
	UserID    string `json:"user_id" jsonschema:"User ID"`
	Name      string `json:"name" jsonschema:"Token name"`
	ExpiresIn string `json:"expires_in,omitempty" jsonschema:"Token lifetime such as 720h"`
}

type UserTokenInput struct {
	UserID  string `json:"user_id" jsonschema:"User ID"`
	TokenID string `json:"token_id" jsonschema:"API token ID"`
}

func (s *MCPServer) registerResourceTools() {
	if s.resourceRequester == nil {
		slog.Warn("[MCP] Resource tools unavailable because no resource requester was configured")
		return
	}
	add := func(name, description string, handler any) {
		tool := &mcp.Tool{Name: name, Description: description}
		switch h := handler.(type) {
		case func(context.Context, *mcp.CallToolRequest, ResourceListInput) (*mcp.CallToolResult, ResourceOutput, error):
			mcp.AddTool(s.server, tool, h)
		case func(context.Context, *mcp.CallToolRequest, WebhookListInput) (*mcp.CallToolResult, ResourceOutput, error):
			mcp.AddTool(s.server, tool, h)
		case func(context.Context, *mcp.CallToolRequest, ResourceIDInput) (*mcp.CallToolResult, ResourceOutput, error):
			mcp.AddTool(s.server, tool, h)
		case func(context.Context, *mcp.CallToolRequest, ResourceBodyInput) (*mcp.CallToolResult, ResourceOutput, error):
			mcp.AddTool(s.server, tool, h)
		case func(context.Context, *mcp.CallToolRequest, ResourceIDBodyInput) (*mcp.CallToolResult, ResourceOutput, error):
			mcp.AddTool(s.server, tool, h)
		case func(context.Context, *mcp.CallToolRequest, SlackbotSimulationInput) (*mcp.CallToolResult, ResourceOutput, error):
			mcp.AddTool(s.server, tool, h)
		case func(context.Context, *mcp.CallToolRequest, CreateUserInput) (*mcp.CallToolResult, ResourceOutput, error):
			mcp.AddTool(s.server, tool, h)
		case func(context.Context, *mcp.CallToolRequest, CreateUserTokenInput) (*mcp.CallToolResult, ResourceOutput, error):
			mcp.AddTool(s.server, tool, h)
		case func(context.Context, *mcp.CallToolRequest, UserTokenInput) (*mcp.CallToolResult, ResourceOutput, error):
			mcp.AddTool(s.server, tool, h)
		default:
			panic(fmt.Sprintf("unsupported MCP handler type for %s", name))
		}
	}

	add("create_asset", "Upload an HTML asset", s.handleCreateAsset)
	add("list_schedules", "List schedules", s.listHandler("/schedules"))
	add("get_schedule", "Get a schedule", s.getHandler("schedule", "/schedules/"))
	add("create_schedule", "Create a schedule", s.createHandler("schedule", "/schedules"))
	add("update_schedule", "Partially update a schedule", s.updateHandler("schedule", "/schedules/"))
	add("delete_schedule", "Delete a schedule", s.deleteHandler("schedule", "/schedules/"))
	add("list_webhooks", "List webhooks", s.handleListWebhooks)
	add("get_webhook", "Get a webhook", s.getHandler("webhook", "/webhooks/"))
	add("create_webhook", "Create a webhook", s.createHandler("webhook", "/webhooks"))
	add("update_webhook", "Partially update a webhook", s.updateHandler("webhook", "/webhooks/"))
	add("delete_webhook", "Delete a webhook", s.deleteHandler("webhook", "/webhooks/"))
	add("regenerate_webhook_secret", "Regenerate a webhook HMAC secret; the new secret is returned once", s.handleRegenerateWebhookSecret)
	add("list_slackbots", "List SlackBots", s.listHandler("/slackbots"))
	add("get_slackbot", "Get a SlackBot", s.getHandler("SlackBot", "/slackbots/"))
	add("create_slackbot", "Create a SlackBot", s.createHandler("SlackBot", "/slackbots"))
	add("update_slackbot", "Partially update a SlackBot", s.updateHandler("SlackBot", "/slackbots/"))
	add("delete_slackbot", "Delete a SlackBot", s.deleteHandler("SlackBot", "/slackbots/"))
	add("simulate_slackbot", "Evaluate a synthetic Slack event, in dry-run mode by default", s.handleSimulateSlackbot)
	add("create_user", "Create a local user (admin only)", s.handleCreateUser)
	add("get_user", "Get a local user (admin only)", s.getHandler("user", "/admin/users/"))
	add("create_user_token", "Create a local user API token; the plaintext token is returned once (admin only)", s.handleCreateUserToken)
	add("list_user_tokens", "List a local user's API tokens (admin only)", s.handleListUserTokens)
	add("revoke_user_token", "Revoke a local user's API token (admin only)", s.handleRevokeUserToken)
	slog.Info("[MCP] Registered 23 resource tools")
}

func (s *MCPServer) callResource(ctx context.Context, method, path string, query url.Values, body any) (ResourceOutput, error) {
	result, err := s.resourceRequester.Do(ctx, method, path, query, body)
	if err != nil {
		return ResourceOutput{}, err
	}
	return ResourceOutput{Result: result}, nil
}

func validateID(kind, id string) error {
	if strings.TrimSpace(id) == "" {
		return fmt.Errorf("%s id is required", kind)
	}
	return nil
}

func listQuery(input ResourceListInput) url.Values {
	query := make(url.Values)
	if input.Status != "" {
		query.Set("status", input.Status)
	}
	if input.Scope != "" {
		query.Set("scope", input.Scope)
	}
	if input.TeamID != "" {
		query.Set("team_id", input.TeamID)
	}
	return query
}

func (s *MCPServer) listHandler(path string) func(context.Context, *mcp.CallToolRequest, ResourceListInput) (*mcp.CallToolResult, ResourceOutput, error) {
	return func(ctx context.Context, _ *mcp.CallToolRequest, input ResourceListInput) (*mcp.CallToolResult, ResourceOutput, error) {
		out, err := s.callResource(ctx, http.MethodGet, path, listQuery(input), nil)
		return nil, out, err
	}
}

func (s *MCPServer) getHandler(kind, prefix string) func(context.Context, *mcp.CallToolRequest, ResourceIDInput) (*mcp.CallToolResult, ResourceOutput, error) {
	return func(ctx context.Context, _ *mcp.CallToolRequest, input ResourceIDInput) (*mcp.CallToolResult, ResourceOutput, error) {
		if err := validateID(kind, input.ID); err != nil {
			return nil, ResourceOutput{}, err
		}
		out, err := s.callResource(ctx, http.MethodGet, prefix+url.PathEscape(input.ID), nil, nil)
		return nil, out, err
	}
}

func (s *MCPServer) createHandler(kind, path string) func(context.Context, *mcp.CallToolRequest, ResourceBodyInput) (*mcp.CallToolResult, ResourceOutput, error) {
	return func(ctx context.Context, _ *mcp.CallToolRequest, input ResourceBodyInput) (*mcp.CallToolResult, ResourceOutput, error) {
		if input.Body == nil {
			return nil, ResourceOutput{}, fmt.Errorf("%s body is required", kind)
		}
		out, err := s.callResource(ctx, http.MethodPost, path, nil, input.Body)
		return nil, out, err
	}
}

func (s *MCPServer) updateHandler(kind, prefix string) func(context.Context, *mcp.CallToolRequest, ResourceIDBodyInput) (*mcp.CallToolResult, ResourceOutput, error) {
	return func(ctx context.Context, _ *mcp.CallToolRequest, input ResourceIDBodyInput) (*mcp.CallToolResult, ResourceOutput, error) {
		if err := validateID(kind, input.ID); err != nil {
			return nil, ResourceOutput{}, err
		}
		if input.Body == nil {
			return nil, ResourceOutput{}, fmt.Errorf("%s body is required", kind)
		}
		out, err := s.callResource(ctx, http.MethodPut, prefix+url.PathEscape(input.ID), nil, input.Body)
		return nil, out, err
	}
}

func (s *MCPServer) deleteHandler(kind, prefix string) func(context.Context, *mcp.CallToolRequest, ResourceIDInput) (*mcp.CallToolResult, ResourceOutput, error) {
	return func(ctx context.Context, _ *mcp.CallToolRequest, input ResourceIDInput) (*mcp.CallToolResult, ResourceOutput, error) {
		if err := validateID(kind, input.ID); err != nil {
			return nil, ResourceOutput{}, err
		}
		out, err := s.callResource(ctx, http.MethodDelete, prefix+url.PathEscape(input.ID), nil, nil)
		return nil, out, err
	}
}

func (s *MCPServer) handleCreateAsset(ctx context.Context, _ *mcp.CallToolRequest, input ResourceBodyInput) (*mcp.CallToolResult, ResourceOutput, error) {
	out, err := s.callResource(ctx, http.MethodPost, "/assets", nil, input.Body)
	return nil, out, err
}

func (s *MCPServer) handleListWebhooks(ctx context.Context, _ *mcp.CallToolRequest, input WebhookListInput) (*mcp.CallToolResult, ResourceOutput, error) {
	query := listQuery(input.ResourceListInput)
	if input.Type != "" {
		query.Set("type", input.Type)
	}
	out, err := s.callResource(ctx, http.MethodGet, "/webhooks", query, nil)
	return nil, out, err
}

func (s *MCPServer) handleRegenerateWebhookSecret(ctx context.Context, _ *mcp.CallToolRequest, input ResourceIDInput) (*mcp.CallToolResult, ResourceOutput, error) {
	if err := validateID("webhook", input.ID); err != nil {
		return nil, ResourceOutput{}, err
	}
	out, err := s.callResource(ctx, http.MethodPost, "/webhooks/"+url.PathEscape(input.ID)+"/regenerate-secret", nil, nil)
	return nil, out, err
}

func (s *MCPServer) handleSimulateSlackbot(ctx context.Context, _ *mcp.CallToolRequest, input SlackbotSimulationInput) (*mcp.CallToolResult, ResourceOutput, error) {
	if err := validateID("SlackBot", input.ID); err != nil {
		return nil, ResourceOutput{}, err
	}
	if input.Event == nil {
		return nil, ResourceOutput{}, fmt.Errorf("event is required")
	}
	input.Event["dry_run"] = !input.Execute
	out, err := s.callResource(ctx, http.MethodPost, "/slackbots/"+url.PathEscape(input.ID)+"/simulate", nil, input.Event)
	return nil, out, err
}

func (s *MCPServer) handleCreateUser(ctx context.Context, _ *mcp.CallToolRequest, input CreateUserInput) (*mcp.CallToolResult, ResourceOutput, error) {
	if strings.TrimSpace(input.Username) == "" {
		return nil, ResourceOutput{}, fmt.Errorf("username is required")
	}
	out, err := s.callResource(ctx, http.MethodPost, "/admin/users", nil, input)
	return nil, out, err
}

func (s *MCPServer) handleCreateUserToken(ctx context.Context, _ *mcp.CallToolRequest, input CreateUserTokenInput) (*mcp.CallToolResult, ResourceOutput, error) {
	if err := validateID("user", input.UserID); err != nil {
		return nil, ResourceOutput{}, err
	}
	if strings.TrimSpace(input.Name) == "" {
		return nil, ResourceOutput{}, fmt.Errorf("token name is required")
	}
	body := map[string]any{"name": input.Name}
	if input.ExpiresIn != "" {
		body["expires_in"] = input.ExpiresIn
	}
	out, err := s.callResource(ctx, http.MethodPost, "/admin/users/"+url.PathEscape(input.UserID)+"/api-tokens", nil, body)
	return nil, out, err
}

func (s *MCPServer) handleListUserTokens(ctx context.Context, _ *mcp.CallToolRequest, input ResourceIDInput) (*mcp.CallToolResult, ResourceOutput, error) {
	if err := validateID("user", input.ID); err != nil {
		return nil, ResourceOutput{}, err
	}
	out, err := s.callResource(ctx, http.MethodGet, "/admin/users/"+url.PathEscape(input.ID)+"/api-tokens", nil, nil)
	return nil, out, err
}

func (s *MCPServer) handleRevokeUserToken(ctx context.Context, _ *mcp.CallToolRequest, input UserTokenInput) (*mcp.CallToolResult, ResourceOutput, error) {
	if err := validateID("user", input.UserID); err != nil {
		return nil, ResourceOutput{}, err
	}
	if err := validateID("token", input.TokenID); err != nil {
		return nil, ResourceOutput{}, err
	}
	path := "/admin/users/" + url.PathEscape(input.UserID) + "/api-tokens/" + url.PathEscape(input.TokenID)
	out, err := s.callResource(ctx, http.MethodDelete, path, nil, nil)
	return nil, out, err
}
