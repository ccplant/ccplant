package cmd

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/takutakahashi/agentapi-proxy/internal/infrastructure/controlapi"
)

func TestMonitorWorkerControlAPIFailsImmediatelyOnAuthenticationError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	t.Cleanup(server.Close)
	repo := controlapi.NewSlackBotRepository(controlapi.NewSessionManager(server.URL, "invalid"))

	err := <-monitorWorkerControlAPI(context.Background(), repo, time.Millisecond, 4)
	require.ErrorContains(t, err, "permanent failure")
}

func TestMonitorWorkerControlAPIRecoversBeforeThreshold(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if calls.Add(1) < 3 {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		_, _ = w.Write([]byte("[]"))
	}))
	t.Cleanup(server.Close)
	repo := controlapi.NewSlackBotRepository(controlapi.NewSessionManager(server.URL, "token"))
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()

	err := <-monitorWorkerControlAPI(ctx, repo, time.Millisecond, 4)
	require.NoError(t, err)
	require.GreaterOrEqual(t, calls.Load(), int32(3))
}

func TestMonitorWorkerControlAPIFailsAfterTransientThreshold(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	t.Cleanup(server.Close)
	repo := controlapi.NewSlackBotRepository(controlapi.NewSessionManager(server.URL, "token"))

	err := <-monitorWorkerControlAPI(context.Background(), repo, time.Millisecond, 3)
	require.ErrorContains(t, err, "3 consecutive checks")
}
