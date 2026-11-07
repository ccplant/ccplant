package services

import (
	"context"
	"fmt"
	"strings"

	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/yaml"

	"github.com/takutakahashi/agentapi-proxy/internal/domain/entities"
	"github.com/takutakahashi/agentapi-proxy/pkg/sessionsettings"
)

type parentResumeContextKey struct{}

var errSessionSettingsUnavailable = fmt.Errorf("session settings unavailable")

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

func (m *KubernetesSessionManager) ResumeParentSession(ctx context.Context, id string, settings *sessionsettings.SessionSettings) (entities.Session, bool, error) {
	if settings != nil {
		if err := m.prepareParentSessionResume(ctx, id, settings); err != nil {
			return nil, false, err
		}
	}
	return m.EnsureSessionWorkload(context.WithValue(ctx, parentResumeContextKey{}, true), id)
}

func (m *KubernetesSessionManager) prepareParentSessionResume(ctx context.Context, id string, settings *sessionsettings.SessionSettings) error {
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
		snapshotID, saved, err := m.savedSessionSnapshotID(ctx, session)
		if err != nil {
			return err
		}
		if saved {
			next.Session.ResumeFrom = snapshotID
		}
	}
	return m.PrepareSessionResume(ctx, id, &next)
}

func (m *KubernetesSessionManager) savedSessionSnapshotID(ctx context.Context, session *KubernetesSession) (string, bool, error) {
	name := strings.TrimSuffix(session.ServiceName(), "-svc") + "-settings"
	secret, err := m.client.CoreV1().Secrets(m.namespace).Get(ctx, name, metav1.GetOptions{})
	if errors.IsNotFound(err) {
		return "", false, nil
	}
	if err != nil {
		return "", false, fmt.Errorf("get session settings secret: %w", err)
	}
	if len(secret.Data["settings.yaml"]) == 0 {
		return "", false, nil
	}
	var current sessionsettings.SessionSettings
	if err := yaml.Unmarshal(secret.Data["settings.yaml"], &current); err != nil {
		return "", false, fmt.Errorf("invalid persisted session settings: %w", err)
	}
	snapshotID := strings.TrimSpace(current.Session.ID)
	return snapshotID, snapshotID != "" && strings.TrimSpace(current.Session.ResumeFrom) == snapshotID, nil
}

func (m *KubernetesSessionManager) persistSuspendSettings(ctx context.Context, id string, settings *sessionsettings.SessionSettings, saved bool) error {
	current, currentErr := m.CurrentSessionSettings(ctx, id)
	if settings == nil {
		if !saved || currentErr == errSessionSettingsUnavailable {
			return nil
		}
		if currentErr != nil {
			return currentErr
		}
		settings = current
	}
	next := *settings
	switch {
	case saved:
		snapshotID, err := m.ownSessionSnapshotID(ctx, id, &next)
		if err != nil {
			return err
		}
		next.Session.ResumeFrom = snapshotID
	case currentErr == nil:
		next.Session.ResumeFrom = current.Session.ResumeFrom
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
