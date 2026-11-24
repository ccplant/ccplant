package provisioner

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"strings"
	"time"
)

func (w *directRuntimeWorker) reportStatus(ctx context.Context) {
	var previous string
	var lastPostedAt time.Time
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		status := w.localStatus(ctx)
		// Refresh unchanged status periodically. Manager heartbeats and proxied
		// agent /status reads can race with the richer hc-aware runtime status;
		// a refresh heals those coarser writes without waiting for another turn.
		if status != "" && (status != previous || time.Since(lastPostedAt) >= directRuntimeStatusRefreshInterval) {
			if err := w.postStatus(ctx, status); err != nil {
				log.Printf("[DIRECT_RUNTIME] status push failed: %v", err)
			} else {
				previous = status
				lastPostedAt = time.Now()
			}
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func (w *directRuntimeWorker) localStatus(ctx context.Context) string {
	provisionerStatus := w.readLocalStatus(ctx, w.provisionerURL+"/status", true)
	if provisionerStatus != string(StatusReady) {
		return provisionerStatus
	}

	// Once provisioning has completed, the provisioner remains "ready" for the
	// lifetime of the process. The user-visible session status must instead come
	// from agentapi, which changes between "running" and "stable" for each turn.
	// If agentapi is briefly unavailable, do not replace the last known runtime
	// status with the coarser provisioner state; the next one-second poll retries.
	status := w.readLocalStatus(ctx, w.agentURL+"/status", false)
	if status == "stable" && w.hasRunningHerdrTerminal(ctx) {
		return "running"
	}
	return status
}

// hasRunningHerdrTerminal keeps the public session busy while work delegated
// through hc is still in progress. terminal.list with all=false only returns
// live terminals. Plain programs do not publish an agent status, while coding
// agents explicitly report idle/done once their turn has finished.
func (w *directRuntimeWorker) hasRunningHerdrTerminal(ctx context.Context) bool {
	body := bytes.NewBufferString(`{"jsonrpc":"2.0","id":1,"method":"terminal.list","params":{"all":false}}`)
	reqCtx, cancel := context.WithTimeout(ctx, 900*time.Millisecond)
	defer cancel()
	req, err := http.NewRequestWithContext(reqCtx, http.MethodPost, w.provisionerURL+"/herdr/rpc", body)
	if err != nil {
		return false
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := w.client.Do(req)
	if err != nil {
		return false
	}
	defer closeDirectRuntimeResponse(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return false
	}
	var result struct {
		Result struct {
			Terminals []struct {
				State  string            `json:"state"`
				Labels map[string]string `json:"labels"`
				Status *struct {
					State string `json:"state"`
				} `json:"status"`
			} `json:"terminals"`
		} `json:"result"`
	}
	if json.NewDecoder(resp.Body).Decode(&result) != nil {
		return false
	}
	for _, terminal := range result.Result.Terminals {
		if terminal.Labels["ccplant.system"] == "notification-bridge" {
			continue
		}
		if terminal.State != "" && terminal.State != "live" {
			continue
		}
		if terminal.Status == nil || terminal.Status.State == "working" || terminal.Status.State == "blocked" {
			return true
		}
	}
	return false
}

func (w *directRuntimeWorker) readLocalStatus(ctx context.Context, endpoint string, allowStructuredError bool) string {
	reqCtx, cancel := context.WithTimeout(ctx, 900*time.Millisecond)
	defer cancel()
	req, err := http.NewRequestWithContext(reqCtx, http.MethodGet, endpoint, nil)
	if err != nil {
		return ""
	}
	resp, err := w.client.Do(req)
	if err != nil {
		return ""
	}
	defer closeDirectRuntimeResponse(resp.Body)
	// The provisioner intentionally returns HTTP 500 together with a structured
	// StatusError response. Preserve that terminal state so the parent session
	// does not remain "starting" (or become "active" from pod readiness alone).
	if resp.StatusCode/100 != 2 && (!allowStructuredError || resp.StatusCode != http.StatusInternalServerError) {
		return ""
	}
	var result struct {
		Status string `json:"status"`
	}
	if json.NewDecoder(resp.Body).Decode(&result) != nil {
		return ""
	}
	if resp.StatusCode == http.StatusInternalServerError && result.Status != string(StatusError) {
		return ""
	}
	return result.Status
}

func (w *directRuntimeWorker) postStatus(ctx context.Context, status string) error {
	raw, err := json.Marshal(map[string]string{"status": status})
	if err != nil {
		return fmt.Errorf("encode runtime status: %w", err)
	}
	u := strings.TrimRight(w.cfg.Endpoint, "/") + "/internal/session-runtime/" + url.PathEscape(w.cfg.SessionID) + "/status?generation=" + fmt.Sprintf("%d", w.cfg.Generation)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, u, bytes.NewReader(raw))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	w.authorize(req)
	resp, err := w.client.Do(req)
	if err != nil {
		return err
	}
	defer closeDirectRuntimeResponse(resp.Body)
	if resp.StatusCode/100 != 2 {
		return fmt.Errorf("status push returned HTTP %d", resp.StatusCode)
	}
	return nil
}
