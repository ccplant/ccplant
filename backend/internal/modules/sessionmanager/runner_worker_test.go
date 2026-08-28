package sessionmanager

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestRunnerClaimTreatsRemovedRegistrationAsTerminal(t *testing.T) {
	for _, status := range []int{http.StatusUnauthorized, http.StatusNotFound} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(status)
			}))
			defer server.Close()

			worker := NewRunnerWorker(nil, server.URL, "manager-a", "token")
			_, ok, err := worker.claim(context.Background(), "runner-a", "runner-token", "linux")
			if ok || !errors.Is(err, errRunnerRegistrationGone) {
				t.Fatalf("claim = (_, %t, %v), want terminal registration error", ok, err)
			}
		})
	}
}

func TestRunnerClaimKeepsTransientErrorsRetryable(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer server.Close()

	worker := NewRunnerWorker(nil, server.URL, "manager-a", "token")
	_, _, err := worker.claim(context.Background(), "runner-a", "runner-token", "linux")
	if err == nil || errors.Is(err, errRunnerRegistrationGone) {
		t.Fatalf("claim error = %v, want retryable error", err)
	}
}
