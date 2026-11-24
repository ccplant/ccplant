package cmd

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"

	corev1 "k8s.io/api/core/v1"
)

func isApplicationKVSecret(secret *corev1.Secret) bool {
	if hasAgentAPILabel(secret.Labels) {
		return true
	}
	// Provider connection and identity data predates the agentapi.proxy label
	// namespace. Keep these persistent records in the migration allowlist
	// without also copying short-lived OAuth states or broker leases.
	for _, label := range []string{
		"agentapi.ccplant.io/github-connection",
		"agentapi.ccplant.io/github-identity",
		"agentapi.ccplant.io/github-principal",
		"agentapi.ccplant.io/google-connection",
		"agentapi.ccplant.io/google-identity",
	} {
		if secret.Labels[label] == "true" {
			return true
		}
	}
	// The pre-v2 schedule store was a fixed-name Secret without labels.
	return secret.Name == "agentapi-schedules"
}

func hasAgentAPILabel(labels map[string]string) bool {
	for key := range labels {
		if strings.HasPrefix(key, "agentapi.proxy/") {
			return true
		}
	}
	return false
}

func isApplicationKVConfigMap(configMap *corev1.ConfigMap) bool {
	switch configMap.Name {
	case "agentapi-session-shares", "agentapi-user-team-mapping", "agentapi-slack-channel-cache":
		// Include fixed-name stores created before their identifying labels were
		// introduced or repaired by a subsequent write.
		return true
	}
	return hasAgentAPILabel(configMap.Labels)
}

func writeKVStoreMigrationResult(w io.Writer, result kvStoreMigrationResult, output string) error {
	if output == "json" {
		encoder := json.NewEncoder(w)
		encoder.SetIndent("", "  ")
		return encoder.Encode(result)
	}
	mode := "migration"
	if result.DryRun {
		mode = "dry run"
	}
	for _, entry := range result.Entries {
		if _, err := fmt.Fprintf(w, "%-12s %s/%s\n", entry.Status, entry.Kind, entry.Key); err != nil {
			return err
		}
	}
	_, err := fmt.Fprintf(w, "%s: selected=%d copied=%d updated=%d skipped=%d conflicts=%d\n",
		mode, result.Selected, result.Copied, result.Updated, result.Skipped, result.Conflicts)
	return err
}
