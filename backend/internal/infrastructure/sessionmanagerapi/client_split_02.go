package sessionmanagerapi

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	coreallocation "github.com/takutakahashi/agentapi-proxy/internal/core/sessionallocation"
	"github.com/takutakahashi/agentapi-proxy/internal/domain/entities"
	portrepos "github.com/takutakahashi/agentapi-proxy/internal/usecases/ports/repositories"
	"github.com/takutakahashi/agentapi-proxy/pkg/sessionsettings"
	"github.com/takutakahashi/agentapi-proxy/pkg/telemetry"
	closeutils "github.com/takutakahashi/agentapi-proxy/pkg/utils"
)

func (c *Client) DeleteSession(id string) error {
	return c.DeleteSessionContext(context.Background(), id)
}

func (c *Client) DeleteSessionContext(ctx context.Context, id string) error {
	return c.do(ctx, http.MethodDelete, "/sessions/"+url.PathEscape(id), nil, nil)
}

func (c *Client) SendMessage(ctx context.Context, id, message string) error {
	return c.do(ctx, http.MethodPost, "/sessions/"+url.PathEscape(id)+"/messages", messageRequest{Message: message}, nil)
}

func (c *Client) StopAgent(ctx context.Context, id string) error {
	return c.do(ctx, http.MethodPost, "/sessions/"+url.PathEscape(id)+"/stop", nil, nil)
}

func (c *Client) SuspendSession(ctx context.Context, id string) error {
	return c.do(ctx, http.MethodPost, "/sessions/"+url.PathEscape(id)+"/suspend", nil, nil)
}

func (c *Client) CreateSessionContextSnapshot(ctx context.Context, sessionID, snapshotID string) error {
	return c.do(ctx, http.MethodPost, "/sessions/"+url.PathEscape(sessionID)+"/context-snapshots/"+url.PathEscape(snapshotID), nil, nil)
}

func (c *Client) DeleteSessionContextSnapshot(ctx context.Context, snapshotID string) error {
	return c.do(ctx, http.MethodDelete, "/context-snapshots/"+url.PathEscape(snapshotID), nil, nil)
}

func (c *Client) GetMessages(ctx context.Context, id string) ([]portrepos.Message, error) {
	return telemetry.Operation(ctx, "sessionmanagerapi.Client.GetMessages", func(ctx context.Context) ([]portrepos.Message, error) {
		return c.getMessages(ctx, id)
	})
}

func (c *Client) getMessages(ctx context.Context, id string) ([]portrepos.Message, error) {
	var response messagesResponse
	if err := c.do(ctx, http.MethodGet, "/sessions/"+url.PathEscape(id)+"/messages", nil, &response); err != nil {
		return nil, err
	}
	return response.Messages, nil
}

// Shutdown intentionally does not stop the remote process or its sessions. It
// only stops client-side background work owned by the API process.
func (c *Client) Shutdown(time.Duration) error {
	c.statusCancel()
	return nil
}

func (c *Client) EnsureSessionWorkload(ctx context.Context, id string) (entities.Session, bool, error) {
	var response ensureWorkloadResponse
	if err := c.do(ctx, http.MethodPost, "/sessions/"+url.PathEscape(id)+"/ensure", nil, &response); err != nil {
		return nil, false, err
	}
	if response.Session == nil {
		return nil, response.Restoring, nil
	}
	return response.Session.entity(), response.Restoring, nil
}

func (c *Client) TouchSession(ctx context.Context, id string, at time.Time) error {
	return c.do(ctx, http.MethodPost, "/sessions/"+url.PathEscape(id)+"/touch", touchRequest{At: at}, nil)
}

func (c *Client) GetSessionSandboxDomains(ctx context.Context, id string) (*portrepos.SandboxDomains, error) {
	var response portrepos.SandboxDomains
	if err := c.do(ctx, http.MethodGet, "/sessions/"+url.PathEscape(id)+"/sandbox-domains", nil, &response); err != nil {
		return nil, err
	}
	return &response, nil
}

func (c *Client) UpdateSessionAnnotations(ctx context.Context, id string, patch entities.UpdateSessionAnnotationsRequest) (entities.SessionAnnotations, error) {
	var response annotationsResponse
	if err := c.do(ctx, http.MethodPatch, "/sessions/"+url.PathEscape(id)+"/annotations", patch, &response); err != nil {
		return entities.SessionAnnotations{}, err
	}
	return response.Annotations, nil
}

func (c *Client) CreateStockSession(ctx context.Context, dind bool) error {
	return c.do(ctx, http.MethodPost, stockPath(dind), nil, nil)
}

func (c *Client) CountStockSessions(ctx context.Context, dind bool) (int, error) {
	var response stockCountResponse
	if err := c.do(ctx, http.MethodGet, stockPath(dind), nil, &response); err != nil {
		return 0, err
	}
	return response.Count, nil
}

func (c *Client) CreateStockSessionForPool(ctx context.Context, pool string, dind bool) error {
	return c.do(ctx, http.MethodPost, stockPoolPath(pool, dind), nil, nil)
}

func (c *Client) CountStockSessionsForPool(ctx context.Context, pool string, dind bool) (int, error) {
	var response stockCountResponse
	if err := c.do(ctx, http.MethodGet, stockPoolPath(pool, dind), nil, &response); err != nil {
		return 0, err
	}
	return response.Count, nil
}

func (c *Client) PurgeStaleStockSessions(ctx context.Context) error {
	return c.do(ctx, http.MethodDelete, "/stock", nil, nil)
}

func stockPath(dind bool) string {
	return "/stock?dind=" + fmt.Sprintf("%t", dind)
}

func stockPoolPath(pool string, dind bool) string {
	values := url.Values{}
	values.Set("pool", pool)
	values.Set("dind", fmt.Sprintf("%t", dind))
	return "/stock?" + values.Encode()
}

func (c *Client) DeletePendingSessionAllocation(ctx context.Context, id string) (bool, error) {
	var response pendingAllocationDeleteResponse
	if err := c.do(ctx, http.MethodDelete, "/sessions/"+url.PathEscape(id)+"/pending-allocation", nil, &response); err != nil {
		return false, err
	}
	return response.Deleted, nil
}

func (c *Client) DeleteProvisionRequest(ctx context.Context, id string) error {
	return c.do(ctx, http.MethodDelete, "/sessions/"+url.PathEscape(id)+"/provision-request", nil, nil)
}

func (c *Client) SubmitExternalSessionAllocation(ctx context.Context, managerID, sessionID string, settings *sessionsettings.SessionSettings, request *entities.RunServerRequest, runtime *coreallocation.RuntimeBootstrap) error {
	input := submitExternalAllocationRequest{
		ManagerID:         managerID,
		ProvisionSettings: settings,
		Request:           request,
		Runtime:           runtime,
	}
	return c.do(ctx, http.MethodPost, "/allocations/external/"+url.PathEscape(sessionID), input, nil)
}

func (c *Client) NextSessionAllocation(ctx context.Context, wait time.Duration) (*coreallocation.AllocationRequest, bool, error) {
	return c.nextAllocation(ctx, "/allocations/next", wait, "")
}

func (c *Client) CompleteSessionAllocation(ctx context.Context, sessionID string, result coreallocation.AllocationResult) (*coreallocation.AllocationRequest, error) {
	var response coreallocation.AllocationRequest
	if err := c.do(ctx, http.MethodPost, "/allocations/"+url.PathEscape(sessionID)+"/result", result, &response); err != nil {
		return nil, err
	}
	return &response, nil
}

func (c *Client) NextExternalSessionAllocation(ctx context.Context, managerID string, wait time.Duration) (*coreallocation.AllocationRequest, bool, error) {
	return c.nextAllocation(ctx, "/allocations/external/next", wait, managerID)
}

func (c *Client) CompleteExternalSessionAllocation(ctx context.Context, sessionID string, result coreallocation.AllocationResult) (*coreallocation.AllocationRequest, error) {
	var response coreallocation.AllocationRequest
	if err := c.do(ctx, http.MethodPost, "/allocations/external/"+url.PathEscape(sessionID)+"/result", result, &response); err != nil {
		return nil, err
	}
	return &response, nil
}

func (c *Client) nextAllocation(ctx context.Context, path string, wait time.Duration, managerID string) (*coreallocation.AllocationRequest, bool, error) {
	query := make(url.Values)
	if wait > 0 {
		query.Set("wait", wait.String())
	}
	if managerID != "" {
		query.Set("manager_id", managerID)
	}
	if encoded := query.Encode(); encoded != "" {
		path += "?" + encoded
	}
	var response coreallocation.AllocationRequest
	status, err := c.doWithStatus(ctx, http.MethodGet, path, nil, &response)
	if status == http.StatusNoContent {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	return &response, true, nil
}

func (c *Client) do(ctx context.Context, method, path string, input, output any) error {
	_, err := c.doWithStatus(ctx, method, path, input, output)
	return err
}

func (c *Client) doWithStatus(ctx context.Context, method, path string, input, output any) (int, error) {
	var body io.Reader
	if input != nil {
		data, err := json.Marshal(input)
		if err != nil {
			return 0, fmt.Errorf("encode session-manager request: %w", err)
		}
		body = bytes.NewReader(data)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+RoutePrefix+path, body)
	if err != nil {
		return 0, fmt.Errorf("create session-manager request: %w", err)
	}
	req.Header.Set(echoAuthorizationHeader, "Bearer "+c.token)
	if input != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	response, err := c.http.Do(req)
	if err != nil {
		return 0, fmt.Errorf("call session-manager API: %w", err)
	}
	defer func() { closeutils.SafeCloseResponseBody(response.Body) }()

	if response.StatusCode < 200 || response.StatusCode >= 300 {
		message := readErrorMessage(response.Body)
		return response.StatusCode, &HTTPError{Method: method, Path: path, StatusCode: response.StatusCode, Message: message}
	}
	if response.StatusCode == http.StatusNoContent || output == nil {
		return response.StatusCode, nil
	}
	decoder := json.NewDecoder(io.LimitReader(response.Body, maxResponseBodyBytes))
	if err := decoder.Decode(output); err != nil {
		return response.StatusCode, fmt.Errorf("decode session-manager response: %w", err)
	}
	return response.StatusCode, nil
}

const echoAuthorizationHeader = "Authorization"

func readErrorMessage(body io.Reader) string {
	data, err := io.ReadAll(io.LimitReader(body, 64<<10))
	if err != nil {
		return "unable to read error response"
	}
	var response errorResponse
	if json.Unmarshal(data, &response) == nil && response.Error != "" {
		return response.Error
	}
	if message := strings.TrimSpace(string(data)); message != "" {
		return message
	}
	return http.StatusText(http.StatusInternalServerError)
}

func isHTTPStatus(err error, status int) bool {
	var httpErr *HTTPError
	return errors.As(err, &httpErr) && httpErr.StatusCode == status
}

func (c *Client) RestartSession(ctx context.Context, id, requestID string, settings *sessionsettings.SessionSettings) error {
	transport := *c.http
	transport.Timeout = 10 * time.Minute
	long := &Client{baseURL: c.baseURL, token: c.token, http: &transport}
	return long.do(ctx, http.MethodPost, "/sessions/"+url.PathEscape(id)+"/restart", struct {
		ID       string
		Settings *sessionsettings.SessionSettings
	}{requestID, settings}, nil)
}
func (c *Client) ValidateSessionRestart(ctx context.Context, id string, settings *sessionsettings.SessionSettings) error {
	return c.do(ctx, http.MethodPost, "/sessions/"+url.PathEscape(id)+"/restart/validate", settings, nil)
}
func (c *Client) PauseSession(ctx context.Context, id string) error {
	transport := *c.http
	transport.Timeout = 10 * time.Minute
	long := &Client{baseURL: c.baseURL, token: c.token, http: &transport}
	return long.do(ctx, http.MethodPost, "/sessions/"+url.PathEscape(id)+"/pause", nil, nil)
}
func (c *Client) CurrentSessionSettings(ctx context.Context, id string) (*sessionsettings.SessionSettings, error) {
	var settings sessionsettings.SessionSettings
	err := c.do(ctx, http.MethodGet, "/sessions/"+url.PathEscape(id)+"/settings", nil, &settings)
	return &settings, err
}
