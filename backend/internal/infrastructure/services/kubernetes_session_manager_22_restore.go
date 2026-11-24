package services

import (
	"context"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/takutakahashi/agentapi-proxy/internal/domain/entities"
	"github.com/takutakahashi/agentapi-proxy/pkg/sessionsettings"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
)

// This is used to recover sessions after agentapi-proxy restart
func (m *KubernetesSessionManager) restoreSessionFromService(svc *corev1.Service) *KubernetesSession {
	sessionID := svc.Labels["agentapi.proxy/session-id"]
	userID := svc.Labels["agentapi.proxy/user-id"]

	// Restore tags from labels
	tags := make(map[string]string)
	for k, v := range svc.Labels {
		if strings.HasPrefix(k, "agentapi.proxy/tag-") {
			tagKey := strings.TrimPrefix(k, "agentapi.proxy/tag-")
			tags[tagKey] = v
		}
	}

	// Restore scope from labels
	scope := entities.ResourceScope(svc.Labels["agentapi.proxy/scope"])
	if scope == "" {
		scope = entities.ScopeUser // Default to user scope for backward compatibility
	}
	// Restore team_id from annotations (original unsanitized value)
	// Labels contain only the hash for querying purposes
	teamID := svc.Annotations["agentapi.proxy/team-id"]

	// Restore initial message: prefer Service annotation (written at creation, immediately
	// available across all proxy replicas) and fall back to the settings Secret (written
	// asynchronously after provisioning completes).
	restoreCtx := context.Background()
	initialMessage := svc.Annotations["agentapi.proxy/initial-message"]
	if initialMessage == "" {
		initialMessage = m.getInitialMessageFromSecret(restoreCtx, svc.Name)
	}
	sessionMeta := m.getSessionMetaFromSecret(restoreCtx, svc.Name)

	// Extract teams from session meta if available.
	var teams []string
	if sessionMeta != nil {
		teams = sessionMeta.Teams
	}

	// Parse created-at from annotations
	createdAt := time.Now()
	if createdAtStr, ok := svc.Annotations["agentapi.proxy/created-at"]; ok {
		if parsed, err := time.Parse(time.RFC3339, createdAtStr); err == nil {
			createdAt = parsed
		}
	}

	// Parse updated-at from annotations
	updatedAt := createdAt // Default to createdAt if not set
	if updatedAtStr, ok := svc.Annotations["agentapi.proxy/updated-at"]; ok {
		if parsed, err := time.Parse(time.RFC3339, updatedAtStr); err == nil {
			updatedAt = parsed
		}
	}

	// Parse last-message-at from annotations.
	lastMessageAt := createdAt // Default to createdAt if not set
	if v, ok := svc.Annotations["agentapi.proxy/last-message-at"]; ok && v != "" {
		if parsed, err := time.Parse(time.RFC3339, v); err == nil {
			lastMessageAt = parsed
		}
	}

	// Extract service port
	servicePort := m.k8sConfig.BasePort
	if len(svc.Spec.Ports) > 0 {
		servicePort = int(svc.Spec.Ports[0].Port)
	}

	ctx, cancel := context.WithCancel(context.Background())

	// Parse session-ttl annotation if present
	sessionTTL := svc.Annotations["agentapi.proxy/session-ttl"]
	agentType := restoreAgentTypeFromService(svc)

	// Create session using constructor
	session := NewKubernetesSession(
		sessionID,
		&entities.RunServerRequest{
			UserID:         userID,
			Tags:           tags,
			Scope:          scope,
			TeamID:         teamID,
			InitialMessage: initialMessage,
			Teams:          teams,
			SessionTTL:     sessionTTL,
			AgentType:      agentType,
		},
		fmt.Sprintf("agentapi-session-%s", sessionID),
		svc.Name,
		fmt.Sprintf("agentapi-session-%s-pvc", sessionID),
		m.namespace,
		servicePort,
		cancel,
		nil, // No webhook payload for restored sessions
	)
	if sessionMeta != nil {
		restoredSettings := &sessionsettings.SessionSettings{Session: *sessionMeta}
		session.Request().ProvisionSettings = restoredSettings
		session.SetProvisionSettings(restoredSettings)
	}
	// Set restored values
	session.SetStartedAt(createdAt)
	session.SetUpdatedAt(updatedAt)
	session.SetLastMessageAt(lastMessageAt)
	status := m.getSessionStatusFromDeployment(sessionID)
	if svc.Annotations[sessionSuspendedAtAnnotation] != "" {
		status = "suspended"
	}
	session.SetStatus(status)
	session.SetDescription(initialMessage) // Cache initial message as description
	session.SetAnnotations(sessionAnnotationsFromMap(svc.Annotations))

	// Register proxy-wide status change broadcaster
	session.statusChangeCallback = m.broadcastStatusChange

	// Add to memory map
	m.mutex.Lock()
	m.sessions[sessionID] = session
	m.mutex.Unlock()

	// Start watching deployment health and agentapi runtime status.
	m.startWorkloadStatusWatchers(ctx, session)
	go m.scheduleSuspendWhenRestoredWorkloadReady(session)

	log.Printf("[K8S_SESSION] Restored session %s from Service", sessionID)

	return session
}

// restoreSessionFromServiceWithWorkload restores a session from Kubernetes Service
// using a pre-fetched workload to avoid additional API calls.
func (m *KubernetesSessionManager) restoreSessionFromServiceWithWorkload(svc *corev1.Service, deployment *appsv1.Deployment, pod *corev1.Pod) *KubernetesSession {
	sessionID := svc.Labels["agentapi.proxy/session-id"]
	userID := svc.Labels["agentapi.proxy/user-id"]

	// Restore tags from labels
	tags := make(map[string]string)
	for k, v := range svc.Labels {
		if strings.HasPrefix(k, "agentapi.proxy/tag-") {
			tagKey := strings.TrimPrefix(k, "agentapi.proxy/tag-")
			tags[tagKey] = v
		}
	}

	// Restore scope from labels
	scope := entities.ResourceScope(svc.Labels["agentapi.proxy/scope"])
	if scope == "" {
		scope = entities.ScopeUser // Default to user scope for backward compatibility
	}
	// Restore team_id from annotations (original unsanitized value)
	// Labels contain only the hash for querying purposes
	teamID := svc.Annotations["agentapi.proxy/team-id"]

	// Restore initial message: prefer Service annotation (written at creation, immediately
	// available across all proxy replicas) and fall back to the settings Secret (written
	// asynchronously after provisioning completes).
	restoreCtx := context.Background()
	initialMessage := svc.Annotations["agentapi.proxy/initial-message"]
	if initialMessage == "" {
		initialMessage = m.getInitialMessageFromSecret(restoreCtx, svc.Name)
	}
	sessionMeta := m.getSessionMetaFromSecret(restoreCtx, svc.Name)

	// Extract teams from session meta if available.
	var teams []string
	if sessionMeta != nil {
		teams = sessionMeta.Teams
	}

	// Parse created-at from annotations
	createdAt := time.Now()
	if createdAtStr, ok := svc.Annotations["agentapi.proxy/created-at"]; ok {
		if parsed, err := time.Parse(time.RFC3339, createdAtStr); err == nil {
			createdAt = parsed
		}
	}

	// Parse updated-at from annotations
	updatedAt := createdAt // Default to createdAt if not set
	if updatedAtStr, ok := svc.Annotations["agentapi.proxy/updated-at"]; ok {
		if parsed, err := time.Parse(time.RFC3339, updatedAtStr); err == nil {
			updatedAt = parsed
		}
	}

	// Parse last-message-at from annotations.
	lastMessageAt := createdAt // Default to createdAt if not set
	if v, ok := svc.Annotations["agentapi.proxy/last-message-at"]; ok && v != "" {
		if parsed, err := time.Parse(time.RFC3339, v); err == nil {
			lastMessageAt = parsed
		}
	}

	// Extract service port
	servicePort := m.k8sConfig.BasePort
	if len(svc.Spec.Ports) > 0 {
		servicePort = int(svc.Spec.Ports[0].Port)
	}

	ctx, cancel := context.WithCancel(context.Background())

	// Parse session-ttl annotation if present
	sessionTTL := svc.Annotations["agentapi.proxy/session-ttl"]
	agentType := restoreAgentTypeFromService(svc)

	// Create session using constructor
	session := NewKubernetesSession(
		sessionID,
		&entities.RunServerRequest{
			UserID:         userID,
			Tags:           tags,
			Scope:          scope,
			TeamID:         teamID,
			InitialMessage: initialMessage,
			Teams:          teams,
			SessionTTL:     sessionTTL,
			AgentType:      agentType,
		},
		fmt.Sprintf("agentapi-session-%s", sessionID),
		svc.Name,
		fmt.Sprintf("agentapi-session-%s-pvc", sessionID),
		m.namespace,
		servicePort,
		cancel,
		nil, // No webhook payload for restored sessions
	)
	if sessionMeta != nil {
		restoredSettings := &sessionsettings.SessionSettings{Session: *sessionMeta}
		session.Request().ProvisionSettings = restoredSettings
		session.SetProvisionSettings(restoredSettings)
	}
	// Set restored values
	session.SetStartedAt(createdAt)
	session.SetUpdatedAt(updatedAt)
	session.SetLastMessageAt(lastMessageAt)
	status := m.getStatusFromWorkloadObject(deployment, pod)
	if svc.Annotations[sessionSuspendedAtAnnotation] != "" && deployment == nil && pod == nil {
		status = "suspended"
	}
	session.SetStatus(status)
	session.SetDescription(initialMessage) // Cache initial message as description
	session.SetAnnotations(sessionAnnotationsFromMap(svc.Annotations))

	// Register proxy-wide status change broadcaster
	session.statusChangeCallback = m.broadcastStatusChange

	// Add to memory map
	m.mutex.Lock()
	m.sessions[sessionID] = session
	m.mutex.Unlock()

	// Start watching workload health and agentapi runtime status.
	if status != "suspended" {
		m.startWorkloadStatusWatchers(ctx, session)
		go m.scheduleSuspendWhenRestoredWorkloadReady(session)
	}

	log.Printf("[K8S_SESSION] Restored session %s from Service (with pre-fetched workload)", sessionID)

	return session
}

// getStatusFromWorkloadObject determines session status from a pre-fetched workload object.
func (m *KubernetesSessionManager) getStatusFromWorkloadObject(deployment *appsv1.Deployment, pod *corev1.Pod) string {
	if !m.isPVCEnabled() {
		if pod == nil {
			return "stopped"
		}
		if isPodReady(pod) {
			return "active"
		}
		if pod.Status.Phase == corev1.PodFailed || pod.Status.Phase == corev1.PodSucceeded {
			return "stopped"
		}
		return "starting"
	}

	if deployment == nil {
		return "stopped"
	}

	if deployment.Status.ReadyReplicas > 0 {
		return "active"
	}
	if deployment.Status.Replicas > 0 {
		return "starting"
	}
	return "unhealthy"
}

// ensureOtelcolConfigMap creates or updates the OpenTelemetry Collector ConfigMap
