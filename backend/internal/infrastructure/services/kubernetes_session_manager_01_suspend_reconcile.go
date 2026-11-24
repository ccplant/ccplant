package services

import (
	"context"
	stderrors "errors"
	"log"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func (m *KubernetesSessionManager) runSessionSuspendReconciler(ctx context.Context) {
	ticker := time.NewTicker(15 * time.Second)
	defer ticker.Stop()
	for {
		if ctx.Err() != nil {
			return
		}
		m.reconcileSessionSuspends(ctx)
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func (m *KubernetesSessionManager) reconcileSessionSuspends(ctx context.Context) {
	services, err := m.client.CoreV1().Services(m.namespace).List(ctx, metav1.ListOptions{LabelSelector: m.managerScopedSelector("agentapi.proxy/session-id")})
	if err != nil {
		log.Printf("[K8S_SESSION] Failed to list session suspend timers: %v", err)
		return
	}
	now := time.Now()
	for i := range services.Items {
		m.reconcileSessionSuspend(ctx, &services.Items[i], now)
	}
}

func (m *KubernetesSessionManager) reconcileSessionSuspend(ctx context.Context, service *corev1.Service, now time.Time) {
	if service.Annotations[restartHoldAnnotation] == "true" {
		return
	}
	sessionID := service.Labels["agentapi.proxy/session-id"]
	var session *KubernetesSession
	if sessionID != "" {
		session = m.kubernetesSession(sessionID)
	}
	if isStockService(service) {
		m.clearStockSuspendTimer(ctx, service)
		return
	}
	if m.initializeMissingSuspendTimer(ctx, service, sessionID) {
		return
	}
	deadline, err := time.Parse(time.RFC3339Nano, service.Annotations[sessionSuspendAtAnnotation])
	if err != nil || deadline.After(now) || sessionID == "" {
		return
	}
	check := activeSuspendCheck{session: session, sessionID: sessionID, deadline: deadline, now: now}
	if m.postponeActiveSessionSuspend(ctx, check) {
		return
	}
	m.handleIdleSuspendResult(ctx, sessionID, m.suspendIdleSession(ctx, sessionID))
}

func (m *KubernetesSessionManager) initializeMissingSuspendTimer(ctx context.Context, service *corev1.Service, sessionID string) bool {
	if service.Annotations[sessionSuspendAtAnnotation] != "" || service.Annotations[sessionSuspendedAtAnnotation] != "" {
		return false
	}
	policyEnabled, policyKnown := service.Annotations[sessionAutoSuspendEnabledAnnotation]
	if policyKnown && policyEnabled != "true" {
		return false
	}
	if sessionID != "" {
		m.rescheduleSessionSuspend(ctx, sessionID, "initialize")
	}
	return true
}

type activeSuspendCheck struct {
	session       *KubernetesSession
	sessionID     string
	deadline, now time.Time
}

func (m *KubernetesSessionManager) postponeActiveSessionSuspend(ctx context.Context, check activeSuspendCheck) bool {
	if check.session == nil || check.session.Status() == "suspended" {
		return false
	}
	running, confirmed := m.podReportsRunning(ctx, check.session)
	if running {
		m.rescheduleSessionSuspend(ctx, check.sessionID, "move")
		return true
	}
	if !confirmed && check.now.Sub(check.deadline) < podRuntimeStatusUnknownGrace {
		log.Printf("[K8S_SESSION] Idle suspend of session %s postponed: pod did not report its runtime status", check.sessionID)
		return true
	}
	return false
}

func (m *KubernetesSessionManager) handleIdleSuspendResult(ctx context.Context, sessionID string, err error) {
	switch {
	case err == nil:
	case stderrors.Is(err, errSessionSuspendNoLongerDue):
		log.Printf("[K8S_SESSION] Idle suspend of session %s cancelled: %v", sessionID, err)
	case stderrors.Is(err, errSessionSetupInProgress):
		log.Printf("[K8S_SESSION] Idle suspend of session %s postponed: %v", sessionID, err)
		m.rescheduleSessionSuspend(ctx, sessionID, "postpone")
	case stderrors.Is(err, errSessionSetupFailed):
		log.Printf("[K8S_SESSION] Idle suspend of session %s skipped: %v", sessionID, err)
		if session := m.kubernetesSession(sessionID); session != nil && session.Status() != "error" {
			session.SetStatusMessage(err.Error())
			session.SetStatus("error")
		}
		m.rescheduleSessionSuspend(ctx, sessionID, "setup failure")
	default:
		log.Printf("[K8S_SESSION] Failed to suspend session %s: %v", sessionID, err)
	}
}

func (m *KubernetesSessionManager) rescheduleSessionSuspend(ctx context.Context, sessionID, reason string) {
	if err := m.ScheduleSessionSuspend(ctx, sessionID); err != nil {
		log.Printf("[K8S_SESSION] Failed to %s suspend timer for session %s: %v", reason, sessionID, err)
	}
}
