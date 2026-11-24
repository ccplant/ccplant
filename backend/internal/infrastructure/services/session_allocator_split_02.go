package services

import (
	"context"
	"fmt"
	"log"
	"strings"
	"time"

	sessionallocation "github.com/takutakahashi/agentapi-proxy/internal/core/sessionallocation"
	"github.com/takutakahashi/agentapi-proxy/internal/domain/entities"
	"github.com/takutakahashi/agentapi-proxy/pkg/sessionsettings"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func (m *KubernetesSessionManager) SubmitExternalSessionAllocation(ctx context.Context, managerID, sessionID string, settings *sessionsettings.SessionSettings, req *entities.RunServerRequest, runtime *sessionallocation.RuntimeBootstrap) error {
	if err := m.CreateProvisionRequestFromSettings(ctx, sessionID, settings); err != nil {
		return fmt.Errorf("failed to create external provision request: %w", err)
	}
	allocation := &sessionallocation.AllocationRequest{
		SessionID:         sessionID,
		ManagerID:         managerID,
		Request:           req,
		ProvisionSettings: settings,
		Status:            sessionallocation.StatusPending,
		Requirements:      sessionRequirements(req),
		UpdatedAt:         time.Now().UTC(),
		Runtime:           runtime,
		RuntimeProfile:    m.ExternalRuntimeProfile(),
	}
	if err := m.saveSessionAllocation(ctx, allocation); err != nil {
		_ = m.deleteProvisionRequest(context.Background(), sessionID)
		return fmt.Errorf("failed to submit external session allocation: %w", err)
	}
	if err := m.notifySessionAllocation(ctx); err != nil {
		log.Printf("[EXTERNAL_SESSION_ALLOCATOR] Warning: failed to notify allocation request %s: %v", sessionID, err)
	}
	return nil
}

func (m *KubernetesSessionManager) NextExternalSessionAllocation(ctx context.Context, managerID string, wait time.Duration) (*sessionallocation.AllocationRequest, bool, error) {
	deadline := time.Now().Add(wait)
	for {
		req, ok, err := m.claimNextExternalSessionAllocation(ctx, managerID)
		if err != nil || ok || wait <= 0 || time.Now().After(deadline) {
			return req, ok, err
		}
		updates, cancel, err := m.subscribeSessionAllocation(ctx)
		if err != nil {
			return nil, false, err
		}
		req, ok, err = m.claimNextExternalSessionAllocation(ctx, managerID)
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

func (m *KubernetesSessionManager) claimNextExternalSessionAllocation(ctx context.Context, managerID string) (*sessionallocation.AllocationRequest, bool, error) {
	if managerID == "" {
		return nil, false, fmt.Errorf("managerID is required")
	}
	secrets, err := m.client.CoreV1().Secrets(m.namespace).List(ctx, metav1.ListOptions{
		LabelSelector: "agentapi.proxy/session-allocation=true,agentapi.proxy/external-session-manager-id,agentapi.proxy/session-allocation-status in (pending,allocating)",
	})
	if err != nil {
		return nil, false, fmt.Errorf("list external session allocations: %w", err)
	}
	for i := range secrets.Items {
		sec := &secrets.Items[i]
		sessionID := sec.Labels["agentapi.proxy/session-id"]
		if sessionID == "" {
			continue
		}
		req, err := m.getSessionAllocation(ctx, sessionID)
		if err != nil {
			log.Printf("[EXTERNAL_SESSION_ALLOCATOR] Failed to read allocation %s: %v", sec.Name, err)
			continue
		}
		if req.ManagerID != managerID {
			continue
		}
		req.Status = sessionallocation.StatusAllocating
		if err := m.saveSessionAllocation(ctx, req); err != nil {
			log.Printf("[EXTERNAL_SESSION_ALLOCATOR] Failed to claim allocation %s: %v", req.SessionID, err)
			continue
		}
		return req, true, nil
	}
	return nil, false, nil
}

func (m *KubernetesSessionManager) CompleteExternalSessionAllocation(ctx context.Context, sessionID string, result sessionallocation.AllocationResult) (*sessionallocation.AllocationRequest, error) {
	req, err := m.getSessionAllocation(ctx, sessionID)
	if err != nil {
		return nil, err
	}
	req.Status = result.Status
	req.Message = result.Message
	req.AllocatedSessionID = result.AllocatedSessionID
	if err := m.saveSessionAllocation(ctx, req); err != nil {
		return nil, err
	}
	if err := m.notifySessionAllocation(ctx); err != nil {
		return nil, err
	}
	if result.Status != sessionallocation.StatusError {
		if err := m.deleteSessionAllocation(context.Background(), sessionID); err != nil {
			log.Printf("[EXTERNAL_SESSION_ALLOCATOR] Warning: failed to delete completed allocation %s: %v", sessionID, err)
		}
	}
	return req, nil
}

func (m *KubernetesSessionManager) CompleteSessionAllocation(ctx context.Context, sessionID string, result sessionallocation.AllocationResult) (*sessionallocation.AllocationRequest, error) {
	req, err := m.getSessionAllocation(ctx, sessionID)
	if err != nil {
		return nil, err
	}
	req.Status = result.Status
	req.Message = result.Message
	req.AllocatedSessionID = result.AllocatedSessionID
	if err := m.saveSessionAllocation(ctx, req); err != nil {
		return nil, err
	}
	if err := m.notifySessionAllocation(ctx); err != nil {
		return nil, err
	}
	if result.Status != sessionallocation.StatusError {
		if err := m.deleteSessionAllocation(context.Background(), sessionID); err != nil {
			log.Printf("[SESSION_ALLOCATOR] Warning: failed to delete completed allocation %s: %v", sessionID, err)
		}
	}
	return req, nil
}

func (m *KubernetesSessionManager) notifySessionAllocation(ctx context.Context) error {
	m.mutex.RLock()
	notifier := m.sessionAllocationNotifier
	m.mutex.RUnlock()
	if notifier == nil {
		return nil
	}
	return notifier.Notify(ctx)
}

func (m *KubernetesSessionManager) subscribeSessionAllocation(ctx context.Context) (<-chan struct{}, func(), error) {
	m.mutex.RLock()
	notifier := m.sessionAllocationNotifier
	m.mutex.RUnlock()
	if notifier == nil {
		ch := make(chan struct{})
		return ch, func() { close(ch) }, nil
	}
	return notifier.Subscribe(ctx)
}

func (m *KubernetesSessionManager) allocationProxyURL() string {
	proxyURL := strings.TrimRight(m.k8sConfig.ProvisionerProxyURL, "/")
	if proxyURL != "" {
		return proxyURL
	}
	return fmt.Sprintf("http://control.%s.svc.cluster.local:8080", m.namespace)
}

func (m *KubernetesSessionManager) AllocationProxyURL() string {
	return m.allocationProxyURL()
}
