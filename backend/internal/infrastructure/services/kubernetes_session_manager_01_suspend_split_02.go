package services

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	coresessioncontrol "github.com/takutakahashi/agentapi-proxy/internal/core/sessioncontrol"
	"github.com/takutakahashi/agentapi-proxy/pkg/sessionsettings"
	closeutils "github.com/takutakahashi/agentapi-proxy/pkg/utils"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/util/retry"
)

func (m *KubernetesSessionManager) acquireSessionSuspendOwnership(ctx context.Context, serviceName string) (*corev1.Service, string, bool, error) {
	svc, err := m.client.CoreV1().Services(m.namespace).Get(ctx, serviceName, metav1.GetOptions{})
	if err != nil {
		return nil, "", false, err
	}
	now := time.Now()
	owner := svc.Annotations[sessionSuspendOwnerAnnotation]
	expiresAt, parseErr := time.Parse(time.RFC3339Nano, svc.Annotations[sessionSuspendOwnerExpiresAtAnnotation])
	if owner != "" && parseErr == nil && expiresAt.After(now) {
		return svc, "", false, nil
	}
	claim := m.podID + "/" + uuid.NewString()
	updated := svc.DeepCopy()
	if updated.Annotations == nil {
		updated.Annotations = map[string]string{}
	}
	updated.Annotations[sessionSuspendOwnerAnnotation] = claim
	updated.Annotations[sessionSuspendOwnerExpiresAtAnnotation] = now.Add(sessionSuspendOwnershipTTL).UTC().Format(time.RFC3339Nano)
	updated, err = m.client.CoreV1().Services(m.namespace).Update(ctx, updated, metav1.UpdateOptions{})
	if errors.IsConflict(err) {
		return svc, "", false, nil
	}
	if err != nil {
		return nil, "", false, err
	}
	return updated, claim, true, nil
}

func (m *KubernetesSessionManager) releaseSessionSuspendOwnership(ctx context.Context, serviceName, claim string) error {
	return retry.RetryOnConflict(retry.DefaultRetry, func() error {
		svc, err := m.client.CoreV1().Services(m.namespace).Get(ctx, serviceName, metav1.GetOptions{})
		if err != nil {
			return err
		}
		if svc.Annotations[sessionSuspendOwnerAnnotation] != claim {
			return nil
		}
		updated := svc.DeepCopy()
		delete(updated.Annotations, sessionSuspendOwnerAnnotation)
		delete(updated.Annotations, sessionSuspendOwnerExpiresAtAnnotation)
		result, updateErr := m.client.CoreV1().Services(m.namespace).Update(ctx, updated, metav1.UpdateOptions{})
		if updateErr == nil && result == nil {
			updateErr = fmt.Errorf("release suspend ownership returned no Service")
		}
		return updateErr
	})
}

func (m *KubernetesSessionManager) requiresSessionCheckpoint(session *KubernetesSession) bool {
	if m.config.SessionPersistence.Backend == "" || session == nil || session.Request() == nil {
		return false
	}
	agentType := session.Request().AgentType
	return agentType == "claude-acp" || agentType == "codex-acp"
}

func (m *KubernetesSessionManager) checkpointSessionState(ctx context.Context, sessionID string, snapshotID ...string) error {
	templateID := ""
	if len(snapshotID) > 0 {
		templateID = snapshotID[0]
	}
	return m.checkpointSessionStateWithSettings(ctx, checkpointRequest{sessionID: sessionID, templateID: templateID})
}

type checkpointRequest struct {
	sessionID, templateID string
	override              *sessionsettings.SessionSettings
	parkHerdr             bool
}

func (m *KubernetesSessionManager) checkpointSessionStateWithSettings(ctx context.Context, request checkpointRequest) error {
	if runtime := m.checkpointParentRuntime(ctx, request); runtime != nil {
		return requestParentSessionCheckpoint(ctx, runtime, request.templateID, request.parkHerdr)
	}
	return m.awaitCheckpointCommand(ctx, request)
}

func (m *KubernetesSessionManager) checkpointParentRuntime(ctx context.Context, request checkpointRequest) *sessionsettings.ParentRuntimeConfig {
	session := m.kubernetesSession(request.sessionID)
	if session == nil {
		return nil
	}
	settings := session.ProvisionSettings()
	if request.override != nil {
		settings = m.normalizeProvisionSettings(request.override)
	}
	if settings == nil || settings.ParentRuntime == nil {
		if persisted, err := m.CurrentSessionSettings(ctx, request.sessionID); err == nil {
			settings = persisted
		}
	}
	if settings == nil || settings.ParentRuntime == nil || !settings.ParentRuntime.Enabled {
		return nil
	}
	return settings.ParentRuntime
}

func (m *KubernetesSessionManager) kubernetesSession(sessionID string) *KubernetesSession {
	session, ok := m.GetSession(sessionID).(*KubernetesSession)
	if !ok {
		return nil
	}
	return session
}

func (m *KubernetesSessionManager) awaitCheckpointCommand(ctx context.Context, request checkpointRequest) error {
	store := m.connectedSessionControlStore(ctx, request.sessionID)
	if store == nil {
		return fmt.Errorf("session control is unavailable")
	}
	commandID := uuid.NewString()
	payload, err := json.Marshal(map[string]interface{}{"snapshot_id": request.templateID, "park_herdr": request.parkHerdr})
	if err != nil {
		return fmt.Errorf("encode checkpoint command: %w", err)
	}
	if _, err := store.EnqueueCommand(ctx, request.sessionID, coresessioncontrol.Command{ID: commandID, Type: "checkpoint_session_state", Payload: payload, CreatedAt: time.Now().UTC()}); err != nil {
		return err
	}
	cursor := "0-0"
	for {
		events, err := store.ReadEvents(ctx, request.sessionID, cursor, 30*time.Second, 100)
		if err != nil {
			return err
		}
		done, eventErr := checkpointEventsResult(events, commandID)
		if done || eventErr != nil {
			return eventErr
		}
		if len(events) > 0 {
			cursor = events[len(events)-1].StreamID
		}
		if err := ctx.Err(); err != nil {
			return err
		}
	}
}

func checkpointEventsResult(events []coresessioncontrol.Event, commandID string) (bool, error) {
	for _, event := range events {
		if event.CommandID == commandID {
			done, err := checkpointEventResult(event)
			if done || err != nil {
				return done, err
			}
		}
	}
	return false, nil
}

func checkpointEventResult(event coresessioncontrol.Event) (bool, error) {
	if event.Type == "command_completed" {
		return true, nil
	}
	if event.Type != "command_failed" {
		return false, nil
	}
	var payload struct {
		Error  string `json:"error"`
		Reason string `json:"reason"`
	}
	if err := json.Unmarshal(event.Payload, &payload); err != nil {
		return true, fmt.Errorf("decode checkpoint failure: %w", err)
	}
	switch payload.Reason {
	case "setup_in_progress":
		return true, errSessionSetupInProgress
	case "setup_failed":
		return true, errSessionSetupFailed
	}
	if payload.Error == "" {
		payload.Error = "checkpoint command failed"
	}
	return true, fmt.Errorf("%s", payload.Error)
}

// CheckpointSessionState exposes checkpointing without suspending the workload.
func (m *KubernetesSessionManager) CheckpointSessionState(ctx context.Context, sessionID string) error {
	session, ok := m.GetSession(sessionID).(*KubernetesSession)
	if !ok || session == nil {
		return fmt.Errorf("session not found: %s", sessionID)
	}
	// Stock sessions and sessions reconstructed by another manager replica do
	// not retain their original RunServerRequest. The public template endpoint
	// has already restricted this operation to ACP sessions, so a missing
	// request must not make an otherwise persistent session look unsupported.
	if m.config.SessionPersistence.Backend == "" {
		return fmt.Errorf("session context templates require a persistent ACP session")
	}
	if req := session.Request(); req != nil && req.AgentType != "" && req.AgentType != "claude-acp" && req.AgentType != "codex-acp" {
		return fmt.Errorf("session context templates require a persistent ACP session")
	}
	return m.checkpointSessionState(ctx, sessionID)
}

func (m *KubernetesSessionManager) CreateSessionContextSnapshot(ctx context.Context, sessionID, snapshotID string) error {
	if !isTemplateSnapshotID(snapshotID) {
		return fmt.Errorf("invalid template snapshot ID")
	}
	return m.checkpointSessionState(ctx, sessionID, snapshotID)
}

func requestParentSessionCheckpoint(ctx context.Context, runtime *sessionsettings.ParentRuntimeConfig, snapshotID string, parkHerdr bool) error {
	endpoint := strings.TrimRight(runtime.Endpoint, "/") + "/internal/session-runtime/" + url.PathEscape(runtime.SessionID) + "/checkpoint?generation=" + strconv.FormatInt(runtime.Generation, 10)
	if snapshotID != "" {
		endpoint += "&snapshot_id=" + url.QueryEscape(snapshotID)
	}
	if parkHerdr {
		endpoint += "&park_herdr=true"
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+runtime.Token)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer func() { closeutils.SafeCloseResponseBody(resp.Body) }()
	switch resp.StatusCode {
	case http.StatusTooEarly:
		return errSessionSetupInProgress
	case http.StatusFailedDependency:
		return errSessionSetupFailed
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, readErr := io.ReadAll(io.LimitReader(resp.Body, 4096))
		if readErr != nil {
			return fmt.Errorf("parent checkpoint returned HTTP %d and response could not be read: %w", resp.StatusCode, readErr)
		}
		return fmt.Errorf("parent checkpoint returned HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	return nil
}

func isStockService(svc *corev1.Service) bool {
	_, ok := svc.Labels["agentapi.proxy/stock"]
	return ok
}

func (m *KubernetesSessionManager) clearStockSuspendTimer(ctx context.Context, svc *corev1.Service) {
	if svc.Annotations[sessionSuspendedAtAnnotation] != "" {
		return
	}
	keys := []string{sessionSuspendAtAnnotation, sessionAutoSuspendEnabledAnnotation, sessionAutoSuspendIdleSecondsAnnotation}
	updated := svc.DeepCopy()
	changed := false
	for _, key := range keys {
		if _, ok := updated.Annotations[key]; ok {
			delete(updated.Annotations, key)
			changed = true
		}
	}
	if !changed {
		return
	}
	if _, err := m.client.CoreV1().Services(m.namespace).Update(ctx, updated, metav1.UpdateOptions{}); err != nil && !errors.IsConflict(err) {
		log.Printf("[K8S_SESSION] Failed to clear suspend timer on stock session %s: %v", svc.Labels["agentapi.proxy/session-id"], err)
	}
}

func (m *KubernetesSessionManager) suspendSessionWorkload(ctx context.Context, sessionID string, svc *corev1.Service) error {
	ownedSvc, claim, acquired, err := m.acquireSessionSuspendOwnership(ctx, svc.Name)
	if err != nil {
		return err
	}
	if !acquired {
		return nil
	}
	return m.suspendSessionWorkloadOwned(ctx, sessionID, ownedSvc, claim)
}

func (m *KubernetesSessionManager) suspendSessionWorkloadOwned(ctx context.Context, sessionID string, svc *corev1.Service, claim string) error {
	// Mark the session before deleting its workload so the deployment watcher
	// cannot race with suspension and replace the canonical state with stopped.
	if session := m.GetSession(sessionID); session != nil {
		if ks, ok := session.(*KubernetesSession); ok {
			ks.SetStatus("suspended")
		}
	}
	workloadName := strings.TrimSuffix(svc.Name, "-svc")
	if m.isPVCEnabled() {
		err := m.client.AppsV1().Deployments(m.namespace).Delete(ctx, workloadName, metav1.DeleteOptions{})
		if err != nil && !errors.IsNotFound(err) {
			return err
		}
	} else {
		err := m.client.CoreV1().Pods(m.namespace).Delete(ctx, workloadName, metav1.DeleteOptions{})
		if err != nil && !errors.IsNotFound(err) {
			return err
		}
	}
	suspendedAt := time.Now().UTC().Format(time.RFC3339Nano)
	if err := retry.RetryOnConflict(retry.DefaultRetry, func() error {
		current, err := m.client.CoreV1().Services(m.namespace).Get(ctx, svc.Name, metav1.GetOptions{})
		if err != nil {
			return err
		}
		if claim != "" && current.Annotations[sessionSuspendOwnerAnnotation] != claim {
			return fmt.Errorf("session suspend ownership was lost")
		}
		updated := current.DeepCopy()
		if updated.Annotations == nil {
			updated.Annotations = map[string]string{}
		}
		delete(updated.Annotations, sessionSuspendAtAnnotation)
		delete(updated.Annotations, sessionSuspendOwnerAnnotation)
		delete(updated.Annotations, sessionSuspendOwnerExpiresAtAnnotation)
		updated.Annotations[sessionSuspendedAtAnnotation] = suspendedAt
		result, updateErr := m.client.CoreV1().Services(m.namespace).Update(ctx, updated, metav1.UpdateOptions{})
		if updateErr == nil && result == nil {
			updateErr = fmt.Errorf("persist suspended state returned no Service")
		}
		return updateErr
	}); err != nil {
		return err
	}
	log.Printf("[K8S_SESSION] Suspended session %s; canonical state retained for lazy resume", sessionID)
	return nil
}
