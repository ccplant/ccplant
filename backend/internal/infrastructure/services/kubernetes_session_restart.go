package services

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"reflect"
	"strings"
	"time"

	"gopkg.in/yaml.v3"

	"github.com/takutakahashi/agentapi-proxy/pkg/sessionsettings"
	closeutils "github.com/takutakahashi/agentapi-proxy/pkg/utils"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
)

const restartHoldAnnotation = "agentapi.proxy/restart-hold"
const restartRequestAnnotation = "agentapi.proxy/restart-request"
const restartPhaseAnnotation = "agentapi.proxy/restart-phase"

type restartContextKey struct{}

// PauseSession persists a hold before checkpointing, so ordinary reads cannot wake it.
func (m *KubernetesSessionManager) PauseSession(ctx context.Context, id string) error {
	return m.pauseSession(ctx, id, nil)
}

func (m *KubernetesSessionManager) PauseSessionWithSettings(ctx context.Context, id string, current *sessionsettings.SessionSettings) error {
	return m.pauseSession(ctx, id, current)
}

func (m *KubernetesSessionManager) pauseSession(ctx context.Context, id string, current *sessionsettings.SessionSettings) error {
	ks, err := m.restartableSessionWith(id, current)
	if err != nil {
		return err
	}
	svc, err := m.client.CoreV1().Services(m.namespace).Get(ctx, ks.ServiceName(), metav1.GetOptions{})
	if err != nil {
		return err
	}
	if svc.Annotations[restartHoldAnnotation] == "true" && (svc.Annotations[restartPhaseAnnotation] == "process_paused" || svc.Annotations[restartPhaseAnnotation] == "paused") {
		return nil
	}
	if err := m.setRestartPhase(ctx, ks, "manual", "pausing"); err != nil {
		return err
	}
	if ks.Status() == "suspended" {
		return m.setRestartPhase(ctx, ks, "manual", "paused")
	}
	if err := m.provisionerLifecycle(ctx, ks, "pause", nil); err != nil {
		return err
	}
	if err := m.checkpointSessionState(ctx, id); err != nil {
		return err
	}
	raw, err := json.Marshal(ks.ProvisionSettings())
	if err != nil {
		return err
	}
	var paused sessionsettings.SessionSettings
	if err := json.Unmarshal(raw, &paused); err != nil {
		return err
	}
	paused.Paused = true
	paused.Restart = true
	paused.Session.ResumeFrom = paused.Session.ID
	if err := m.PrepareSessionResume(ctx, id, &paused); err != nil {
		return err
	}
	return m.setRestartPhase(ctx, ks, "manual", "process_paused")
}
func (m *KubernetesSessionManager) restartableSession(id string) (*KubernetesSession, error) {
	return m.restartableSessionWith(id, nil)
}

func (m *KubernetesSessionManager) restartableSessionWith(id string, current *sessionsettings.SessionSettings) (*KubernetesSession, error) {
	ks, ok := m.GetSession(id).(*KubernetesSession)
	if !ok || ks == nil {
		return nil, fmt.Errorf("session settings unavailable")
	}
	settings := m.normalizeProvisionSettings(current)
	if settings == nil {
		var err error
		settings, err = m.CurrentSessionSettings(context.Background(), id)
		if err != nil {
			return nil, err
		}
	}
	ks.SetProvisionSettings(settings)
	if err := sessionsettings.ValidateRestart(settings, settings); err != nil {
		return nil, err
	}
	if m.config.SessionPersistence.Backend == "" {
		return nil, fmt.Errorf("conversation checkpoint storage is required for restart")
	}
	return ks, nil
}
func (m *KubernetesSessionManager) setRestartPhase(ctx context.Context, ks *KubernetesSession, id, phase string) error {
	svc, err := m.client.CoreV1().Services(m.namespace).Get(ctx, ks.ServiceName(), metav1.GetOptions{})
	if err != nil {
		return err
	}
	if svc.Annotations == nil {
		svc.Annotations = map[string]string{}
	}
	svc.Annotations[restartHoldAnnotation] = "true"
	svc.Annotations[restartRequestAnnotation] = id
	svc.Annotations[restartPhaseAnnotation] = phase
	updated, err := m.client.CoreV1().Services(m.namespace).Update(ctx, svc, metav1.UpdateOptions{})
	if err == nil && updated == nil {
		return fmt.Errorf("update restart phase returned no service")
	}
	return err
}

// RestartSession restarts the agent in place, or replaces the workload when
// sandbox/Docker configuration changed. Both paths persist the complete settings.
func (m *KubernetesSessionManager) RestartSession(ctx context.Context, id, requestID string, next *sessionsettings.SessionSettings) error {
	ks, err := m.restartableSession(id)
	if err != nil {
		return err
	}
	if err := sessionsettings.ValidateRestart(ks.ProvisionSettings(), next); err != nil {
		return err
	}
	svc, err := m.client.CoreV1().Services(m.namespace).Get(ctx, ks.ServiceName(), metav1.GetOptions{})
	if err != nil {
		return err
	}
	phase := svc.Annotations[restartPhaseAnnotation]
	if svc.Annotations[restartRequestAnnotation] == requestID && phase == "ready" {
		return nil
	}
	if svc.Annotations[restartHoldAnnotation] == "true" && phase != "paused" && phase != "process_paused" && phase != "failed" && svc.Annotations[restartRequestAnnotation] != requestID {
		return fmt.Errorf("another restart is in progress")
	}
	previous := ks.ProvisionSettings()
	inPlace := phase == "process_paused" && reflect.DeepEqual(previous.Sandbox, next.Sandbox) && reflect.DeepEqual(previous.Docker, next.Docker)
	next.Paused = false
	next.RestartID = requestID
	if inPlace {
		if err := m.PrepareSessionResume(ctx, id, next); err != nil {
			return err
		}
		if err := m.setRestartPhase(ctx, ks, requestID, "resuming"); err != nil {
			return err
		}
		if err := m.provisionerLifecycle(ctx, ks, "restart", next); err != nil {
			m.markRestartFailed(ctx, ks, requestID)
			return err
		}
		if err := m.waitRestartReady(ctx, ks, requestID); err != nil {
			m.markRestartFailed(ctx, ks, requestID)
			return err
		}
		return m.finishRestart(ctx, ks)
	}
	if err := m.setRestartPhase(ctx, ks, requestID, "stopping"); err != nil {
		return err
	}
	success := false
	defer func() {
		if !success {
			m.markRestartFailed(ctx, ks, requestID)
		}
	}()
	if phase == "process_paused" {
		if err := m.suspendSessionWorkload(ctx, id, svc); err != nil {
			return err
		}
	} else if ks.Status() != "suspended" {
		if err := m.SuspendSession(ctx, id); err != nil {
			return err
		}
	}
	if err := m.waitRestartWorkloadStopped(ctx, ks); err != nil {
		return err
	}
	next.Restart = true
	next.Session.ResumeFrom = next.Session.ID
	next.InitialMessage = ""
	next.WebhookPayload = ""
	next.Startup.PreScript = ""
	if err := m.PrepareSessionResume(ctx, id, next); err != nil {
		return err
	}
	if err := m.setRestartPhase(ctx, ks, requestID, "resuming"); err != nil {
		return err
	}
	ctx = context.WithValue(ctx, restartContextKey{}, true)
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		_, waiting, err := m.EnsureSessionWorkload(ctx, id)
		if err != nil {
			return err
		}
		if !waiting {
			ready, err := m.restartedPodReady(ctx, ks)
			if err != nil {
				return err
			}
			if ready {
				break
			}
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
	}
	if err := m.waitRestartReady(ctx, ks, requestID); err != nil {
		return err
	}
	if err := m.finishRestart(ctx, ks); err != nil {
		return err
	}
	success = true
	return nil
}

func (m *KubernetesSessionManager) markRestartFailed(ctx context.Context, session *KubernetesSession, requestID string) {
	if err := m.setRestartPhase(context.WithoutCancel(ctx), session, requestID, "failed"); err != nil {
		log.Printf("[K8S_SESSION] failed to persist restart failure for %s: %v", session.ID(), err)
	}
}
func (m *KubernetesSessionManager) waitRestartWorkloadStopped(ctx context.Context, ks *KubernetesSession) error {
	svc, err := m.client.CoreV1().Services(m.namespace).Get(ctx, ks.ServiceName(), metav1.GetOptions{})
	if err != nil {
		return err
	}
	if len(svc.Spec.Selector) == 0 {
		return fmt.Errorf("session service has no workload selector")
	}
	selector := labels.SelectorFromSet(svc.Spec.Selector).String()
	ticker := time.NewTicker(250 * time.Millisecond)
	defer ticker.Stop()
	for {
		pods, err := m.client.CoreV1().Pods(m.namespace).List(ctx, metav1.ListOptions{LabelSelector: selector})
		if err != nil {
			return err
		}
		deploymentGone := true
		if m.isPVCEnabled() {
			_, e := m.client.AppsV1().Deployments(m.namespace).Get(ctx, strings.TrimSuffix(ks.ServiceName(), "-svc"), metav1.GetOptions{})
			deploymentGone = apierrors.IsNotFound(e)
			if e != nil && !deploymentGone {
				return e
			}
		}
		if len(pods.Items) == 0 && deploymentGone {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
	}
}

// Implemented on the concrete manager; callers feature-detect before stopping.
var _ interface {
	RestartSession(context.Context, string, string, *sessionsettings.SessionSettings) error
	PauseSession(context.Context, string) error
} = (*KubernetesSessionManager)(nil)

func (m *KubernetesSessionManager) ValidateSessionRestart(ctx context.Context, id string, next *sessionsettings.SessionSettings) error {
	ks, err := m.restartableSession(id)
	if err != nil {
		return err
	}
	return sessionsettings.ValidateRestart(ks.ProvisionSettings(), next)
}
func (m *KubernetesSessionManager) CurrentSessionSettings(ctx context.Context, id string) (*sessionsettings.SessionSettings, error) {
	ks, ok := m.GetSession(id).(*KubernetesSession)
	if !ok || ks == nil {
		return nil, fmt.Errorf("session settings unavailable")
	}
	secret, err := m.client.CoreV1().Secrets(m.namespace).Get(ctx, strings.TrimSuffix(ks.ServiceName(), "-svc")+"-settings", metav1.GetOptions{})
	if err == nil && len(secret.Data["settings.yaml"]) > 0 {
		var settings sessionsettings.SessionSettings
		if err := yaml.Unmarshal(secret.Data["settings.yaml"], &settings); err != nil {
			return nil, fmt.Errorf("invalid persisted session settings")
		}
		return &settings, nil
	}
	if err != nil && !apierrors.IsNotFound(err) {
		return nil, err
	}
	if ks.ProvisionSettings() == nil {
		return nil, errSessionSettingsUnavailable
	}
	return ks.ProvisionSettings(), nil
}

func (m *KubernetesSessionManager) ManagedFileSyncOwner(ctx context.Context, id, syncID string) (string, error) {
	ks, ok := m.GetSession(id).(*KubernetesSession)
	if !ok || ks == nil {
		return "", fmt.Errorf("session not found")
	}
	svc, err := m.client.CoreV1().Services(m.namespace).Get(ctx, ks.ServiceName(), metav1.GetOptions{})
	if err != nil {
		return "", err
	}
	if svc.Annotations[restartHoldAnnotation] == "true" {
		return "", fmt.Errorf("session is stopped")
	}
	settings, err := m.CurrentSessionSettings(ctx, id)
	if err != nil {
		return "", err
	}
	if settings.CredentialSyncID != syncID {
		return "", fmt.Errorf("stale credential sync")
	}
	if settings.CredentialOwner != "" {
		return settings.CredentialOwner, nil
	}
	if settings.Restart {
		return "", fmt.Errorf("no credential sync owner")
	}
	return ks.UserID(), nil
}

// Ready endpoints may still describe the deleted Pod. Require a new, ready Pod.
func (m *KubernetesSessionManager) restartedPodReady(ctx context.Context, ks *KubernetesSession) (bool, error) {
	svc, err := m.client.CoreV1().Services(m.namespace).Get(ctx, ks.ServiceName(), metav1.GetOptions{})
	if err != nil {
		return false, err
	}
	if len(svc.Spec.Selector) == 0 {
		return false, fmt.Errorf("missing workload selector")
	}
	pods, err := m.client.CoreV1().Pods(m.namespace).List(ctx, metav1.ListOptions{LabelSelector: labels.SelectorFromSet(svc.Spec.Selector).String()})
	if err != nil {
		return false, err
	}
	for _, pod := range pods.Items {
		if pod.DeletionTimestamp != nil {
			continue
		}
		for _, condition := range pod.Status.Conditions {
			if condition.Type == "Ready" && condition.Status == "True" {
				return true, nil
			}
		}
	}
	return false, nil
}

func (m *KubernetesSessionManager) provisionerLifecycle(ctx context.Context, ks *KubernetesSession, action string, settings *sessionsettings.SessionSettings) error {
	var raw []byte
	if settings != nil {
		var err error
		raw, err = json.Marshal(settings)
		if err != nil {
			return err
		}
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, fmt.Sprintf("http://%s:%d/%s", ks.ServiceDNS(), ProvisionerPort, action), bytes.NewReader(raw))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+m.k8sConfig.ProvisionerToken)
	req.Header.Set("Content-Type", "application/json")
	resp, err := m.restartClient().Do(req)
	if err != nil {
		return err
	}
	defer closeutils.SafeCloseResponseBody(resp.Body)
	drained, drainErr := io.Copy(io.Discard, io.LimitReader(resp.Body, 4096))
	if drainErr != nil {
		return fmt.Errorf("read provisioner %s response after %d bytes: %w", action, drained, drainErr)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("provisioner %s failed: HTTP %d", action, resp.StatusCode)
	}
	return nil
}
