package provisioner

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/takutakahashi/agentapi-proxy/pkg/sessionsettings"
	closeutils "github.com/takutakahashi/agentapi-proxy/pkg/utils"
)

func (s *Server) runFilesSync(ctx context.Context, settings *sessionsettings.SessionSettings) {
	userID, unsyncedFilePaths := settings.Session.UserID, settings.UnsyncedFilePaths

	const syncInterval = 10 * time.Second

	if userID == "" {
		log.Printf("[FILES_SYNC] No user ID set, skipping files sync")
		return
	}

	// Pool runners use PROVISIONER_PROXY_URL to claim work from the parent API.
	// Session-scoped controls are always served by the local Session Manager.
	proxyURL := sessionControlProxyURL()
	sessionID := sessionControlSessionID()
	token := os.Getenv("SESSION_CONTROL_TOKEN")
	if proxyURL == "" || sessionID == "" || token == "" {
		log.Printf("[FILES_SYNC] Session control URL, session ID, or token missing; skipping")
		return
	}

	watchedPaths := syncedManagedFilePaths(unsyncedFilePaths)
	if len(watchedPaths) == 0 {
		log.Printf("[FILES_SYNC] No managed file paths left after exclusions %v, skipping files sync", unsyncedFilePaths)
		return
	}

	log.Printf("[FILES_SYNC] Starting: watching %v -> session control API (interval: %s)", watchedPaths, syncInterval)
	// The local manager is cluster-internal control traffic and must not pass
	// through the session's sandbox HTTP proxy, which may reject or strip the
	// session-scoped Authorization header.
	client := &http.Client{Timeout: 15 * time.Second, Transport: &http.Transport{Proxy: nil}}

	// Track last hash per file path to detect changes.
	lastHashes := make(map[string]string, len(watchedPaths))
	for _, file := range settings.Files {
		sum := sha256.Sum256([]byte(file.Content))
		lastHashes[file.Path] = fmt.Sprintf("%x", sum)
	}

	ticker := time.NewTicker(syncInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			log.Printf("[FILES_SYNC] Context cancelled, stopping")
			return
		case <-ticker.C:
			// Read each file exactly once to avoid TOCTOU races between the
			// hash-check pass and the content-collection pass.
			type fileSnapshot struct {
				data []byte
				hash string
			}
			snapshots := make(map[string]fileSnapshot, len(watchedPaths))
			for _, path := range watchedPaths {
				data, hash, err := readFileWithHash(path)
				if err != nil {
					// File may not exist yet; not an error.
					continue
				}
				snapshots[path] = fileSnapshot{data: data, hash: hash}
			}

			changed := false
			for _, path := range watchedPaths {
				snap, ok := snapshots[path]
				if !ok {
					continue
				}
				if snap.hash != lastHashes[path] {
					changed = true
				}
			}
			if !changed {
				continue
			}

			// Build the full snapshot of all managed files from the single read above.
			files := make([]sessionsettings.ManagedFile, 0, len(snapshots))
			for _, path := range watchedPaths {
				snap, ok := snapshots[path]
				if !ok {
					continue
				}
				files = append(files, sessionsettings.ManagedFile{
					Path:    path,
					Content: string(snap.data),
				})
			}

			log.Printf("[FILES_SYNC] Files changed, syncing %d file(s) through control API", len(files))
			if err := saveManagedFilesVersioned(ctx, client, proxyURL, sessionID, token, files, settings.CredentialSyncID, lastHashes); err != nil {
				log.Printf("[FILES_SYNC] ERROR: control API sync failed: %v", err)
				// Do not update lastHashes so the next tick retries the sync.
			} else {
				log.Printf("[FILES_SYNC] Successfully synced through control API")
				// Update hashes only after a successful sync so failed upserts are retried.
				for _, path := range watchedPaths {
					if snap, ok := snapshots[path]; ok {
						lastHashes[path] = snap.hash
					}
				}
			}
		}
	}
}

func sessionControlProxyURL() string {
	proxyURL := strings.TrimRight(os.Getenv("PROVISIONER_LOCAL_PROXY_URL"), "/")
	if proxyURL == "" {
		proxyURL = strings.TrimRight(os.Getenv("PROVISIONER_PROXY_URL"), "/")
	}
	return proxyURL
}

func sessionControlSessionID() string {
	if allocatedID := os.Getenv("AGENTAPI_ALLOCATED_SESSION_ID"); allocatedID != "" {
		return allocatedID
	}
	return os.Getenv("AGENTAPI_SESSION_ID")
}

func syncedManagedFilePaths(unsyncedFilePaths []string) []string {
	excluded := make(map[string]struct{}, len(unsyncedFilePaths))
	for _, path := range unsyncedFilePaths {
		path = strings.TrimSpace(path)
		if path == "" {
			continue
		}
		excluded[path] = struct{}{}
	}

	paths := make([]string, 0, len(managedFilePaths))
	for _, path := range managedFilePaths {
		if _, ok := excluded[path]; ok {
			continue
		}
		paths = append(paths, path)
	}
	return paths
}

// readFileWithHash reads a file and returns its content and hex-encoded SHA256 hash.
func readFileWithHash(path string) ([]byte, string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, "", err
	}
	sum := sha256.Sum256(data)
	return data, fmt.Sprintf("%x", sum), nil
}

func saveManagedFiles(ctx context.Context, client *http.Client, proxyURL, sessionID, token string, files []sessionsettings.ManagedFile, syncIDs ...string) error {
	syncID := ""
	if len(syncIDs) > 0 {
		syncID = syncIDs[0]
	}
	return saveManagedFilesVersioned(ctx, client, proxyURL, sessionID, token, files, syncID, nil)
}
func saveManagedFilesVersioned(ctx context.Context, client *http.Client, proxyURL, sessionID, token string, files []sessionsettings.ManagedFile, syncID string, expected map[string]string) error {
	body, err := json.Marshal(map[string]any{"files": files, "expected": expected})
	if err != nil {
		return err
	}
	endpoint := proxyURL + "/internal/session-control/" + url.PathEscape(sessionID) + "/managed-files"
	req, err := http.NewRequestWithContext(ctx, http.MethodPut, endpoint, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("X-Session-Control-Token", token)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Credential-Sync-ID", syncID)
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer func() { closeutils.SafeCloseResponseBody(resp.Body) }()
	if resp.StatusCode != http.StatusNoContent {
		payload, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return fmt.Errorf("control API returned %d: %s", resp.StatusCode, strings.TrimSpace(string(payload)))
	}
	return nil
}

// runOtelcol starts the OpenTelemetry Collector binary as a subprocess.
// It is used when otelcol runs in in-process mode (OtelCollectorInProcess=true)
// instead of as a Kubernetes sidecar. This allows otelcol to be started
// after user context is established, so metrics labels (user_id, session_id,
// etc.) are correctly set even when using the stock inventory feature.
// The subprocess is tied to ctx: when ctx is cancelled the goroutine exits.
func (s *Server) runOtelcol(ctx context.Context, cfg *sessionsettings.OtelCollectorConfig) {
	const otelcolBin = "/usr/local/bin/otelcol"
	const configPath = "/tmp/otelcol-config.yaml"

	scrapeInterval := cfg.ScrapeInterval
	if scrapeInterval == "" {
		scrapeInterval = "15s"
	}
	claudeCodePort := cfg.ClaudeCodePort
	if claudeCodePort == 0 {
		claudeCodePort = 9464
	}
	exporterPort := cfg.ExporterPort
	if exporterPort == 0 {
		exporterPort = 9090
	}

	// Generate otelcol config with label values already substituted.
	// Using Go fmt.Sprintf rather than otelcol ${env:VAR} expansion so that
	// the correct values are used regardless of the container's own env vars
	// (which may be unset in stock sessions at startup time).
	otelConfig := fmt.Sprintf(`receivers:
  prometheus:
    config:
      scrape_configs:
        - job_name: 'claude-code'
          scrape_interval: %s
          static_configs:
            - targets: ['localhost:%d']

processors:
  resource:
    attributes:
      - key: user_id
        action: delete
      - key: session_id
        action: delete
  transform:
    error_mode: ignore
    metric_statements:
      - context: datapoint
        statements:
          # Rename claude-code's native labels
          - set(attributes["claude_user_id"], attributes["user_id"]) where attributes["user_id"] != nil
          - set(attributes["claude_session_id"], attributes["session_id"]) where attributes["session_id"] != nil
          - delete_key(attributes, "user_id")
          - delete_key(attributes, "session_id")
          # Remove user_email label to prevent it from being scraped by Prometheus
          - delete_key(attributes, "user_email")
          # Add agentapi labels
          - set(attributes["agentapi_session_id"], "%s")
          - set(attributes["agentapi_user_id"], "%s")
          - set(attributes["agentapi_team_id"], "%s")
          - set(attributes["agentapi_schedule_id"], "%s")
          - set(attributes["agentapi_webhook_id"], "%s")
          - set(attributes["agentapi_agent_type"], "%s")

exporters:
  prometheus:
    endpoint: "0.0.0.0:%d"
    resource_to_telemetry_conversion:
      enabled: false

service:
  pipelines:
    metrics:
      receivers: [prometheus]
      processors: [resource, transform]
      exporters: [prometheus]`,
		scrapeInterval, claudeCodePort,
		cfg.SessionID, cfg.UserID, cfg.TeamID,
		cfg.ScheduleID, cfg.WebhookID, cfg.AgentType,
		exporterPort)

	if err := os.WriteFile(configPath, []byte(otelConfig), 0o600); err != nil {
		log.Printf("[OTELCOL] Failed to write config to %s: %v", configPath, err)
		return
	}
	log.Printf("[OTELCOL] Config written to %s, starting otelcol subprocess", configPath)

	cmd := exec.CommandContext(ctx, otelcolBin, "--config="+configPath)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Run(); err != nil {
		if ctx.Err() != nil {
			log.Printf("[OTELCOL] Exited due to context cancellation")
		} else {
			log.Printf("[OTELCOL] Exited with error: %v", err)
		}
	} else {
		log.Printf("[OTELCOL] Exited normally")
	}
}

// buildAgentCommand returns the executable and arguments for the agent
// process, mirroring the logic in BuildRemoteProvisionSettings().
