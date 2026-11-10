package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

// backfillLegacyMCPServersBase converts the legacy mcp-servers-base document
// into the unified base settings document. Existing unified MCP entries win,
// making the migration safe to run on every startup and across replicas.
func backfillLegacyMCPServersBase(ctx context.Context, client kubernetes.Interface, namespace, sourceName, destinationName string) (bool, error) {
	if sourceName == "" || destinationName == "" {
		return false, nil
	}
	legacy, err := client.CoreV1().Secrets(namespace).Get(ctx, sourceName, metav1.GetOptions{})
	if apierrors.IsNotFound(err) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("read legacy MCP base secret %s: %w", sourceName, err)
	}
	legacyData, ok := legacy.Data["mcp-servers.json"]
	if !ok {
		return false, nil
	}
	return backfillLegacyMCPServersBaseData(ctx, client, namespace, sourceName, destinationName, legacyData)
}

func backfillLegacyMCPServersBaseData(ctx context.Context, client kubernetes.Interface, namespace, sourceName, destinationName string, legacyData []byte) (bool, error) {
	if destinationName == "" || len(legacyData) == 0 {
		return false, nil
	}
	var legacyDocument struct {
		MCPServers map[string]json.RawMessage `json:"mcpServers"`
	}
	if err := json.Unmarshal(legacyData, &legacyDocument); err != nil {
		return false, fmt.Errorf("decode %s/mcp-servers.json: %w", sourceName, err)
	}
	if len(legacyDocument.MCPServers) == 0 {
		return false, nil
	}

	for attempt := 0; attempt < 5; attempt++ {
		destination, getErr := client.CoreV1().Secrets(namespace).Get(ctx, destinationName, metav1.GetOptions{})
		if apierrors.IsNotFound(getErr) {
			data, marshalErr := marshalBackfilledBaseSettings(nil, legacyDocument.MCPServers)
			if marshalErr != nil {
				return false, marshalErr
			}
			_, createErr := client.CoreV1().Secrets(namespace).Create(ctx, &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      destinationName,
					Namespace: namespace,
					Labels:    map[string]string{"agentapi.proxy/settings": "true"},
				},
				Type: corev1.SecretTypeOpaque,
				Data: map[string][]byte{"settings.json": data},
			}, metav1.CreateOptions{})
			if apierrors.IsAlreadyExists(createErr) {
				continue
			}
			if createErr != nil {
				return false, fmt.Errorf("create base settings secret %s: %w", destinationName, createErr)
			}
			return true, nil
		}
		if getErr != nil {
			return false, fmt.Errorf("read base settings secret %s: %w", destinationName, getErr)
		}

		data, changed, mergeErr := mergeLegacyMCPIntoBaseSettings(destination.Data["settings.json"], legacyDocument.MCPServers)
		if mergeErr != nil {
			return false, fmt.Errorf("merge legacy MCP servers into %s/settings.json: %w", destinationName, mergeErr)
		}
		if !changed {
			return false, nil
		}
		if destination.Data == nil {
			destination.Data = make(map[string][]byte)
		}
		destination.Data["settings.json"] = data
		if destination.Labels == nil {
			destination.Labels = make(map[string]string)
		}
		destination.Labels["agentapi.proxy/settings"] = "true"
		if _, updateErr := client.CoreV1().Secrets(namespace).Update(ctx, destination, metav1.UpdateOptions{}); apierrors.IsConflict(updateErr) {
			continue
		} else if updateErr != nil {
			return false, fmt.Errorf("update base settings secret %s: %w", destinationName, updateErr)
		}
		return true, nil
	}
	return false, errors.New("backfill legacy MCP base settings exceeded conflict retry limit")
}

func marshalBackfilledBaseSettings(existing []byte, legacy map[string]json.RawMessage) ([]byte, error) {
	data, _, err := mergeLegacyMCPIntoBaseSettings(existing, legacy)
	return data, err
}

func mergeLegacyMCPIntoBaseSettings(existing []byte, legacy map[string]json.RawMessage) ([]byte, bool, error) {
	document := make(map[string]json.RawMessage)
	if len(existing) > 0 {
		if err := json.Unmarshal(existing, &document); err != nil {
			return nil, false, err
		}
	}
	servers := make(map[string]json.RawMessage)
	if raw, ok := document["mcp_servers"]; ok && string(raw) != "null" {
		if err := json.Unmarshal(raw, &servers); err != nil {
			return nil, false, fmt.Errorf("decode mcp_servers: %w", err)
		}
	}
	changed := false
	for name, server := range legacy {
		if _, exists := servers[name]; exists {
			continue
		}
		servers[name] = server
		changed = true
	}
	if !changed {
		return existing, false, nil
	}
	encodedServers, err := json.Marshal(servers)
	if err != nil {
		return nil, false, err
	}
	document["mcp_servers"] = encodedServers
	data, err := json.Marshal(document)
	return data, true, err
}
