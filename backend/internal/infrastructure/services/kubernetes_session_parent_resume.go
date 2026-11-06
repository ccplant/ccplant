package services

import (
	"context"
	"fmt"
	"strings"

	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/takutakahashi/agentapi-proxy/pkg/sessionsettings"
)

func (m *KubernetesSessionManager) SessionWorkloadSuspended(ctx context.Context, id string) (bool, error) {
	session, ok := m.GetSession(id).(*KubernetesSession)
	if !ok || session == nil {
		return false, fmt.Errorf("session %s not found", id)
	}
	svc, err := m.client.CoreV1().Services(m.namespace).Get(ctx, session.ServiceName(), metav1.GetOptions{})
	if err != nil {
		return false, fmt.Errorf("get canonical service: %w", err)
	}
	if svc.Annotations[sessionSuspendedAtAnnotation] != "" {
		return true, nil
	}
	exists, err := m.sessionWorkloadExists(ctx, session)
	if err != nil {
		return false, err
	}
	return !exists, nil
}

func (m *KubernetesSessionManager) PrepareParentSessionResume(ctx context.Context, id string, settings *sessionsettings.SessionSettings) error {
	session, ok := m.GetSession(id).(*KubernetesSession)
	if !ok || session == nil {
		return fmt.Errorf("session %s not found", id)
	}
	exists, err := m.sessionWorkloadExists(ctx, session)
	if err != nil {
		return err
	}
	if exists {
		return nil
	}
	next := *settings
	if m.config.SessionPersistence.Backend != "" {
		snapshotID, err := m.ownSessionSnapshotID(ctx, id, &next)
		if err != nil {
			return err
		}
		next.Session.ResumeFrom = snapshotID
	}
	return m.PrepareSessionResume(ctx, id, &next)
}

func (m *KubernetesSessionManager) ownSessionSnapshotID(ctx context.Context, id string, settings *sessionsettings.SessionSettings) (string, error) {
	if snapshotID := strings.TrimSpace(settings.Session.ID); snapshotID != "" {
		return snapshotID, nil
	}
	current, err := m.CurrentSessionSettings(ctx, id)
	if err != nil {
		return "", fmt.Errorf("load session snapshot ID: %w", err)
	}
	if snapshotID := strings.TrimSpace(current.Session.ID); snapshotID != "" {
		return snapshotID, nil
	}
	return "", fmt.Errorf("session %s has no snapshot ID", id)
}

func (m *KubernetesSessionManager) sessionWorkloadExists(ctx context.Context, session *KubernetesSession) (bool, error) {
	var err error
	if m.isPVCEnabled() {
		_, err = m.client.AppsV1().Deployments(m.namespace).Get(ctx, session.DeploymentName(), metav1.GetOptions{})
	} else {
		_, err = m.client.CoreV1().Pods(m.namespace).Get(ctx, session.DeploymentName(), metav1.GetOptions{})
	}
	if errors.IsNotFound(err) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("get session workload: %w", err)
	}
	return true, nil
}
