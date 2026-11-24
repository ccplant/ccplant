package schedule

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"

	"github.com/takutakahashi/agentapi-proxy/internal/domain/entities"
)

const (
	// MaxExecutionHistory bounds persisted schedule history so long-running
	// recurring schedules do not grow their backing Secret indefinitely.
	MaxExecutionHistory = 20

	// LabelSchedule is the label key for schedule resources
	LabelSchedule = "agentapi.proxy/schedule"
	// LabelScheduleID is the label key for schedule ID
	LabelScheduleID = "agentapi.proxy/schedule-id"
	// LabelScheduleScope is the label key for schedule scope (user or team)
	LabelScheduleScope = "agentapi.proxy/schedule-scope"
	// LabelScheduleUserID is the label key for schedule user ID
	LabelScheduleUserID = "agentapi.proxy/schedule-user-id"
	// LabelScheduleTeamID is the label key for schedule team ID (hashed)
	LabelScheduleTeamID = "agentapi.proxy/schedule-team-id"
	// AnnotationScheduleTeamID is the annotation key for original schedule team ID
	AnnotationScheduleTeamID = "agentapi.proxy/schedule-team-id"
	// SecretKeySchedule is the key in the Secret data for single schedule JSON
	SecretKeySchedule = "schedule.json"
	// ScheduleSecretPrefix is the prefix for schedule Secret names
	ScheduleSecretPrefix = "agentapi-schedule-"
	// LegacyScheduleSecretName is the name of the legacy Secret containing all schedules
	LegacyScheduleSecretName = "agentapi-schedules"
	// LegacySecretKeySchedules is the key in the legacy Secret data for schedules JSON
	LegacySecretKeySchedules = "schedules.json"
)

// schedulesData is the JSON structure stored in the legacy Secret
type schedulesData struct {
	Schedules []*Schedule `json:"schedules"`
}

// scheduleSecretName returns the Secret name for a given schedule ID
func scheduleSecretName(id string) string {
	return ScheduleSecretPrefix + id
}

// KubernetesManager implements Manager using Kubernetes Secrets
type KubernetesManager struct {
	client    kubernetes.Interface
	namespace string
	mu        sync.RWMutex
	clock     Clock
	ids       IDGenerator
}

// NewKubernetesManager creates a new KubernetesManager
func NewKubernetesManager(client kubernetes.Interface, namespace string) *KubernetesManager {
	return &KubernetesManager{
		client:    client,
		namespace: namespace,
		clock:     realClock{},
		ids:       uuidGenerator{},
	}
}

// WithRuntime replaces time and ID sources used while persisting and claiming
// schedules. Production callers use the defaults from NewKubernetesManager.
func (m *KubernetesManager) WithRuntime(clock Clock, ids IDGenerator) *KubernetesManager {
	if clock != nil {
		m.clock = clock
	}
	if ids != nil {
		m.ids = ids
	}
	return m
}

// Create creates a new schedule
func (m *KubernetesManager) Create(ctx context.Context, schedule *Schedule) error {
	if err := schedule.Validate(); err != nil {
		return err
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	// Check if schedule already exists
	secretName := scheduleSecretName(schedule.ID)
	_, err := m.client.CoreV1().Secrets(m.namespace).Get(ctx, secretName, metav1.GetOptions{})
	if err == nil {
		return fmt.Errorf("schedule already exists: %s", schedule.ID)
	}
	if !errors.IsNotFound(err) {
		return fmt.Errorf("failed to check schedule existence: %w", err)
	}

	now := m.clock.Now()
	schedule.CreatedAt = now
	schedule.UpdatedAt = now

	if err := m.saveSchedule(ctx, schedule); err != nil {
		return fmt.Errorf("failed to save schedule: %w", err)
	}

	return nil
}

// Get retrieves a schedule by ID
func (m *KubernetesManager) Get(ctx context.Context, id string) (*Schedule, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	return m.loadSchedule(ctx, id)
}

// List retrieves schedules matching the filter
func (m *KubernetesManager) List(ctx context.Context, filter ScheduleFilter) ([]*Schedule, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	schedules, err := m.loadAllSchedules(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to load schedules: %w", err)
	}

	var result []*Schedule
	for _, s := range schedules {
		if filter.UserID != "" && s.UserID != filter.UserID {
			continue
		}
		if filter.Status != "" && s.Status != filter.Status {
			continue
		}
		// Scope filter (use GetScope() to handle default value)
		if filter.Scope != "" && s.GetScope() != filter.Scope {
			continue
		}
		// TeamID filter
		if filter.TeamID != "" && s.TeamID != filter.TeamID {
			continue
		}
		// TeamIDs filter (for team-scoped schedules, check if schedule's team is in user's teams)
		if len(filter.TeamIDs) > 0 && s.GetScope() == entities.ScopeTeam {
			teamMatch := false
			for _, teamID := range filter.TeamIDs {
				if s.TeamID == teamID {
					teamMatch = true
					break
				}
			}
			if !teamMatch {
				continue
			}
		}
		result = append(result, s)
	}

	return result, nil
}

// Update updates an existing schedule
func (m *KubernetesManager) Update(ctx context.Context, schedule *Schedule) error {
	if err := schedule.Validate(); err != nil {
		return err
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	// Check if schedule exists
	secretName := scheduleSecretName(schedule.ID)
	_, err := m.client.CoreV1().Secrets(m.namespace).Get(ctx, secretName, metav1.GetOptions{})
	if err != nil {
		if errors.IsNotFound(err) {
			return ErrScheduleNotFound{ID: schedule.ID}
		}
		return fmt.Errorf("failed to get schedule: %w", err)
	}

	schedule.UpdatedAt = m.clock.Now()

	if err := m.saveSchedule(ctx, schedule); err != nil {
		return fmt.Errorf("failed to save schedule: %w", err)
	}

	return nil
}

// Delete removes a schedule by ID
func (m *KubernetesManager) Delete(ctx context.Context, id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	return m.deleteScheduleSecret(ctx, id)
}

// GetDueSchedules returns schedules that are due for execution
func (m *KubernetesManager) GetDueSchedules(ctx context.Context, now time.Time) ([]*Schedule, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	schedules, err := m.loadAllSchedules(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to load schedules: %w", err)
	}

	var due []*Schedule
	for _, s := range schedules {
		if s.IsDue(now) {
			due = append(due, s)
		}
	}

	return due, nil
}

func (m *KubernetesManager) ClaimDueSchedules(ctx context.Context, now time.Time, lease time.Duration) ([]*Schedule, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	schedules, err := m.loadAllSchedules(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to load schedules: %w", err)
	}
	claimed := make([]*Schedule, 0)
	for _, item := range schedules {
		item, ok, err := m.claimSchedule(ctx, item.ID, now, lease)
		if err != nil {
			return nil, err
		}
		if ok {
			claimed = append(claimed, item)
		}
	}
	return claimed, nil
}

// claimSchedule uses the Kubernetes resourceVersion as an optimistic lock.
// The in-process mutex above protects a single manager; resourceVersion makes
// the claim exclusive when multiple API pods race to claim the same schedule.
func (m *KubernetesManager) claimSchedule(ctx context.Context, id string, now time.Time, lease time.Duration) (*Schedule, bool, error) {
	secretName := scheduleSecretName(id)
	for range 3 {
		secret, err := m.client.CoreV1().Secrets(m.namespace).Get(ctx, secretName, metav1.GetOptions{})
		if err != nil {
			return nil, false, fmt.Errorf("failed to load schedule for claim: %w", err)
		}
		data, ok := secret.Data[SecretKeySchedule]
		if !ok {
			return nil, false, fmt.Errorf("schedule secret missing data key: %s", SecretKeySchedule)
		}
		var item Schedule
		if err := json.Unmarshal(data, &item); err != nil {
			return nil, false, fmt.Errorf("failed to unmarshal schedule for claim: %w", err)
		}
		if teamID := secret.Annotations[AnnotationScheduleTeamID]; teamID != "" {
			item.TeamID = teamID
		}
		if !item.IsDue(now) || (item.PendingExecution != nil && item.PendingExecution.ExpiresAt.After(now)) {
			return nil, false, nil
		}
		if item.PendingExecution == nil {
			item.PendingExecution = &PendingExecution{ExecutionID: m.ids.New(), SessionID: m.ids.New()}
		}
		// An expired claim keeps its stable IDs. If session creation succeeded but
		// finalize was interrupted, retrying /start remains idempotent.
		item.PendingExecution.ClaimedAt = now
		item.PendingExecution.ExpiresAt = now.Add(lease)
		item.UpdatedAt = now
		encoded, err := json.Marshal(&item)
		if err != nil {
			return nil, false, fmt.Errorf("failed to marshal schedule claim: %w", err)
		}
		secret.Data[SecretKeySchedule] = encoded
		if _, err = m.client.CoreV1().Secrets(m.namespace).Update(ctx, secret, metav1.UpdateOptions{}); err == nil {
			return &item, true, nil
		} else if !errors.IsConflict(err) {
			return nil, false, fmt.Errorf("failed to persist schedule claim: %w", err)
		}
	}
	return nil, false, fmt.Errorf("failed to claim schedule %s after concurrent updates", id)
}

func (m *KubernetesManager) FinalizeClaim(ctx context.Context, id, executionID string, record ExecutionRecord, nextAt *time.Time, completed bool) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	item, err := m.loadSchedule(ctx, id)
	if err != nil {
		return err
	}
	if item.LastExecution != nil && item.LastExecution.ExecutionID == executionID {
		return nil
	}
	if item.PendingExecution == nil || item.PendingExecution.ExecutionID != executionID {
		return fmt.Errorf("schedule execution claim does not match")
	}
	record.ExecutionID = executionID
	recordExecution(item, record)
	item.ExecutionCount++
	item.PendingExecution = nil
	item.NextExecutionAt = nextAt
	if completed {
		item.Status = ScheduleStatusCompleted
	}
	item.UpdatedAt = m.clock.Now()
	return m.saveSchedule(ctx, item)
}

// RecordExecution records an execution attempt
func (m *KubernetesManager) RecordExecution(ctx context.Context, id string, record ExecutionRecord) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	schedule, err := m.loadSchedule(ctx, id)
	if err != nil {
		return err
	}

	recordExecution(schedule, record)
	schedule.ExecutionCount++
	schedule.UpdatedAt = m.clock.Now()

	if err := m.saveSchedule(ctx, schedule); err != nil {
		return fmt.Errorf("failed to save schedule: %w", err)
	}

	return nil
}

func appendExecutionHistory(history []ExecutionRecord, record ExecutionRecord) []ExecutionRecord {
	history = append(history, record)
	if len(history) > MaxExecutionHistory {
		history = history[len(history)-MaxExecutionHistory:]
	}
	return history
}

func recordExecution(schedule *Schedule, record ExecutionRecord) {
	// Seed history from the legacy last_execution field on the first write after
	// upgrading, so the last known attempt is not discarded during migration.
	if len(schedule.ExecutionHistory) == 0 && schedule.LastExecution != nil {
		schedule.ExecutionHistory = appendExecutionHistory(schedule.ExecutionHistory, *schedule.LastExecution)
	}
	schedule.ExecutionHistory = appendExecutionHistory(schedule.ExecutionHistory, record)
	schedule.LastExecution = &record
}

// UpdateNextExecution updates the next execution time for a schedule
func (m *KubernetesManager) UpdateNextExecution(ctx context.Context, id string, nextAt time.Time) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	schedule, err := m.loadSchedule(ctx, id)
	if err != nil {
		return err
	}

	schedule.NextExecutionAt = &nextAt
	schedule.UpdatedAt = m.clock.Now()

	if err := m.saveSchedule(ctx, schedule); err != nil {
		return fmt.Errorf("failed to save schedule: %w", err)
	}

	return nil
}

// loadSchedule loads a single schedule from its Kubernetes Secret
