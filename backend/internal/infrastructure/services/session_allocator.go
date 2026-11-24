package services

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"strings"
	"time"

	sessionallocation "github.com/takutakahashi/agentapi-proxy/internal/core/sessionallocation"
	"github.com/takutakahashi/agentapi-proxy/internal/domain/entities"
	portrepos "github.com/takutakahashi/agentapi-proxy/internal/usecases/ports/repositories"
	"github.com/takutakahashi/agentapi-proxy/pkg/sessionsettings"
	"github.com/takutakahashi/agentapi-proxy/pkg/telemetry"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const (
	sessionAllocationSecretPrefix = "agentapi-session-allocation-"
	sessionAllocationDataKey      = "request.json"
)

// CreateSession creates a session by submitting a SessionAllocationRequest when
// the leader-elected allocator is enabled. Tests and non-server usage fall back
// to direct allocation when the allocator has not been started.
func (m *KubernetesSessionManager) CreateSession(ctx context.Context, id string, req *entities.RunServerRequest, webhookPayload []byte) (entities.Session, error) {
	return telemetry.Operation(ctx, "services.KubernetesSessionManager.CreateSession", func(ctx context.Context) (entities.Session, error) {
		return m.createSession(ctx, id, req, webhookPayload)
	}, telemetry.Bool("session.allocator_enabled", m.isSessionAllocatorEnabled()))
}

func (m *KubernetesSessionManager) createSession(ctx context.Context, id string, req *entities.RunServerRequest, webhookPayload []byte) (entities.Session, error) {
	cleanupRoute, err := m.prepareBuiltInDirectRuntime(ctx, id, req)
	if err != nil {
		return nil, err
	}
	created := false
	defer func() {
		if !created && cleanupRoute != nil {
			cleanupRoute()
		}
	}()
	var session entities.Session
	if !m.isSessionAllocatorEnabled() {
		session, err = m.allocateSessionDirect(ctx, id, req, webhookPayload)
	} else {
		session, err = m.submitSessionAllocation(ctx, id, req, webhookPayload)
	}
	created = err == nil
	return session, err
}

func (m *KubernetesSessionManager) prepareBuiltInDirectRuntime(ctx context.Context, id string, req *entities.RunServerRequest) (func(), error) {
	m.mutex.RLock()
	repo := m.sessionRouteRepo
	m.mutex.RUnlock()
	if repo == nil || req == nil || req.ParentRuntime != nil || (req.ProvisionSettings != nil && req.ProvisionSettings.ParentRuntime != nil) {
		return nil, nil
	}
	tokenBytes := make([]byte, 32)
	if _, err := rand.Read(tokenBytes); err != nil {
		return nil, fmt.Errorf("create built-in runtime credential: %w", err)
	}
	token := hex.EncodeToString(tokenBytes)
	digest := sha256.Sum256([]byte(token))
	endpoint := strings.TrimRight(m.k8sConfig.ProvisionerProxyURL, "/")
	if endpoint == "" {
		endpoint = fmt.Sprintf("http://control.%s.svc.cluster.local:8080", m.namespace)
	}
	runtime := &sessionsettings.ParentRuntimeConfig{
		Enabled: true, Endpoint: endpoint, SessionID: id, Token: token, Generation: 1,
	}
	now := time.Now().UTC()
	route := &portrepos.SessionRoute{
		SessionID: id, RemoteSessionID: id, Transport: portrepos.SessionRouteTransportDirectRuntime,
		RuntimeTokenHash: hex.EncodeToString(digest[:]), Generation: 1, UserID: req.UserID,
		Scope: string(req.Scope), TeamID: req.TeamID, Tags: req.Tags, StartedAt: now,
		InitialMessage: req.InitialMessage, Status: "creating", StatusUpdatedAt: now,
	}
	if err := repo.Save(ctx, route); err != nil {
		return nil, fmt.Errorf("save built-in direct runtime route: %w", err)
	}
	req.ParentRuntime = runtime
	if req.ProvisionSettings != nil {
		settingsCopy := *req.ProvisionSettings
		settingsCopy.ParentRuntime = runtime
		req.ProvisionSettings = &settingsCopy
	}
	return func() {
		cleanupCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := repo.Delete(cleanupCtx, id); err != nil {
			log.Printf("[SESSION_ROUTE] Warning: failed to clean up built-in route %s: %v", id, err)
		}
	}, nil
}

// CreateSessionDirect allocates a session on this manager without submitting it
// to the cluster-wide allocator. External session manager workers use this to
// ensure the remote session ID they report matches the concrete local session.
func (m *KubernetesSessionManager) CreateSessionDirect(ctx context.Context, id string, req *entities.RunServerRequest, webhookPayload []byte) (entities.Session, error) {
	return telemetry.Operation(ctx, "services.KubernetesSessionManager.CreateSessionDirect", func(ctx context.Context) (entities.Session, error) {
		return m.allocateSessionDirect(ctx, id, req, webhookPayload)
	})
}

func (m *KubernetesSessionManager) submitSessionAllocation(ctx context.Context, id string, req *entities.RunServerRequest, webhookPayload []byte) (entities.Session, error) {
	return telemetry.Operation(ctx, "services.KubernetesSessionManager.submitSessionAllocation", func(ctx context.Context) (entities.Session, error) {
		return m.submitSessionAllocationRequest(ctx, id, req, webhookPayload)
	})
}

func (m *KubernetesSessionManager) submitSessionAllocationRequest(ctx context.Context, id string, req *entities.RunServerRequest, webhookPayload []byte) (entities.Session, error) {
	allocation := &sessionallocation.AllocationRequest{
		SessionID:      id,
		Request:        req,
		WebhookPayload: webhookPayload,
		Status:         sessionallocation.StatusPending,
		Requirements:   sessionRequirements(req),
		UpdatedAt:      time.Now().UTC(),
	}
	if err := m.saveSessionAllocation(ctx, allocation); err != nil {
		return nil, fmt.Errorf("failed to submit session allocation: %w", err)
	}
	if err := m.notifySessionAllocation(ctx); err != nil {
		log.Printf("[SESSION_ALLOCATOR] Warning: failed to notify allocation request %s: %v", id, err)
	}

	return entities.NewProxySessionWithStatus(id, req.UserID, req.Scope, req.TeamID, req.Tags, allocation.UpdatedAt, "creating"), nil
}

func (m *KubernetesSessionManager) isSessionAllocatorEnabled() bool {
	m.mutex.RLock()
	defer m.mutex.RUnlock()
	return m.sessionAllocatorEnabled
}

func (m *KubernetesSessionManager) setSessionAllocatorEnabled(enabled bool) {
	m.mutex.Lock()
	defer m.mutex.Unlock()
	m.sessionAllocatorEnabled = enabled
}

func (m *KubernetesSessionManager) SetSessionAllocatorEnabled(enabled bool) {
	m.setSessionAllocatorEnabled(enabled)
}

func (m *KubernetesSessionManager) SetSessionAllocationNotifier(notifier sessionallocation.Notifier) {
	if notifier == nil {
		return
	}
	m.mutex.Lock()
	defer m.mutex.Unlock()
	m.sessionAllocationNotifier = notifier
}

func sessionRequirements(req *entities.RunServerRequest) sessionallocation.Requirements {
	return sessionallocation.RequirementsFromRunServerRequest(req)
}

func sessionAllocationSecretName(sessionID string) string {
	return sessionAllocationSecretPrefix + sessionID
}

func (m *KubernetesSessionManager) getSessionAllocation(ctx context.Context, sessionID string) (*sessionallocation.AllocationRequest, error) {
	sec, err := m.client.CoreV1().Secrets(m.namespace).Get(ctx, sessionAllocationSecretName(sessionID), metav1.GetOptions{})
	if err != nil {
		return nil, err
	}
	data := sec.Data[sessionAllocationDataKey]
	if len(data) == 0 {
		return nil, fmt.Errorf("session allocation Secret %s has no %s", sec.Name, sessionAllocationDataKey)
	}
	var req sessionallocation.AllocationRequest
	if err := json.Unmarshal(data, &req); err != nil {
		return nil, fmt.Errorf("decode session allocation: %w", err)
	}
	return &req, nil
}

// GetSessionAllocation returns the durable allocation request. Allocator
// clients use it to persist dependent state before completion deletes the
// allocation Secret.
func (m *KubernetesSessionManager) GetSessionAllocation(ctx context.Context, sessionID string) (*sessionallocation.AllocationRequest, error) {
	return m.getSessionAllocation(ctx, sessionID)
}

func (m *KubernetesSessionManager) saveSessionAllocation(ctx context.Context, req *sessionallocation.AllocationRequest) error {
	req.UpdatedAt = time.Now().UTC()
	data, err := json.Marshal(req)
	if err != nil {
		return fmt.Errorf("encode session allocation: %w", err)
	}
	name := sessionAllocationSecretName(req.SessionID)
	labels := map[string]string{
		"app.kubernetes.io/managed-by":             "agentapi-proxy",
		"agentapi.proxy/session-id":                req.SessionID,
		"agentapi.proxy/session-allocation":        "true",
		"agentapi.proxy/requirement-sandbox":       fmt.Sprintf("%t", req.Requirements.Sandbox),
		"agentapi.proxy/requirement-dind":          fmt.Sprintf("%t", req.Requirements.DinD),
		"agentapi.proxy/session-allocation-status": string(req.Status),
	}
	if req.Request != nil {
		labels["agentapi.proxy/user-id"] = sanitizeLabelValue(req.Request.UserID)
		scope := req.Request.Scope
		if scope == "" {
			scope = entities.ScopeUser
		}
		labels["agentapi.proxy/scope"] = string(scope)
		if req.Request.TeamID != "" {
			labels["agentapi.proxy/team-id-hash"] = hashTeamID(req.Request.TeamID)
		}
		for key, value := range req.Request.Tags {
			labels["agentapi.proxy/tag-"+sanitizeLabelKey(key)] = sanitizeLabelValue(value)
		}
	}
	if req.ManagerID != "" {
		labels["agentapi.proxy/external-session-manager-id"] = "true"
	}
	if req.Requirements.AgentType != "" {
		labels["agentapi.proxy/agent-type"] = req.Requirements.AgentType
	}

	sec, err := m.client.CoreV1().Secrets(m.namespace).Get(ctx, name, metav1.GetOptions{})
	if apierrors.IsNotFound(err) {
		sec = &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name:      name,
				Namespace: m.namespace,
				Labels:    labels,
			},
			Type: corev1.SecretTypeOpaque,
			Data: map[string][]byte{sessionAllocationDataKey: data},
		}
		created, createErr := m.client.CoreV1().Secrets(m.namespace).Create(ctx, sec, metav1.CreateOptions{})
		err = createErr
		if err == nil {
			m.updateAllocationInformer(created, req.Status)
			m.invalidateSessionAllocationListCache()
			m.invalidateSessionListCache("session allocation save")
		}
		return err
	}
	if err != nil {
		return err
	}
	sec.Labels = labels
	if sec.Data == nil {
		sec.Data = make(map[string][]byte)
	}
	sec.Data[sessionAllocationDataKey] = data
	updated, updateErr := m.client.CoreV1().Secrets(m.namespace).Update(ctx, sec, metav1.UpdateOptions{})
	err = updateErr
	if err == nil {
		m.updateAllocationInformer(updated, req.Status)
		m.invalidateSessionAllocationListCache()
		m.invalidateSessionListCache("session allocation save")
	}
	return err
}

func (m *KubernetesSessionManager) deleteSessionAllocation(ctx context.Context, sessionID string) error {
	err := m.client.CoreV1().Secrets(m.namespace).Delete(ctx, sessionAllocationSecretName(sessionID), metav1.DeleteOptions{})
	if apierrors.IsNotFound(err) {
		return nil
	}
	if err == nil {
		m.deleteAllocationFromInformer(sessionID)
		m.invalidateSessionAllocationListCache()
		m.invalidateSessionListCache("session allocation delete")
	}
	return err
}

func (m *KubernetesSessionManager) updateAllocationInformer(secret *corev1.Secret, status sessionallocation.Status) {
	if m.allocationInformer == nil || secret == nil {
		return
	}
	switch status {
	case sessionallocation.StatusPending, sessionallocation.StatusAllocating, sessionallocation.StatusError:
		_ = m.allocationInformer.GetStore().Update(secret.DeepCopy())
	default:
		_ = m.allocationInformer.GetStore().Delete(secret)
	}
}

func (m *KubernetesSessionManager) deleteAllocationFromInformer(sessionID string) {
	if m.allocationInformer == nil {
		return
	}
	_ = m.allocationInformer.GetStore().Delete(&corev1.Secret{ObjectMeta: metav1.ObjectMeta{
		Name:      sessionAllocationSecretName(sessionID),
		Namespace: m.namespace,
	}})
}

// DeletePendingSessionAllocation removes an allocation that has not produced a
// session yet. Allocating requests can otherwise become permanently orphaned
// when their selected manager disappears after claiming them.
func (m *KubernetesSessionManager) DeletePendingSessionAllocation(ctx context.Context, sessionID string) (bool, error) {
	allocation, err := m.getSessionAllocation(ctx, sessionID)
	if apierrors.IsNotFound(err) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if allocation.Status != sessionallocation.StatusPending && allocation.Status != sessionallocation.StatusAllocating && allocation.Status != sessionallocation.StatusError {
		return false, nil
	}
	if err := m.deleteSessionAllocation(ctx, sessionID); err != nil {
		return false, err
	}
	if err := m.notifySessionAllocation(ctx); err != nil {
		log.Printf("[SESSION_ALLOCATOR] Warning: failed to notify after deleting allocation request %s: %v", sessionID, err)
	}
	return true, nil
}

func (m *KubernetesSessionManager) NextSessionAllocation(ctx context.Context, wait time.Duration) (*sessionallocation.AllocationRequest, bool, error) {
	deadline := time.Now().Add(wait)
	for {
		req, ok, err := m.claimNextSessionAllocation(ctx)
		if err != nil || ok || wait <= 0 || time.Now().After(deadline) {
			return req, ok, err
		}
		updates, cancel, err := m.subscribeSessionAllocation(ctx)
		if err != nil {
			return nil, false, err
		}
		req, ok, err = m.claimNextSessionAllocation(ctx)
		if err != nil || ok {
			cancel()
			return req, ok, err
		}
		remaining := time.Until(deadline)
		if remaining <= 0 {
			cancel()
			return nil, false, nil
		}
		timer := time.NewTimer(remaining)
		select {
		case <-ctx.Done():
			timer.Stop()
			cancel()
			return nil, false, ctx.Err()
		case <-timer.C:
			cancel()
			return nil, false, nil
		case <-updates:
			timer.Stop()
			cancel()
		}
	}
}

func (m *KubernetesSessionManager) claimNextSessionAllocation(ctx context.Context) (*sessionallocation.AllocationRequest, bool, error) {
	secrets, err := m.client.CoreV1().Secrets(m.namespace).List(ctx, metav1.ListOptions{
		LabelSelector: "agentapi.proxy/session-allocation=true,!agentapi.proxy/external-session-manager-id,agentapi.proxy/session-allocation-status in (pending,allocating)",
	})
	if err != nil {
		return nil, false, fmt.Errorf("list session allocations: %w", err)
	}
	for i := range secrets.Items {
		sec := &secrets.Items[i]
		sessionID := sec.Labels["agentapi.proxy/session-id"]
		if sessionID == "" {
			continue
		}
		req, err := m.getSessionAllocation(ctx, sessionID)
		if err != nil {
			log.Printf("[SESSION_ALLOCATOR] Failed to read allocation %s: %v", sec.Name, err)
			continue
		}
		req.Status = sessionallocation.StatusAllocating
		if err := m.saveSessionAllocation(ctx, req); err != nil {
			log.Printf("[SESSION_ALLOCATOR] Failed to claim allocation %s: %v", req.SessionID, err)
			continue
		}
		return req, true, nil
	}
	return nil, false, nil
}
