package provisioner

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	core "github.com/takutakahashi/agentapi-proxy/internal/core/esmcontrol"
	"github.com/takutakahashi/agentapi-proxy/pkg/sessionsettings"
)

type directRuntimeWorker struct {
	cfg            *sessionsettings.ParentRuntimeConfig
	client         *http.Client
	agentURL       string
	provisionerURL string
	instanceID     string
	active         sync.Map
}

var (
	errDirectRuntimeFenced       = errors.New("direct runtime generation fenced")
	errDirectRuntimeUnauthorized = errors.New("direct runtime credential rejected")
)

var directRuntimeReadyPollInterval = time.Second

const directRuntimeStatusRefreshInterval = 5 * time.Second

const directRuntimeAgentHost = "agent.local"
const directRuntimeProvisionerHost = "provisioner.local"

type directRuntimeTransport struct {
	upstream    http.RoundTripper
	agent       http.RoundTripper
	provisioner http.RoundTripper
}

func (t *directRuntimeTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if req.URL.Host == directRuntimeAgentHost {
		return t.agent.RoundTrip(req)
	}
	if req.URL.Host == directRuntimeProvisionerHost && t.provisioner != nil {
		return t.provisioner.RoundTrip(req)
	}
	return t.upstream.RoundTrip(req)
}

type directRuntimeIdentity struct {
	instanceID      string
	provisionerPort int
}

func runDirectRuntimeClient(ctx context.Context, transport http.RoundTripper, cfg *sessionsettings.ParentRuntimeConfig, identity directRuntimeIdentity) {
	if cfg == nil || !cfg.Enabled || cfg.Endpoint == "" || cfg.SessionID == "" || cfg.Token == "" || cfg.Generation <= 0 {
		return
	}
	w := newDirectRuntimeWorker(transport, cfg, identity.instanceID, identity.provisionerPort)
	go w.reportStatus(ctx)
	w.run(ctx)
}

func newDirectRuntimeWorker(transport http.RoundTripper, cfg *sessionsettings.ParentRuntimeConfig, instanceID string, provisionerPort int) *directRuntimeWorker {
	if transport == nil {
		transport = http.DefaultTransport
	}
	if provisionerPort <= 0 {
		provisionerPort = 9001
	}
	agentSocket := localAgentSocketPath()
	agentTransport := &http.Transport{DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, "unix", agentSocket)
	}}
	provisionerURL := fmt.Sprintf("http://127.0.0.1:%d", provisionerPort)
	var provisionerTransport http.RoundTripper
	if provisionerSocket := strings.TrimSpace(os.Getenv("PROVISIONER_LOCAL_SOCKET")); provisionerSocket != "" {
		provisionerURL = "http://" + directRuntimeProvisionerHost
		provisionerTransport = &http.Transport{DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
			return (&net.Dialer{}).DialContext(ctx, "unix", provisionerSocket)
		}}
	}
	return &directRuntimeWorker{
		cfg:            cfg,
		client:         &http.Client{Transport: &directRuntimeTransport{upstream: transport, agent: agentTransport, provisioner: provisionerTransport}},
		agentURL:       "http://" + directRuntimeAgentHost,
		provisionerURL: provisionerURL,
		instanceID:     instanceID,
	}
}

func (w *directRuntimeWorker) run(ctx context.Context) {
	cursorPath := filepath.Join(os.TempDir(), "agentapi-direct-runtime-cursor")
	cursor := loadDirectRuntimeCursor(cursorPath)
	backoff := time.Second
	for ctx.Err() == nil {
		requests, next, err := w.poll(ctx, cursor)
		if err != nil {
			if errors.Is(err, errDirectRuntimeFenced) {
				log.Printf("[DIRECT_RUNTIME] stopping runtime worker: %v", err)
				return
			}
			// A rolling parent may temporarily reject valid runtime credentials.
			log.Printf("[DIRECT_RUNTIME] request poll failed: %v", err)
			w.sleepWithJitter(ctx, backoff)
			backoff = minDuration(backoff*2, 30*time.Second)
			continue
		}
		backoff = time.Second
		for _, request := range requests {
			w.dispatch(ctx, request)
		}
		if next != "" {
			cursor = next
			saveDirectRuntimeCursor(cursorPath, cursor)
		}
	}
}

func loadDirectRuntimeCursor(cursorPath string) string {
	data, readErr := os.ReadFile(cursorPath)
	if readErr != nil && !errors.Is(readErr, os.ErrNotExist) {
		log.Printf("[DIRECT_RUNTIME] read cursor failed: %v", readErr)
	}
	cursor := strings.TrimSpace(string(data))
	if cursor == "" {
		cursor = "0-0"
	}
	return cursor
}

func saveDirectRuntimeCursor(cursorPath, cursor string) {
	if err := persistDirectRuntimeCursor(cursorPath, cursor); err != nil {
		log.Printf("[DIRECT_RUNTIME] persist cursor failed: %v", err)
	}
}

func (w *directRuntimeWorker) poll(ctx context.Context, cursor string) ([]core.Command, string, error) {
	pollCtx, cancel := context.WithTimeout(ctx, 35*time.Second)
	defer cancel()
	u, err := url.Parse(strings.TrimRight(w.cfg.Endpoint, "/") + "/internal/session-runtime/" + url.PathEscape(w.cfg.SessionID) + "/requests")
	if err != nil {
		return nil, cursor, err
	}
	q := u.Query()
	q.Set("after", cursor)
	q.Set("wait", "30s")
	q.Set("count", "100")
	q.Set("generation", fmt.Sprintf("%d", w.cfg.Generation))
	q.Set("instance_id", w.instanceID)
	u.RawQuery = q.Encode()
	req, err := http.NewRequestWithContext(pollCtx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, cursor, err
	}
	w.authorize(req)
	resp, err := w.client.Do(req)
	if err != nil {
		return nil, cursor, err
	}
	defer closeDirectRuntimeResponse(resp.Body)
	if resp.StatusCode == http.StatusNoContent {
		return nil, cursor, nil
	}
	if resp.StatusCode == http.StatusConflict {
		return nil, cursor, errDirectRuntimeFenced
	}
	if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
		return nil, cursor, errDirectRuntimeUnauthorized
	}
	if resp.StatusCode != http.StatusOK {
		return nil, cursor, fmt.Errorf("request poll returned HTTP %d", resp.StatusCode)
	}
	var result struct {
		Requests   []core.Command `json:"requests"`
		NextCursor string         `json:"next_cursor"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, cursor, err
	}
	return result.Requests, result.NextCursor, nil
}

func (w *directRuntimeWorker) dispatch(ctx context.Context, request core.Command) {
	if request.CancelRequestID != "" {
		if value, ok := w.active.Load(request.CancelRequestID); ok {
			value.(context.CancelFunc)()
		}
		w.postFramesBestEffort(ctx, []core.ResponseFrame{{ID: uuid.NewString(), RequestID: request.ID, CommandStreamID: request.StreamID, Status: http.StatusNoContent, Done: true, CreatedAt: time.Now().UTC()}})
		return
	}
	// A slow agent endpoint (for example a message request waiting on an
	// upstream model) must not block status, cancellation, or other HTTP
	// traffic for the session.
	go w.execute(ctx, request)
}
