package provisioner

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/takutakahashi/agentapi-proxy/pkg/sessionsettings"
	"github.com/takutakahashi/agentapi-proxy/pkg/sessionstate"
	closeutils "github.com/takutakahashi/agentapi-proxy/pkg/utils"
)

func (s *Server) restoreSessionState(ctx context.Context, sourceID, cwd string, keepWorkspace bool) (bool, error) {
	unpack := sessionstate.Unpack
	if keepWorkspace {
		unpack = sessionstate.UnpackMissing
	}
	if volumePath := strings.TrimSpace(os.Getenv("AGENTAPI_SESSION_STATE_VOLUME_PATH")); volumePath != "" {
		if _, err := sessionstate.TryRemoveStaleVolumeTemps(filepath.Dir(volumePath)); err != nil {
			log.Printf("[PROVISIONER] Failed to remove stale session state temporary archives: %v", err)
		}
		archive, err := os.Open(volumePath)
		if os.IsNotExist(err) {
			// A template starts with a fresh PVC, so its portable snapshot must
			// be fetched from the execution plane's template store. Ordinary
			// volume-backed resumes remain entirely local.
			if !strings.HasPrefix(sourceID, "tpl_") {
				return false, nil
			}
		} else if err != nil {
			return false, err
		} else {
			defer func() { _ = archive.Close() }()
			if err := unpack(archive, runtimeHome, cwd); err != nil {
				return false, err
			}
			return true, nil
		}
	}
	proxy := strings.TrimRight(os.Getenv("SESSION_STATE_PROXY_URL"), "/")
	if proxy == "" {
		proxy = strings.TrimRight(os.Getenv("PROVISIONER_PROXY_URL"), "/")
	}
	token := os.Getenv("PROVISIONER_TOKEN")
	if proxy == "" || token == "" {
		return false, fmt.Errorf("provisioner proxy credentials are missing")
	}
	restoreCtx, cancel := context.WithTimeout(ctx, 10*time.Minute)
	defer cancel()
	directReq, directErr := http.NewRequestWithContext(restoreCtx, http.MethodGet, proxy+"/internal/session-state/"+sourceID+"/download-url", nil)
	if directErr == nil {
		directReq.Header.Set("Authorization", "Bearer "+token)
		directResp, err := s.httpClient.Do(directReq)
		if err != nil {
			return false, errSessionStateBackendUnavailable
		}
		if directResp.StatusCode == http.StatusOK {
			var signed struct {
				URL string `json:"url"`
			}
			decodeErr := json.NewDecoder(io.LimitReader(directResp.Body, 64<<10)).Decode(&signed)
			closeutils.SafeCloseResponseBody(directResp.Body)
			if decodeErr != nil || signed.URL == "" {
				return false, fmt.Errorf("invalid direct restore response")
			}
			objectReq, reqErr := http.NewRequestWithContext(restoreCtx, http.MethodGet, signed.URL, nil)
			if reqErr != nil {
				return false, reqErr
			}
			objectResp, reqErr := sessionStateObjectClient(signed.URL).Do(objectReq)
			if reqErr != nil {
				return false, errSessionStateBackendUnavailable
			}
			defer func() { closeutils.SafeCloseResponseBody(objectResp.Body) }()
			if objectResp.StatusCode == http.StatusNotFound {
				return false, nil
			}
			if objectResp.StatusCode != http.StatusOK {
				return false, errSessionStateBackendUnavailable
			}
			if err := unpack(objectResp.Body, runtimeHome, cwd); err != nil {
				return false, err
			}
			return true, nil
		}
		closeutils.SafeCloseResponseBody(directResp.Body)
		if directResp.StatusCode == http.StatusNotFound {
			return false, nil
		}
		if directResp.StatusCode == http.StatusServiceUnavailable {
			return false, errSessionStateBackendUnavailable
		}
		if directResp.StatusCode != http.StatusNotImplemented {
			return false, fmt.Errorf("direct restore endpoint returned HTTP %d", directResp.StatusCode)
		}
	}
	req, err := http.NewRequestWithContext(restoreCtx, http.MethodGet, proxy+"/internal/session-state/"+sourceID, nil)
	if err != nil {
		return false, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := s.httpClient.Do(req)
	if err != nil {
		return false, err
	}
	defer func() { closeutils.SafeCloseResponseBody(resp.Body) }()
	if resp.StatusCode == http.StatusNotFound {
		return false, nil
	}
	if resp.StatusCode == http.StatusServiceUnavailable {
		return false, errSessionStateBackendUnavailable
	}
	if resp.StatusCode != http.StatusOK {
		data, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return false, fmt.Errorf("backend returned HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(data)))
	}
	if err := unpack(resp.Body, runtimeHome, cwd); err != nil {
		return false, err
	}
	return true, nil
}

func sessionStateObjectClient(rawURL string) *http.Client {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return http.DefaultClient
	}
	hostname := parsed.Hostname()
	if strings.Contains(hostname, ".") && !strings.HasSuffix(hostname, ".svc") && !strings.HasSuffix(hostname, ".cluster.local") {
		return http.DefaultClient
	}
	transport, ok := http.DefaultTransport.(*http.Transport)
	if !ok {
		return http.DefaultClient
	}
	direct := transport.Clone()
	direct.Proxy = nil
	return &http.Client{Transport: direct}
}

func trustNativeWorkspace(configPath, workspace string) error {
	data, err := os.ReadFile(configPath)
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	config := make(map[string]interface{})
	if len(data) > 0 {
		if err := json.Unmarshal(data, &config); err != nil {
			return err
		}
	}
	projects, _ := config["projects"].(map[string]interface{})
	if projects == nil {
		projects = make(map[string]interface{})
	}
	project, _ := projects[workspace].(map[string]interface{})
	if project == nil {
		project = make(map[string]interface{})
	}
	project["hasTrustDialogAccepted"] = true
	project["hasCompletedProjectOnboarding"] = true
	projects[workspace] = project
	config["projects"] = projects
	out, err := json.MarshalIndent(config, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(configPath), 0o700); err != nil {
		return err
	}
	return os.WriteFile(configPath, out, 0o600)
}

// runDockerLogins runs in the background after DinD is enabled: waits for the docker daemon
// to be ready, then runs docker login for each registry with inline credentials.
// Secret-based registries are already mounted as /root/.docker/config.json in the DinD
// container, so they need no docker login call here.
func (s *Server) runDockerLogins(ctx context.Context, docker *sessionsettings.DockerConfig) {
	if docker == nil || !docker.Enabled {
		return
	}

	// Check if there are any inline credentials to log in with.
	hasInlineCreds := false
	for _, reg := range docker.Registries {
		if reg.Username != "" && reg.Password != "" {
			hasInlineCreds = true
			break
		}
	}
	if !hasInlineCreds {
		return
	}

	// Wait for the DinD daemon to be ready (up to 120 seconds).
	log.Printf("[PROVISIONER] Waiting for Docker daemon to be ready")
	deadline := time.Now().Add(120 * time.Second)
	for time.Now().Before(deadline) {
		cmd := exec.CommandContext(ctx, "docker", "info")
		if err := cmd.Run(); err == nil {
			break
		}
		select {
		case <-ctx.Done():
			log.Printf("[PROVISIONER] Context cancelled while waiting for Docker daemon")
			return
		case <-time.After(2 * time.Second):
		}
	}
	log.Printf("[PROVISIONER] Docker daemon is ready")

	// Run docker login for each registry with inline credentials.
	for _, reg := range docker.Registries {
		if reg.Username == "" || reg.Password == "" {
			continue
		}
		log.Printf("[PROVISIONER] Logging into registry %s as %s", reg.Server, reg.Username)
		args := []string{"login", "-u", reg.Username, "--password-stdin"}
		if reg.Server != "" {
			args = append(args, reg.Server)
		}
		cmd := exec.CommandContext(ctx, "docker", args...)
		cmd.Stdin = strings.NewReader(reg.Password)
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		if err := cmd.Run(); err != nil {
			log.Printf("[PROVISIONER] Warning: docker login for %s failed: %v", reg.Server, err)
		} else {
			log.Printf("[PROVISIONER] Successfully logged into registry %s", reg.Server)
		}
	}
}

// runPreScript executes the pre-script shell snippet before the agent process starts.
// It inherits the session environment and streams stdout/stderr to the provisioner log.
func (s *Server) runPreScript(ctx context.Context, script string, envMap map[string]string) error {
	cmd := exec.CommandContext(ctx, "sh", "-c", script)
	cmd.Env = sessionProcessEnvironment(os.Environ(), nil, envMap)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

// runAcpPosts starts the acp-posts binary as a subprocess, forwarding
// agent output (history.jsonl) to Slack. It waits for the ACP bridge history
// file to appear before launching.
//
// The subprocess is tied to ctx: when ctx is cancelled the goroutine exits.
func (s *Server) runAcpPosts(ctx context.Context, params *sessionsettings.SlackParams, agentType string) {
	const acpPostsBin = "/usr/local/bin/acp-posts"

	historyFile := acpHistoryPath

	log.Printf("[ACP_POSTS] Waiting for history file %s", historyFile)
	for {
		select {
		case <-ctx.Done():
			log.Printf("[ACP_POSTS] Context cancelled while waiting for history file")
			return
		case <-time.After(time.Second):
		}
		if _, err := os.Stat(historyFile); err == nil {
			break
		}
	}
	log.Printf("[ACP_POSTS] History file found, starting acp-posts")

	cmd := exec.CommandContext(ctx, acpPostsBin, "--file", historyFile)
	cmd.Env = sessionProcessEnvironment(os.Environ(), nil, map[string]string{
		"SLACK_BOT_TOKEN":  params.BotToken,
		"SLACK_CHANNEL_ID": params.Channel,
		"SLACK_THREAD_TS":  params.ThreadTS,
	})
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Run(); err != nil {
		// ctx cancellation causes an expected error; don't log it as fatal.
		if ctx.Err() != nil {
			log.Printf("[ACP_POSTS] Exited due to context cancellation")
		} else {
			log.Printf("[ACP_POSTS] Exited with error: %v", err)
		}
	} else {
		log.Printf("[ACP_POSTS] Exited normally")
	}
}

// managedFilePaths lists the absolute paths that runFilesSync watches and persists
// to the agentapi-agent-files-{userID} Secret.  Order determines the index used as
// the Secret key prefix (0, 1, …).
