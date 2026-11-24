package services

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/takutakahashi/agentapi-proxy/pkg/sessionsettings"
	closeutils "github.com/takutakahashi/agentapi-proxy/pkg/utils"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
)

func (m *KubernetesSessionManager) waitRestartReady(ctx context.Context, ks *KubernetesSession, id string) error {
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, fmt.Sprintf("http://%s:%d/status", ks.ServiceDNS(), ProvisionerPort), nil)
		if err != nil {
			return err
		}
		resp, err := m.restartClient().Do(req)
		if err == nil {
			var status struct {
				Status    string
				RestartID string `json:"restart_id"`
			}
			decodeErr := json.NewDecoder(io.LimitReader(resp.Body, 4096)).Decode(&status)
			closeutils.SafeCloseResponseBody(resp.Body)
			if decodeErr == nil && status.RestartID == id {
				if status.Status == "ready" {
					return nil
				}
				if status.Status == "error" {
					return fmt.Errorf("agent could not restore the conversation")
				}
			}
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
	}
}
func (m *KubernetesSessionManager) finishRestart(ctx context.Context, ks *KubernetesSession) error {
	patch := []byte(fmt.Sprintf(`{"metadata":{"annotations":{"%s":null,"%s":"ready"}}}`, restartHoldAnnotation, restartPhaseAnnotation))
	_, err := m.client.CoreV1().Services(m.namespace).Patch(ctx, ks.ServiceName(), types.MergePatchType, patch, metav1.PatchOptions{})
	return err
}

func (m *KubernetesSessionManager) restartClient() *http.Client {
	if m.restartHTTPClient != nil {
		return m.restartHTTPClient
	}
	return http.DefaultClient
}

// Validate the parent's running snapshot without mutating or stopping a pooled
// runner. PauseSession's handler persists this snapshot only after validation.
func (m *KubernetesSessionManager) ValidateSessionRestartWithCurrent(ctx context.Context, id string, current, next *sessionsettings.SessionSettings) error {
	if current == nil || next == nil || current.Session.ID == "" || current.Session.ID != next.Session.ID {
		return fmt.Errorf("session identity mismatch")
	}
	ks, ok := m.GetSession(id).(*KubernetesSession)
	if !ok || ks == nil {
		return fmt.Errorf("session settings unavailable")
	}
	if m.config.SessionPersistence.Backend == "" {
		return fmt.Errorf("conversation checkpoint storage is required for restart")
	}
	if saved := ks.ProvisionSettings(); saved != nil && saved.Session.UserID != "" && saved.Session.ID != current.Session.ID {
		return fmt.Errorf("session identity mismatch")
	}
	return sessionsettings.ValidateRestart(current, next)
}
