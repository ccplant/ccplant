package schedule

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/labstack/echo/v4"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/kubernetes/fake"
	k8stesting "k8s.io/client-go/testing"
)

type fakeClock struct {
	mu  sync.Mutex
	now time.Time
}

func (c *fakeClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *fakeClock) Advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = c.now.Add(d)
}

type sequenceIDs struct {
	mu  sync.Mutex
	ids []string
}

// enforceSecretResourceVersions adds the optimistic concurrency behavior of a
// real Kubernetes API server, which client-go's object tracker does not model.
func enforceSecretResourceVersions(client *fake.Clientset) {
	var mu sync.Mutex
	version := 0
	resource := corev1.SchemeGroupVersion.WithResource("secrets")
	client.PrependReactor("update", "secrets", func(action k8stesting.Action) (bool, runtime.Object, error) {
		mu.Lock()
		defer mu.Unlock()
		update := action.(k8stesting.UpdateAction)
		candidate := update.GetObject().(*corev1.Secret)
		currentObject, err := client.Tracker().Get(resource, candidate.Namespace, candidate.Name)
		if err != nil {
			return true, nil, err
		}
		current := currentObject.(*corev1.Secret)
		if candidate.ResourceVersion != current.ResourceVersion {
			return true, nil, apierrors.NewConflict(
				schema.GroupResource{Resource: "secrets"},
				candidate.Name,
				fmt.Errorf("resource version changed"),
			)
		}
		version++
		updated := candidate.DeepCopy()
		updated.ResourceVersion = strconv.Itoa(version)
		if err := client.Tracker().Update(resource, updated, candidate.Namespace); err != nil {
			return true, nil, err
		}
		return true, updated, nil
	})
}

func (g *sequenceIDs) New() string {
	g.mu.Lock()
	defer g.mu.Unlock()
	if len(g.ids) == 0 {
		panic("schedule test exhausted deterministic IDs")
	}
	id := g.ids[0]
	g.ids = g.ids[1:]
	return id
}

// TestScheduleAcceptance_OneTimeLifecycle is the vertical schedule contract:
// create through HTTP, remain idle before the due time, launch once when due,
// and expose the completed execution through HTTP.
func TestScheduleAcceptance_OneTimeLifecycle(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	start := time.Date(2026, time.September, 7, 9, 0, 0, 0, time.UTC)
	due := start.Add(time.Hour)
	clock := &fakeClock{now: start}
	ids := &sequenceIDs{ids: []string{"schedule-fixed", "session-fixed"}}
	manager := NewKubernetesManager(fake.NewSimpleClientset(), "default").WithRuntime(clock, nil)
	sessions := newMockProxySessionManager()
	handlers := NewHandlersWithTimezone(manager, sessions, nil, nil, "UTC").WithRuntime(clock, ids)
	worker := NewWorker(manager, sessions, nil, WorkerConfig{Enabled: true}, nil).WithRuntime(clock, ids)

	e := echo.New()
	e.Use(func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			setTestUser(c, "user-fixed")
			return next(c)
		}
	})
	require.NoError(t, handlers.RegisterRoutes(e))

	body, err := json.Marshal(CreateScheduleRequest{
		Name:        "deterministic one-time schedule",
		ScheduledAt: &due,
		SessionConfig: SessionConfig{
			Tags: map[string]string{"qa": "acceptance"},
		},
	})
	require.NoError(t, err)

	create := httptest.NewRequest(http.MethodPost, "/schedules", bytes.NewReader(body))
	create.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
	createRecorder := httptest.NewRecorder()
	e.ServeHTTP(createRecorder, create)
	require.Equal(t, http.StatusCreated, createRecorder.Code, createRecorder.Body.String())

	var created ScheduleResponse
	require.NoError(t, json.Unmarshal(createRecorder.Body.Bytes(), &created))
	require.Equal(t, "schedule-fixed", created.ID)
	require.Equal(t, due, *created.NextExecutionAt)

	processed, err := worker.ProcessDueSchedules(ctx)
	require.NoError(t, err)
	require.Zero(t, processed)
	require.Empty(t, sessions.sessions)

	clock.Advance(time.Hour)
	processed, err = worker.ProcessDueSchedules(ctx)
	require.NoError(t, err)
	require.Equal(t, 1, processed)
	require.Contains(t, sessions.sessions, "session-fixed")
	require.Equal(t, "schedule-fixed", sessions.sessions["session-fixed"].tags["schedule_id"])

	get := httptest.NewRequest(http.MethodGet, "/schedules/schedule-fixed", nil)
	getRecorder := httptest.NewRecorder()
	e.ServeHTTP(getRecorder, get)
	require.Equal(t, http.StatusOK, getRecorder.Code, getRecorder.Body.String())

	var completed ScheduleResponse
	require.NoError(t, json.Unmarshal(getRecorder.Body.Bytes(), &completed))
	require.Equal(t, ScheduleStatusCompleted, completed.Status)
	require.Equal(t, 1, completed.ExecutionCount)
	require.NotNil(t, completed.LastExecution)
	require.Equal(t, "success", completed.LastExecution.Status)
	require.Equal(t, "session-fixed", completed.LastExecution.SessionID)
	require.Equal(t, due, completed.LastExecution.ExecutedAt)

	processed, err = worker.ProcessDueSchedules(ctx)
	require.NoError(t, err)
	require.Zero(t, processed)
	require.Len(t, sessions.sessions, 1)
}

// TestScheduleAcceptance_ConcurrentClaim guarantees that horizontally scaled
// workers receive a single execution job for a due schedule.
func TestScheduleAcceptance_ConcurrentClaim(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	now := time.Date(2026, time.September, 7, 9, 0, 0, 0, time.UTC)
	clock := &fakeClock{now: now}
	client := fake.NewSimpleClientset()
	enforceSecretResourceVersions(client)
	manager := NewKubernetesManager(client, "default").WithRuntime(clock, &sequenceIDs{ids: []string{"execution-fixed", "session-fixed"}})
	due := now.Add(-time.Minute)
	require.NoError(t, manager.Create(ctx, &Schedule{
		ID:              "schedule-fixed",
		Name:            "concurrent schedule",
		UserID:          "user-fixed",
		Status:          ScheduleStatusActive,
		CronExpr:        "* * * * *",
		Timezone:        "UTC",
		NextExecutionAt: &due,
	}))

	start := make(chan struct{})
	results := make(chan []*Schedule, 2)
	errs := make(chan error, 2)
	var wg sync.WaitGroup
	workers := []*KubernetesManager{
		manager,
		NewKubernetesManager(client, "default").WithRuntime(clock, &sequenceIDs{ids: []string{"execution-fixed", "session-fixed"}}),
	}
	for _, workerManager := range workers {
		wg.Add(1)
		go func(workerManager *KubernetesManager) {
			defer wg.Done()
			<-start
			claimed, err := workerManager.ClaimDueSchedules(ctx, clock.Now(), 5*time.Minute)
			results <- claimed
			errs <- err
		}(workerManager)
	}
	close(start)
	wg.Wait()
	close(results)
	close(errs)

	for err := range errs {
		require.NoError(t, err)
	}
	claimedCount := 0
	for claimed := range results {
		claimedCount += len(claimed)
		if len(claimed) == 1 {
			require.Equal(t, "execution-fixed", claimed[0].PendingExecution.ExecutionID)
			require.Equal(t, "session-fixed", claimed[0].PendingExecution.SessionID)
		}
	}
	require.Equal(t, 1, claimedCount)
}
