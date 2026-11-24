package sessionmanagerapi

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"sync"
	"time"

	coreallocation "github.com/takutakahashi/agentapi-proxy/internal/core/sessionallocation"
	"github.com/takutakahashi/agentapi-proxy/internal/domain/entities"
	portrepos "github.com/takutakahashi/agentapi-proxy/internal/usecases/ports/repositories"
	"github.com/takutakahashi/agentapi-proxy/pkg/sessionsettings"
	"github.com/takutakahashi/agentapi-proxy/pkg/telemetry"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
)

const maxResponseBodyBytes = 16 << 20

// HTTPError is returned for a non-2xx response from the session-manager API.
type HTTPError struct {
	Method     string
	Path       string
	StatusCode int
	Message    string
}

func (e *HTTPError) Error() string {
	return fmt.Sprintf("session-manager API %s %s returned %d: %s", e.Method, e.Path, e.StatusCode, e.Message)
}

// Client is the API-side implementation of the session lifecycle and allocation
// ports. It has no Kubernetes dependency.
type Client struct {
	baseURL                  string
	token                    string
	http                     *http.Client
	provisionSettingsBuilder portrepos.RemoteProvisionSettingsBuilder
	statusOnce               sync.Once
	statusCtx                context.Context
	statusCancel             context.CancelFunc
	statusMu                 sync.Mutex
	statusSubscribers        map[uint64]*statusSubscriber
	statusNextSubscriberID   uint64
	statusKnown              map[string]string
}

type statusSubscriber struct {
	out   chan portrepos.SessionStatusEvent
	wake  chan struct{}
	stop  chan struct{}
	queue []portrepos.SessionStatusEvent
}

// SetProvisionSettingsBuilder installs the API-side settings resolver. The
// public API owns credential decryption; the isolated session manager receives
// only the already-resolved, ephemeral provision payload.
func (c *Client) SetProvisionSettingsBuilder(builder portrepos.RemoteProvisionSettingsBuilder) {
	c.provisionSettingsBuilder = builder
}

type ClientOption func(*Client)

// WithHTTPClient replaces the default client. It is primarily useful for
// custom transports and tests.
func WithHTTPClient(client *http.Client) ClientOption {
	return func(c *Client) {
		if client != nil {
			c.http = client
		}
	}
}

func NewClient(baseURL, bearerToken string, options ...ClientOption) (*Client, error) {
	baseURL = strings.TrimRight(strings.TrimSpace(baseURL), "/")
	parsed, err := url.Parse(baseURL)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return nil, fmt.Errorf("valid session-manager API base URL is required")
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return nil, fmt.Errorf("unsupported session-manager API URL scheme %q", parsed.Scheme)
	}
	if bearerToken == "" {
		return nil, errors.New("session-manager API bearer token is required")
	}
	statusCtx, statusCancel := context.WithCancel(context.Background())
	client := &Client{
		baseURL: baseURL,
		token:   bearerToken,
		// Stock creation can wait up to 120 seconds for its Kubernetes workload.
		// Keep this hop alive for the entire session-manager operation.
		http:              &http.Client{Timeout: 150 * time.Second, Transport: otelhttp.NewTransport(http.DefaultTransport)},
		statusCtx:         statusCtx,
		statusCancel:      statusCancel,
		statusSubscribers: make(map[uint64]*statusSubscriber),
		statusKnown:       make(map[string]string),
	}
	for _, option := range options {
		option(client)
	}
	return client, nil
}

var _ portrepos.SessionManager = (*Client)(nil)
var _ portrepos.SessionWorkloadEnsurer = (*Client)(nil)
var _ portrepos.SessionSuspender = (*Client)(nil)
var _ portrepos.SessionToucher = (*Client)(nil)
var _ portrepos.SessionSandboxDomainReader = (*Client)(nil)
var _ portrepos.SessionStatusWatcher = (*Client)(nil)
var _ portrepos.SessionMessageWatcher = (*Client)(nil)
var _ portrepos.RemoteProvisionSettingsBuilder = (*Client)(nil)
var _ coreallocation.Queue = (*Client)(nil)
var _ SessionAnnotationUpdater = (*Client)(nil)
var _ StockManager = (*Client)(nil)
var _ PendingAllocationDeleter = (*Client)(nil)
var _ ProvisionRequestDeleter = (*Client)(nil)

func (c *Client) Health(ctx context.Context) error {
	return c.do(ctx, http.MethodGet, "/health", nil, nil)
}

func (c *Client) ExternalRuntimeProfile() *sessionsettings.RuntimeProfile {
	var profile sessionsettings.RuntimeProfile
	if err := c.do(context.Background(), http.MethodGet, "/runtime-profile", nil, &profile); err != nil {
		return nil
	}
	return &profile
}

// SubscribeStatusEvents preserves the public API's watcher capability without
// giving it Redis or Kubernetes credentials. All subscribers on this client
// share one manager poller so session-list work does not multiply with the
// number of connected browsers.
func (c *Client) SubscribeStatusEvents() (<-chan portrepos.SessionStatusEvent, func()) {
	subscriber := &statusSubscriber{
		out:  make(chan portrepos.SessionStatusEvent, 32),
		wake: make(chan struct{}, 1),
		stop: make(chan struct{}),
	}

	c.statusMu.Lock()
	id := c.statusNextSubscriberID
	c.statusNextSubscriberID++
	now := time.Now()
	for sessionID, status := range c.statusKnown {
		subscriber.queue = append(subscriber.queue, portrepos.SessionStatusEvent{SessionID: sessionID, Status: status, Timestamp: now})
	}
	c.statusSubscribers[id] = subscriber
	hasSnapshot := len(subscriber.queue) > 0
	c.statusMu.Unlock()

	go c.runStatusSubscriber(subscriber)
	if hasSnapshot {
		subscriber.wake <- struct{}{}
	}
	c.statusOnce.Do(func() { go c.runStatusPoller() })

	var cancelOnce sync.Once
	cancel := func() {
		cancelOnce.Do(func() {
			c.statusMu.Lock()
			if _, exists := c.statusSubscribers[id]; exists {
				delete(c.statusSubscribers, id)
				close(subscriber.stop)
			}
			c.statusMu.Unlock()
		})
	}
	return subscriber.out, cancel
}

func (c *Client) runStatusPoller() {
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		c.pollSessionStatuses()
		select {
		case <-c.statusCtx.Done():
			return
		case <-ticker.C:
		}
	}
}

func (c *Client) pollSessionStatuses() {
	sessions, err := c.ListSessionsContext(c.statusCtx, entities.SessionFilter{})
	if err != nil {
		return
	}
	next := make(map[string]string, len(sessions))
	now := time.Now()

	c.statusMu.Lock()
	defer c.statusMu.Unlock()
	for _, session := range sessions {
		next[session.ID()] = session.Status()
		if previous, exists := c.statusKnown[session.ID()]; exists && previous == session.Status() {
			continue
		}
		event := portrepos.SessionStatusEvent{SessionID: session.ID(), Status: session.Status(), Timestamp: now}
		for _, subscriber := range c.statusSubscribers {
			subscriber.queue = append(subscriber.queue, event)
			select {
			case subscriber.wake <- struct{}{}:
			default:
			}
		}
	}
	c.statusKnown = next
}

func (c *Client) runStatusSubscriber(subscriber *statusSubscriber) {
	defer close(subscriber.out)
	for {
		select {
		case <-subscriber.stop:
			return
		case <-c.statusCtx.Done():
			return
		case <-subscriber.wake:
		}

		for {
			c.statusMu.Lock()
			if len(subscriber.queue) == 0 {
				c.statusMu.Unlock()
				break
			}
			event := subscriber.queue[0]
			subscriber.queue = subscriber.queue[1:]
			c.statusMu.Unlock()

			select {
			case subscriber.out <- event:
			case <-subscriber.stop:
				return
			case <-c.statusCtx.Done():
				return
			}
		}
	}
}

func (c *Client) SubscribeMessageEvents(sessionID string) (<-chan portrepos.SessionMessageEvent, func()) {
	ctx, cancel := context.WithCancel(context.Background())
	events := make(chan portrepos.SessionMessageEvent, 1)
	go func() {
		defer close(events)
		var last time.Time
		initialized := false
		ticker := time.NewTicker(time.Second)
		defer ticker.Stop()
		for {
			session, err := c.GetSessionContext(ctx, sessionID)
			if err == nil && session != nil {
				current := session.LastMessageAt()
				if initialized && current.After(last) {
					select {
					case events <- portrepos.SessionMessageEvent{SessionID: sessionID, Timestamp: current}:
					case <-ctx.Done():
						return
					}
				}
				last, initialized = current, true
			}
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
			}
		}
	}()
	return events, cancel
}

func (c *Client) CreateSession(ctx context.Context, id string, request *entities.RunServerRequest, webhookPayload []byte) (entities.Session, error) {
	return telemetry.Operation(ctx, "sessionmanagerapi.Client.CreateSession", func(ctx context.Context) (entities.Session, error) {
		return c.createSession(ctx, id, request, webhookPayload)
	})
}

func (c *Client) createSession(ctx context.Context, id string, request *entities.RunServerRequest, webhookPayload []byte) (entities.Session, error) {
	if c.provisionSettingsBuilder != nil && request.ProvisionSettings == nil {
		settings, err := c.provisionSettingsBuilder.BuildRemoteProvisionSettings(ctx, id, request)
		if err != nil {
			return nil, fmt.Errorf("build API-side provision settings: %w", err)
		}
		request = cloneRunServerRequest(request)
		request.ProvisionSettings = settings
	}
	var response SessionDTO
	input := createSessionRequest{Request: request, WebhookPayload: webhookPayload}
	if err := c.do(ctx, http.MethodPost, "/sessions/"+url.PathEscape(id), input, &response); err != nil {
		return nil, err
	}
	return response.entity(), nil
}

func (c *Client) BuildRemoteProvisionSettings(ctx context.Context, id string, request *entities.RunServerRequest) (*sessionsettings.SessionSettings, error) {
	if c.provisionSettingsBuilder != nil {
		return c.provisionSettingsBuilder.BuildRemoteProvisionSettings(ctx, id, request)
	}
	var response sessionsettings.SessionSettings
	input := provisionSettingsRequest{Request: request}
	if err := c.do(ctx, http.MethodPost, "/sessions/"+url.PathEscape(id)+"/provision-settings", input, &response); err != nil {
		return nil, err
	}
	return &response, nil
}

func cloneRunServerRequest(request *entities.RunServerRequest) *entities.RunServerRequest {
	if request == nil {
		return nil
	}
	copy := *request
	return &copy
}

func (c *Client) GetSession(id string) entities.Session {
	session, err := c.GetSessionContext(context.Background(), id)
	if err != nil {
		return nil
	}
	return session
}

// GetSessionContext is the error-preserving variant of GetSession. A 404 is
// returned as (nil, nil).
func (c *Client) GetSessionContext(ctx context.Context, id string) (entities.Session, error) {
	var response SessionDTO
	err := c.do(ctx, http.MethodGet, "/sessions/"+url.PathEscape(id), nil, &response)
	if isHTTPStatus(err, http.StatusNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return response.entity(), nil
}

func (c *Client) ListSessions(filter entities.SessionFilter) []entities.Session {
	sessions, err := c.ListSessionsContext(context.Background(), filter)
	if err != nil {
		return nil
	}
	return sessions
}

// ListSessionsContext is the error-preserving variant of ListSessions.
func (c *Client) ListSessionsContext(ctx context.Context, filter entities.SessionFilter) ([]entities.Session, error) {
	query := make(url.Values)
	query.Set("user_id", filter.UserID)
	query.Set("status", filter.Status)
	query.Set("scope", string(filter.Scope))
	query.Set("team_id", filter.TeamID)
	if len(filter.TeamIDs) > 0 {
		query.Set("team_ids", strings.Join(filter.TeamIDs, ","))
	}
	keys := make([]string, 0, len(filter.Tags))
	for key := range filter.Tags {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		query.Set("tag."+key, filter.Tags[key])
	}
	var response sessionsResponse
	path := "/sessions"
	if encoded := query.Encode(); encoded != "" {
		path += "?" + encoded
	}
	if err := c.do(ctx, http.MethodGet, path, nil, &response); err != nil {
		return nil, err
	}
	result := make([]entities.Session, 0, len(response.Sessions))
	for _, dto := range response.Sessions {
		result = append(result, dto.entity())
	}
	return result, nil
}
