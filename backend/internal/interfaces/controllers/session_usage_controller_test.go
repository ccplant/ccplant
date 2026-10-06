package controllers

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/takutakahashi/agentapi-proxy/internal/domain/entities"
)

func TestAggregateSessionRuntimeCarriesStateAndSplitsDays(t *testing.T) {
	from := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	to := from.Add(48 * time.Hour)
	events := []entities.SessionStatusUsageEvent{
		{EventID: "1", SessionID: "one", Pool: "linux", Status: "active", OccurredAt: from.Add(-time.Hour)},
		{EventID: "2", SessionID: "one", Pool: "linux", Status: "running", OccurredAt: from.Add(2 * time.Hour)},
		{EventID: "3", SessionID: "one", Pool: "linux", Status: "suspended", OccurredAt: from.Add(26 * time.Hour)},
		{EventID: "4", SessionID: "two", Pool: "gpu", Status: "active", OccurredAt: from.Add(time.Hour)},
		{EventID: "5", SessionID: "two", Pool: "gpu", Status: "terminated", OccurredAt: from.Add(3 * time.Hour)},
	}

	result := aggregateSessionRuntime(events, from, to, to, time.UTC, 10)
	require.Equal(t, int64(28*time.Hour/time.Second), result.Summary.RuntimeSeconds)
	require.Equal(t, int64(24*time.Hour/time.Second), result.Summary.RunningSeconds)
	require.Equal(t, int64(22*time.Hour/time.Second), result.Summary.SuspendedSeconds)
	require.Equal(t, 1, result.Summary.Sessions)
	require.Equal(t, 2, result.Summary.PeakConcurrent)
	require.Len(t, result.Trend, 2)
	require.Equal(t, int64(26*time.Hour/time.Second), result.Trend[0].RuntimeSeconds)
	require.Equal(t, 1, result.Trend[0].Sessions)
	require.Equal(t, int64(2*time.Hour/time.Second), result.Trend[1].RuntimeSeconds)
	require.Equal(t, 0, result.Trend[1].Sessions)
	require.Equal(t, []string{"gpu", "linux"}, result.AvailablePools)
	require.Equal(t, "one", result.BySession[0].SessionID)
	require.Equal(t, "suspended", result.BySession[0].CurrentStatus)
}

func TestAggregateSessionRuntimeExcludesUnavailableStatuses(t *testing.T) {
	from := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	to := from.Add(4 * time.Hour)
	events := []entities.SessionStatusUsageEvent{
		{EventID: "1", SessionID: "one", Status: "active", OccurredAt: from},
		{EventID: "2", SessionID: "one", Status: "error", OccurredAt: from.Add(time.Hour)},
		{EventID: "3", SessionID: "one", Status: "stopped", OccurredAt: from.Add(2 * time.Hour)},
	}

	result := aggregateSessionRuntime(events, from, to, to, time.UTC, 10)
	require.Equal(t, int64(time.Hour/time.Second), result.Summary.RuntimeSeconds)
	require.Equal(t, 1, result.Summary.PeakConcurrent)
}
