package services

import (
	"encoding/json"
	closeutils "github.com/takutakahashi/agentapi-proxy/pkg/utils"
	"log"
	"net/http"
)

func herdrResponseHasRunningTerminal(client *http.Client, req *http.Request) (bool, bool) {
	resp, err := client.Do(req)
	if err != nil {
		return false, false
	}
	defer func() { closeutils.SafeCloseResponseBody(resp.Body) }()
	if resp.StatusCode != http.StatusOK {
		return false, false
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
		return false, false
	}
	for _, terminal := range result.Result.Terminals {
		if terminal.Labels["ccplant.system"] == "notification-bridge" {
			continue
		}
		if terminal.State != "" && terminal.State != "live" {
			continue
		}
		if terminal.Status == nil || terminal.Status.State == "working" || terminal.Status.State == "blocked" {
			return true, true
		}
	}
	return false, true
}

func applyAgentRuntimeStatus(session *KubernetesSession, status string) {
	switch status {
	case "running":
		session.SetStatus("running")
		log.Printf("[AGENT_STATUS] Session %s is now running", session.id)
	case "stable":
		session.SetStatus("active")
		log.Printf("[AGENT_STATUS] Session %s is now stable (active)", session.id)
	}
}
