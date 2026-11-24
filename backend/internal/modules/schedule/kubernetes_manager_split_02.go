package schedule

import (
	"context"
	"encoding/json"
	"fmt"
	"log"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/takutakahashi/agentapi-proxy/internal/core/kubelabel"
)

func (m *KubernetesManager) loadSchedule(ctx context.Context, id string) (*Schedule, error) {
	secretName := scheduleSecretName(id)
	secret, err := m.client.CoreV1().Secrets(m.namespace).Get(ctx, secretName, metav1.GetOptions{})
	if err != nil {
		if errors.IsNotFound(err) {
			return nil, ErrScheduleNotFound{ID: id}
		}
		return nil, fmt.Errorf("failed to get schedule secret: %w", err)
	}

	data, ok := secret.Data[SecretKeySchedule]
	if !ok {
		return nil, fmt.Errorf("schedule secret missing data key: %s", SecretKeySchedule)
	}

	var schedule Schedule
	if err := json.Unmarshal(data, &schedule); err != nil {
		return nil, fmt.Errorf("failed to unmarshal schedule: %w", err)
	}

	return &schedule, nil
}

// loadAllSchedules loads all schedules from Kubernetes Secrets using label selector
func (m *KubernetesManager) loadAllSchedules(ctx context.Context) ([]*Schedule, error) {
	labelSelector := fmt.Sprintf("%s=true", LabelSchedule)
	secrets, err := m.client.CoreV1().Secrets(m.namespace).List(ctx, metav1.ListOptions{
		LabelSelector: labelSelector,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to list schedule secrets: %w", err)
	}

	result := make([]*Schedule, 0, len(secrets.Items))
	for _, secret := range secrets.Items {
		// Skip legacy secret
		if secret.Name == LegacyScheduleSecretName {
			continue
		}

		data, ok := secret.Data[SecretKeySchedule]
		if !ok {
			continue
		}

		var schedule Schedule
		if err := json.Unmarshal(data, &schedule); err != nil {
			continue
		}

		// Prefer team_id from annotation over JSON data
		// Use the annotation value as-is (should be in slash format: org/team-slug)
		if annotationTeamID, ok := secret.Annotations[AnnotationScheduleTeamID]; ok && annotationTeamID != "" {
			schedule.TeamID = annotationTeamID
		}

		result = append(result, &schedule)
	}

	return result, nil
}

// saveSchedule saves a schedule to its own Kubernetes Secret
func (m *KubernetesManager) saveSchedule(ctx context.Context, schedule *Schedule) error {
	data, err := json.Marshal(schedule)
	if err != nil {
		return fmt.Errorf("failed to marshal schedule: %w", err)
	}

	secretName := scheduleSecretName(schedule.ID)
	labels := map[string]string{
		LabelSchedule:       "true",
		LabelScheduleID:     schedule.ID,
		LabelScheduleScope:  string(schedule.GetScope()),
		LabelScheduleUserID: kubelabel.HashValue(schedule.UserID),
	}
	annotations := make(map[string]string)
	if schedule.TeamID != "" {
		// Use hash for label to avoid issues with "/" in team IDs
		labels[LabelScheduleTeamID] = kubelabel.HashValue(schedule.TeamID)
		// Store original team_id in annotation for restoration
		annotations[AnnotationScheduleTeamID] = schedule.TeamID
	}

	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:        secretName,
			Namespace:   m.namespace,
			Labels:      labels,
			Annotations: annotations,
		},
		Type: corev1.SecretTypeOpaque,
		Data: map[string][]byte{
			SecretKeySchedule: data,
		},
	}

	// Try to create first
	_, err = m.client.CoreV1().Secrets(m.namespace).Create(ctx, secret, metav1.CreateOptions{})
	if err != nil {
		if errors.IsAlreadyExists(err) {
			existing, getErr := m.client.CoreV1().Secrets(m.namespace).Get(ctx, secretName, metav1.GetOptions{})
			if getErr != nil {
				return fmt.Errorf("failed to get existing secret: %w", getErr)
			}

			existing.Data[SecretKeySchedule] = data
			// Ensure labels and annotations are set
			existing.Labels = labels
			existing.Annotations = annotations

			_, err = m.client.CoreV1().Secrets(m.namespace).Update(ctx, existing, metav1.UpdateOptions{})
			if err != nil {
				return fmt.Errorf("failed to update schedule secret: %w", err)
			}
			return nil
		}
		return fmt.Errorf("failed to create schedule secret: %w", err)
	}

	return nil
}

// deleteScheduleSecret deletes a schedule's Kubernetes Secret
func (m *KubernetesManager) deleteScheduleSecret(ctx context.Context, id string) error {
	secretName := scheduleSecretName(id)
	err := m.client.CoreV1().Secrets(m.namespace).Delete(ctx, secretName, metav1.DeleteOptions{})
	if err != nil {
		if errors.IsNotFound(err) {
			return ErrScheduleNotFound{ID: id}
		}
		return fmt.Errorf("failed to delete schedule secret: %w", err)
	}
	return nil
}

// MigrateFromLegacy migrates schedules from the legacy single-Secret format
// to individual Secrets per schedule. This is idempotent - schedules that
// already exist as individual Secrets are skipped. The legacy Secret is
// preserved after migration for backup purposes.
func (m *KubernetesManager) MigrateFromLegacy(ctx context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	// Check if legacy Secret exists
	legacySecret, err := m.client.CoreV1().Secrets(m.namespace).Get(ctx, LegacyScheduleSecretName, metav1.GetOptions{})
	if err != nil {
		if errors.IsNotFound(err) {
			log.Printf("[SCHEDULE_MIGRATION] No legacy secret found, skipping migration")
			return nil
		}
		return fmt.Errorf("failed to get legacy secret: %w", err)
	}

	// Parse legacy schedules
	data, ok := legacySecret.Data[LegacySecretKeySchedules]
	if !ok {
		log.Printf("[SCHEDULE_MIGRATION] Legacy secret has no schedules data, skipping migration")
		return nil
	}

	var sd schedulesData
	if err := json.Unmarshal(data, &sd); err != nil {
		return fmt.Errorf("failed to unmarshal legacy schedules: %w", err)
	}

	if len(sd.Schedules) == 0 {
		log.Printf("[SCHEDULE_MIGRATION] No schedules in legacy secret, skipping migration")
		return nil
	}

	log.Printf("[SCHEDULE_MIGRATION] Found %d schedules in legacy secret, starting migration", len(sd.Schedules))

	migratedCount := 0
	skippedCount := 0
	errorCount := 0

	for _, schedule := range sd.Schedules {
		// Check if individual Secret already exists
		secretName := scheduleSecretName(schedule.ID)
		_, err := m.client.CoreV1().Secrets(m.namespace).Get(ctx, secretName, metav1.GetOptions{})
		if err == nil {
			// Schedule already migrated
			log.Printf("[SCHEDULE_MIGRATION] Schedule %s already exists, skipping", schedule.ID)
			skippedCount++
			continue
		}
		if !errors.IsNotFound(err) {
			log.Printf("[SCHEDULE_MIGRATION] Error checking schedule %s: %v, continuing", schedule.ID, err)
			errorCount++
			continue
		}

		// Migrate schedule to individual Secret
		if err := m.saveSchedule(ctx, schedule); err != nil {
			log.Printf("[SCHEDULE_MIGRATION] Failed to migrate schedule %s: %v, continuing", schedule.ID, err)
			errorCount++
			continue
		}

		log.Printf("[SCHEDULE_MIGRATION] Migrated schedule %s (%s)", schedule.ID, schedule.Name)
		migratedCount++
	}

	log.Printf("[SCHEDULE_MIGRATION] Migration complete: %d migrated, %d skipped, %d errors",
		migratedCount, skippedCount, errorCount)

	return nil
}
