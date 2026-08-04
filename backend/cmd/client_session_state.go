package cmd

import (
	"bytes"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"github.com/takutakahashi/agentapi-proxy/pkg/sessionstate"
)

var backupSessionStateCmd = &cobra.Command{Use: "backup-session-state", Short: "Back up ACP state through the proxy", Args: cobra.NoArgs, RunE: runBackupSessionState}

func runBackupSessionState(_ *cobra.Command, _ []string) error {
	proxy := strings.TrimRight(os.Getenv("PROVISIONER_PROXY_URL"), "/")
	token := os.Getenv("PROVISIONER_TOKEN")
	id := os.Getenv("AGENTAPI_SESSION_ID")
	agentType := os.Getenv("AGENTAPI_AGENT_TYPE")
	if proxy == "" || token == "" || id == "" {
		return fmt.Errorf("PROVISIONER_PROXY_URL, PROVISIONER_TOKEN and AGENTAPI_SESSION_ID are required")
	}
	cwd, err := os.Getwd()
	if err != nil {
		return err
	}
	home := os.Getenv("HOME")
	if home == "" {
		home = "/home/agentapi"
	}
	var body bytes.Buffer
	if err := sessionstate.Pack(&body, agentType, readACPSessionID(cwd), home, cwd); err != nil {
		return err
	}
	req, err := http.NewRequest(http.MethodPut, proxy+"/internal/session-state/"+id, &body)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/gzip")
	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusNoContent {
		if resp.StatusCode == http.StatusServiceUnavailable {
			_, _ = fmt.Fprintln(os.Stderr, "session state backup skipped: persistence backend is unavailable")
			return nil
		}
		return fmt.Errorf("session state backup failed: HTTP %d", resp.StatusCode)
	}
	return nil
}

func readACPSessionID(cwd string) string {
	data, _ := os.ReadFile(filepath.Join(cwd, ".acp-session-id"))
	return strings.TrimSpace(string(data))
}
