package services

import (
	"context"
	stderrors "errors"
	"fmt"
	"log"
	"strconv"
	"time"

	"github.com/takutakahashi/agentapi-proxy/internal/domain/entities"
	"github.com/takutakahashi/agentapi-proxy/pkg/sessionsettings"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
)

// ScheduleSessionSuspend persists the deadline on the canonical Service. The
// annotation survives proxy restarts and is reconciled by every proxy replica.
func (m *KubernetesSessionManager) ScheduleSessionSuspend(ctx context.Context, sessionID string) error {
	session := m.GetSession(sessionID)
	if session == nil {
		return fmt.Errorf("session not found: %s", sessionID)
	}
	ks, ok := session.(*KubernetesSession)
	if !ok {
		return fmt.Errorf("session suspend is only supported by the Kubernetes session manager")
	}
	svc, getErr := m.client.CoreV1().Services(m.namespace).Get(ctx, ks.ServiceName(), metav1.GetOptions{})
	if getErr == nil && isStockService(svc) {
		return nil
	}
	after, enabled, err := m.resolveAutoSuspendPolicy(ctx, ks)
	if err != nil {
		return err
	}
	if getErr == nil {
		after, enabled = persistedAutoSuspendPolicy(svc, ks, after, enabled)
	}
	if !enabled {
		patch := []byte(fmt.Sprintf(`{"metadata":{"annotations":{"%s":null,"%s":"false","%s":null}}}`, sessionSuspendAtAnnotation, sessionAutoSuspendEnabledAnnotation, sessionAutoSuspendIdleSecondsAnnotation))
		if _, err := m.client.CoreV1().Services(m.namespace).Patch(ctx, ks.ServiceName(), types.MergePatchType, patch, metav1.PatchOptions{}); err != nil {
			return err
		}
		log.Printf("[K8S_SESSION] Auto suspend is disabled for session %s; no suspend deadline scheduled", sessionID)
		return nil
	}
	deadline := time.Now().Add(after).UTC().Format(time.RFC3339Nano)
	patch := []byte(fmt.Sprintf(`{"metadata":{"annotations":{"%s":%q,"%s":null,"%s":"true","%s":%q}}}`, sessionSuspendAtAnnotation, deadline, sessionSuspendedAtAnnotation, sessionAutoSuspendEnabledAnnotation, sessionAutoSuspendIdleSecondsAnnotation, strconv.FormatInt(int64(after/time.Second), 10)))
	if _, err := m.client.CoreV1().Services(m.namespace).Patch(ctx, ks.ServiceName(), types.MergePatchType, patch, metav1.PatchOptions{}); err != nil {
		return fmt.Errorf("persist suspend deadline: %w", err)
	}
	log.Printf("[K8S_SESSION] Session %s scheduled to suspend at %s", sessionID, deadline)
	return nil
}

func persistedAutoSuspendPolicy(service *corev1.Service, session *KubernetesSession, fallback time.Duration, enabled bool) (time.Duration, bool) {
	request := session.Request()
	hasCachedPolicy := request != nil && request.ProvisionSettings != nil && request.ProvisionSettings.Session.AutoSuspendEnabled != nil
	hasPersistedPolicy := service.Annotations[sessionAutoSuspendPolicySourceAnnotation] == "resume"
	if !hasCachedPolicy && !hasPersistedPolicy {
		return fallback, enabled
	}
	if rawEnabled, ok := service.Annotations[sessionAutoSuspendEnabledAnnotation]; ok {
		if parsed, err := strconv.ParseBool(rawEnabled); err == nil {
			enabled = parsed
		}
	}
	rawSeconds := service.Annotations[sessionAutoSuspendIdleSecondsAnnotation]
	seconds, err := strconv.ParseInt(rawSeconds, 10, 64)
	if !enabled || err != nil || seconds <= 0 {
		return fallback, enabled
	}
	return time.Duration(seconds) * time.Second, enabled
}

// ApplyRunnerAutoSuspendPolicy reconciles allocation-specific policy onto a
// pre-warmed runner. Stock workloads start with the manager default and only
// learn the owning user's policy after the parent assigns an allocation.
func (m *KubernetesSessionManager) ApplyRunnerAutoSuspendPolicy(ctx context.Context, sessionID string, enabled *bool, minutes int) error {
	if enabled == nil {
		return nil
	}
	session, ok := m.GetSession(sessionID).(*KubernetesSession)
	if !ok || session == nil {
		return fmt.Errorf("session not found: %s", sessionID)
	}
	service, err := m.client.CoreV1().Services(m.namespace).Get(ctx, session.ServiceName(), metav1.GetOptions{})
	if err != nil {
		return fmt.Errorf("get session service: %w", err)
	}
	if service.Annotations[sessionAutoSuspendPolicySourceAnnotation] == "resume" {
		return nil
	}
	desiredEnabled := strconv.FormatBool(*enabled)
	desiredSeconds := strconv.FormatInt(int64(time.Duration(minutes)*time.Minute/time.Second), 10)
	policyAlreadyScheduled := service.Annotations[sessionAutoSuspendEnabledAnnotation] == desiredEnabled &&
		(!*enabled || service.Annotations[sessionAutoSuspendIdleSecondsAnnotation] == desiredSeconds) &&
		(service.Annotations[sessionSuspendAtAnnotation] != "" || service.Annotations[sessionSuspendedAtAnnotation] != "")
	setRunnerAutoSuspendRequest(session, enabled, minutes)
	if policyAlreadyScheduled {
		return nil
	}
	if !*enabled {
		patch := []byte(fmt.Sprintf(`{"metadata":{"annotations":{"%s":null,"%s":null,"%s":"false","%s":null}}}`, sessionSuspendAtAnnotation, sessionSuspendedAtAnnotation, sessionAutoSuspendEnabledAnnotation, sessionAutoSuspendIdleSecondsAnnotation))
		updated, patchErr := m.client.CoreV1().Services(m.namespace).Patch(ctx, session.ServiceName(), types.MergePatchType, patch, metav1.PatchOptions{})
		if patchErr == nil && updated == nil {
			patchErr = stderrors.New("disable auto suspend patch returned no service")
		}
		return patchErr
	}
	deadline := time.Now().Add(time.Duration(minutes) * time.Minute).UTC().Format(time.RFC3339Nano)
	patch := []byte(fmt.Sprintf(`{"metadata":{"annotations":{"%s":%q,"%s":null,"%s":%q,"%s":%q}}}`, sessionSuspendAtAnnotation, deadline, sessionSuspendedAtAnnotation, sessionAutoSuspendEnabledAnnotation, desiredEnabled, sessionAutoSuspendIdleSecondsAnnotation, desiredSeconds))
	updated, patchErr := m.client.CoreV1().Services(m.namespace).Patch(ctx, session.ServiceName(), types.MergePatchType, patch, metav1.PatchOptions{})
	if patchErr == nil && updated == nil {
		patchErr = stderrors.New("auto suspend patch returned no service")
	}
	return patchErr
}

func setRunnerAutoSuspendRequest(session *KubernetesSession, enabled *bool, minutes int) {
	request := session.Request()
	if request == nil {
		request = &entities.RunServerRequest{}
		session.SetRequest(request)
	}
	if request.ProvisionSettings == nil {
		request.ProvisionSettings = &sessionsettings.SessionSettings{}
	}
	request.ProvisionSettings.Session.AutoSuspendEnabled = enabled
	request.ProvisionSettings.Session.AutoSuspendMinutes = minutes
}

// ApplyRunnerAllocationMetadata persists allocation-specific metadata onto a
// pre-warmed runner so every manager replica can reconstruct the claimed session.
func (m *KubernetesSessionManager) ApplyRunnerAllocationMetadata(ctx context.Context, sessionID string, meta sessionsettings.SessionMeta) error {
	session, ok := m.GetSession(sessionID).(*KubernetesSession)
	if !ok || session == nil {
		return fmt.Errorf("session not found: %s", sessionID)
	}
	if meta.AgentType != "" {
		req := session.Request()
		if req == nil {
			req = &entities.RunServerRequest{}
			session.SetRequest(req)
		}
		req.AgentType = meta.AgentType
		patch := []byte(fmt.Sprintf(`{"metadata":{"annotations":{"agentapi.proxy/agent-type":%q}}}`, meta.AgentType))
		if _, err := m.client.CoreV1().Services(m.namespace).Patch(ctx, session.ServiceName(), types.MergePatchType, patch, metav1.PatchOptions{}); err != nil {
			return fmt.Errorf("persist runner agent type: %w", err)
		}
	}
	return m.ApplyRunnerAutoSuspendPolicy(ctx, sessionID, meta.AutoSuspendEnabled, meta.AutoSuspendMinutes)
}

// RefreshRunnerSessionStatus reconciles a leader's in-memory runner status
// with the Service and the workload that may have been changed by another replica.
func (m *KubernetesSessionManager) RefreshRunnerSessionStatus(ctx context.Context, sessionID string) error {
	session, ok := m.GetSession(sessionID).(*KubernetesSession)
	if !ok || session == nil {
		return fmt.Errorf("session not found: %s", sessionID)
	}
	svc, err := m.client.CoreV1().Services(m.namespace).Get(ctx, session.ServiceName(), metav1.GetOptions{})
	if err != nil {
		return fmt.Errorf("get runner service: %w", err)
	}
	// Provisioning status is persisted in a Secret, while the in-memory status
	// update is local to the manager replica that receives the provisioner's
	// callback. The heartbeat leader can therefore still have this runner as
	// starting after setup failed on another replica. Reconcile the terminal
	// status before reporting it to the parent API.
	if provisionReq, provisionErr := m.getProvisionRequest(ctx, sessionID); provisionErr == nil && provisionReq.Status == "error" {
		session.SetStatusMessage(provisionReq.Message)
		session.SetErrorCode(provisionReq.ErrorCode)
		session.SetStatus("error")
		return nil
	}
	status := session.Status()
	if svc.Annotations[sessionSuspendedAtAnnotation] != "" {
		if status == "stopped" || !statusWatcherTerminal(status) {
			session.SetStatus("suspended")
		}
		return nil
	}
	if status != "suspended" && status != "stopped" {
		return nil
	}
	pods, err := m.client.CoreV1().Pods(m.namespace).List(ctx, metav1.ListOptions{
		LabelSelector: "agentapi.proxy/session-id=" + sessionID,
	})
	if err != nil {
		return fmt.Errorf("list runner pods: %w", err)
	}
	if hasNewReadyRunnerPod(pods.Items, session.UpdatedAt()) {
		session.SetStatus("starting")
	}
	return nil
}

func hasNewReadyRunnerPod(pods []corev1.Pod, updatedAt time.Time) bool {
	for i := range pods {
		pod := &pods[i]
		if pod.DeletionTimestamp == nil && isPodReady(pod) && pod.CreationTimestamp.After(updatedAt) {
			return true
		}
	}
	return false
}

func (m *KubernetesSessionManager) resolveAutoSuspendPolicy(ctx context.Context, session *KubernetesSession) (time.Duration, bool, error) {
	if after, enabled, found := m.configuredAutoSuspendPolicy(ctx, session); found {
		return after, enabled, nil
	}
	if m.config.SessionPersistence.Backend == "" {
		return 0, false, nil
	}
	return m.defaultAutoSuspendPolicy()
}

func (m *KubernetesSessionManager) configuredAutoSuspendPolicy(ctx context.Context, session *KubernetesSession) (time.Duration, bool, bool) {
	request := session.Request()
	if request == nil {
		return 0, false, false
	}
	if request.ProvisionSettings != nil && request.ProvisionSettings.Session.AutoSuspendEnabled != nil {
		policy := request.ProvisionSettings.Session
		return time.Duration(policy.AutoSuspendMinutes) * time.Minute, *policy.AutoSuspendEnabled, true
	}
	if m.settingsRepo == nil {
		return 0, false, false
	}
	settingsName := request.UserID
	if request.Scope == entities.ScopeTeam && request.TeamID != "" {
		settingsName = request.TeamID
	}
	settings, err := m.settingsRepo.FindByName(ctx, settingsName)
	if err != nil || settings.AutoSuspend() == nil {
		return 0, false, false
	}
	policy := settings.AutoSuspend()
	return time.Duration(policy.IdleTimeoutMinutes) * time.Minute, policy.Enabled, true
}

func (m *KubernetesSessionManager) defaultAutoSuspendPolicy() (time.Duration, bool, error) {
	if m.config.SessionPersistence.SuspendAfter == "" || m.config.SessionPersistence.SuspendAfter == "0" {
		return 0, false, nil
	}
	after, err := time.ParseDuration(m.config.SessionPersistence.SuspendAfter)
	if err != nil {
		return 0, false, fmt.Errorf("invalid session suspend timer: %w", err)
	}
	if after <= 0 {
		return 0, false, fmt.Errorf("invalid session suspend timer: duration must be positive")
	}
	return after, true, nil
}

// SuspendSession checkpoints persistent ACP state under the manager's policy
// and only then removes the workload. Callers request suspension; checkpointing
// remains an execution-plane concern.
func (m *KubernetesSessionManager) SuspendSession(ctx context.Context, sessionID string) error {
	return m.suspendSession(ctx, sessionID, nil)
}

func (m *KubernetesSessionManager) SuspendSessionWithSettings(ctx context.Context, sessionID string, settings *sessionsettings.SessionSettings) error {
	return m.suspendSession(ctx, sessionID, settings)
}

var (
	errSessionSetupInProgress    = stderrors.New("session setup has not completed")
	errSessionSetupFailed        = stderrors.New("session setup failed")
	errSessionSuspendNoLongerDue = stderrors.New("session is no longer due for idle suspend")
)

func (m *KubernetesSessionManager) suspendSession(ctx context.Context, sessionID string, settings *sessionsettings.SessionSettings) error {
	return m.suspendSessionIf(ctx, sessionID, settings, false)
}

func (m *KubernetesSessionManager) suspendIdleSession(ctx context.Context, sessionID string) error {
	return m.suspendSessionIf(ctx, sessionID, nil, true)
}

func (m *KubernetesSessionManager) podReportsRunning(ctx context.Context, session *KubernetesSession) (running bool, confirmed bool) {
	// The runtime pushes status over its outbound direct-runtime connection.
	// The manager must never probe the loopback-only agent listener through a
	// Kubernetes Service.
	return session.Status() == "running", true
}

func idleSuspendStillDue(running bool, svc *corev1.Service, requireExpiredDeadline bool) error {
	if running {
		return errSessionSuspendNoLongerDue
	}
	if !requireExpiredDeadline {
		return nil
	}
	deadline, err := time.Parse(time.RFC3339Nano, svc.Annotations[sessionSuspendAtAnnotation])
	if err != nil || deadline.After(time.Now()) {
		return errSessionSuspendNoLongerDue
	}
	return nil
}

func (m *KubernetesSessionManager) suspendSessionIf(ctx context.Context, sessionID string, settings *sessionsettings.SessionSettings, requireExpiredDeadline bool) error {
	session := m.GetSession(sessionID)
	if session == nil {
		return fmt.Errorf("session not found: %s", sessionID)
	}
	ks, ok := session.(*KubernetesSession)
	if !ok {
		return fmt.Errorf("session suspend is only supported by the Kubernetes session manager")
	}
	if session.Status() == "suspended" {
		return nil
	}
	if running, confirmed := m.podReportsRunning(ctx, ks); !confirmed || running {
		return fmt.Errorf("session is busy")
	}
	svc, claim, acquired, err := m.acquireSessionSuspendOwnership(ctx, ks.ServiceName())
	if err != nil {
		return fmt.Errorf("acquire session suspend ownership: %w", err)
	}
	if !acquired {
		return nil
	}
	running, confirmed := m.podReportsRunning(ctx, ks)
	if !confirmed {
		return m.releaseSuspendOwnershipAfterError(ctx, svc.Name, claim, stderrors.New("session activity could not be confirmed"))
	}
	if err := idleSuspendStillDue(running, svc, requireExpiredDeadline); err != nil {
		return m.releaseSuspendOwnershipAfterError(ctx, svc.Name, claim, err)
	}
	saved, checkpointErr := m.checkpointSuspendSession(ctx, ks, settings, requireExpiredDeadline)
	if checkpointErr != nil {
		return m.releaseSuspendOwnershipAfterError(ctx, svc.Name, claim, checkpointErr)
	}
	if err := m.persistSuspendSettings(ctx, sessionID, settings, saved); err != nil {
		return m.releaseSuspendOwnershipAfterError(ctx, svc.Name, claim, fmt.Errorf("persist suspend settings: %w", err))
	}
	return m.suspendSessionWorkloadOwned(ctx, sessionID, svc, claim)
}

func (m *KubernetesSessionManager) checkpointSuspendSession(ctx context.Context, session *KubernetesSession, settings *sessionsettings.SessionSettings, requireExpiredDeadline bool) (bool, error) {
	if !m.requiresSessionCheckpoint(session) {
		return false, nil
	}
	if err := m.checkpointSessionStateWithSettings(ctx, checkpointRequest{sessionID: session.ID(), override: settings, parkHerdr: true}); err != nil {
		return false, fmt.Errorf("checkpoint session state: %w", err)
	}
	current, err := m.client.CoreV1().Services(m.namespace).Get(ctx, session.ServiceName(), metav1.GetOptions{})
	if err != nil {
		return false, fmt.Errorf("recheck session suspend: %w", err)
	}
	running, confirmed := m.podReportsRunning(ctx, session)
	if !confirmed {
		return false, stderrors.New("session activity could not be confirmed")
	}
	if err := idleSuspendStillDue(running, current, requireExpiredDeadline); err != nil {
		return false, err
	}
	return true, nil
}

func (m *KubernetesSessionManager) releaseSuspendOwnershipAfterError(ctx context.Context, serviceName, claim string, cause error) error {
	releaseErr := m.releaseSessionSuspendOwnership(context.WithoutCancel(ctx), serviceName, claim)
	return stderrors.Join(cause, releaseErr)
}

// SuspendSessionOnTermination is called by a PVC-backed session Pod after it
// receives SIGTERM and flushes its local agent state to the volume. It bypasses
// the ordinary busy/checkpoint checks because the terminating Pod has already
// stopped its agent process and cannot continue serving work. Deleting the
// Deployment prevents its controller from replacing an evicted Pod before the
// user explicitly resumes the session.
func (m *KubernetesSessionManager) SuspendSessionOnTermination(ctx context.Context, sessionID string) error {
	if !m.isPVCEnabled() {
		return fmt.Errorf("termination suspend requires a PVC-backed session")
	}
	serviceName := fmt.Sprintf("agentapi-session-%s-svc", sessionID)
	if session, ok := m.GetSession(sessionID).(*KubernetesSession); ok && session != nil {
		if session.Status() == "suspended" {
			return nil
		}
		serviceName = session.ServiceName()
	}
	svc, err := m.client.CoreV1().Services(m.namespace).Get(ctx, serviceName, metav1.GetOptions{})
	if errors.IsNotFound(err) {
		// Explicit session deletion removes the Service before the Pod finishes
		// its SIGTERM handler. There is nothing left to suspend in that case.
		return nil
	}
	if err != nil {
		return err
	}
	ownedSvc, claim, acquired, err := m.acquireSessionSuspendOwnership(ctx, svc.Name)
	if err != nil {
		return err
	}
	if !acquired {
		return nil
	}
	if err := m.persistSuspendSettings(ctx, sessionID, nil, m.config.SessionPersistence.Backend != ""); err != nil {
		return m.releaseSuspendOwnershipAfterError(ctx, ownedSvc.Name, claim, fmt.Errorf("persist suspend settings: %w", err))
	}
	return m.suspendSessionWorkloadOwned(ctx, sessionID, ownedSvc, claim)
}
