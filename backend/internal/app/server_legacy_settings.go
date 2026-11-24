package app

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"strings"

	"github.com/takutakahashi/agentapi-proxy/pkg/config"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

func backfillServerLegacySettings(cfg *config.Config, persistenceClient kubernetes.Interface, namespace string) {
	if backend := configuredKVBackend(cfg.KVStore); backend == "libsql" || backend == "libsql-encrypted" {
		destinationName := cfg.KubernetesSession.SettingsBaseSecret
		if destinationName == "" {
			destinationName = "agentapi-settings-base"
		}
		var changed bool
		var backfillErr error
		if sourceFile := strings.TrimSpace(cfg.KubernetesSession.MCPServersBaseFile); sourceFile != "" {
			legacyData, readErr := os.ReadFile(sourceFile)
			switch {
			case readErr == nil:
				changed, backfillErr = backfillLegacyMCPServersBaseData(
					context.Background(), persistenceClient, namespace,
					cfg.KubernetesSession.MCPServersBaseSecret, destinationName, legacyData,
				)
			case !errors.Is(readErr, os.ErrNotExist):
				backfillErr = fmt.Errorf("read mounted legacy MCP base settings: %w", readErr)
			default:
				changed, backfillErr = backfillLegacyMCPServersBase(
					context.Background(), persistenceClient, namespace,
					cfg.KubernetesSession.MCPServersBaseSecret, destinationName,
				)
			}
		} else {
			changed, backfillErr = backfillLegacyMCPServersBase(
				context.Background(), persistenceClient, namespace,
				cfg.KubernetesSession.MCPServersBaseSecret, destinationName,
			)
		}
		if backfillErr != nil {
			log.Fatalf("[SERVER] Failed to backfill legacy MCP base settings: %v", backfillErr)
		}
		// Helm historically rendered an empty settings base name to disable the
		// unified base. When migration material is present, enable the canonical
		// destination so this and later starts use the normal settings path.
		if cfg.KubernetesSession.SettingsBaseSecret == "" {
			if _, getErr := persistenceClient.CoreV1().Secrets(namespace).Get(context.Background(), destinationName, metav1.GetOptions{}); getErr == nil {
				cfg.KubernetesSession.SettingsBaseSecret = destinationName
			} else if !apierrors.IsNotFound(getErr) {
				log.Fatalf("[SERVER] Failed to verify backfilled MCP base settings: %v", getErr)
			}
		}
		if changed {
			log.Printf("[SERVER] Backfilled legacy MCP base settings into %s", destinationName)
		}
	}

}
