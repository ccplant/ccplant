package schedule

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/labstack/echo/v4"
	"github.com/stretchr/testify/require"
	"k8s.io/client-go/kubernetes/fake"
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
	ids := &sequenceIDs{ids: []string{"execution-fixed", "session-fixed"}}
	client := fake.NewSimpleClientset()
	manager := NewKubernetesManager(client, "default").WithRuntime(clock, ids)
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
		NewKubernetesManager(client, "default").WithRuntime(clock, ids),
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
