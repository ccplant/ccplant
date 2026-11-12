package provisioner

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

type statusTransport int

func (s statusTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	return &http.Response{
		StatusCode: int(s),
		Header:     http.Header{},
		Body:       io.NopCloser(strings.NewReader("")),
		Request:    req,
	}, nil
}

func restartProvisionSignalled(s *Server) bool {
	select {
	case <-s.restartProvisionNow:
		return true
	default:
		return false
	}
}

func TestConnectProvisionerStartsRestartProvisionWhenRequestIsMissing(t *testing.T) {
	tests := []struct {
		name   string
		status int
		want   bool
	}{
		{name: "provision request exists", status: http.StatusOK, want: false},
		{name: "provision request missing", status: http.StatusNotFound, want: true},
		{name: "manager error", status: http.StatusInternalServerError, want: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv := New(0, "")
			client := &http.Client{Transport: statusTransport(tt.status)}
			connectProvisioner(context.Background(), srv, client, PullClientConfig{
				ProxyURL:  "http://manager.invalid",
				SessionID: "session-1",
				PodName:   "pod-1",
			})
			if got := restartProvisionSignalled(srv); got != tt.want {
				t.Fatalf("restart provision signalled = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestWaitForRestartAutoProvision(t *testing.T) {
	t.Run("returns as soon as it is signalled", func(t *testing.T) {
		srv := New(0, "")
		srv.StartRestartAutoProvisionNow()
		srv.StartRestartAutoProvisionNow()
		started := time.Now()
		if !srv.waitForRestartAutoProvision(context.Background(), time.Hour) {
			t.Fatal("wait reported cancellation")
		}
		if elapsed := time.Since(started); elapsed > time.Second {
			t.Fatalf("wait took %s", elapsed)
		}
	})
	t.Run("falls back to the delay", func(t *testing.T) {
		srv := New(0, "")
		if !srv.waitForRestartAutoProvision(context.Background(), 10*time.Millisecond) {
			t.Fatal("wait reported cancellation")
		}
		if restartProvisionSignalled(srv) {
			t.Fatal("delay expiry must not look like a signal")
		}
	})
	t.Run("stops when the context is cancelled", func(t *testing.T) {
		srv := New(0, "")
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		if srv.waitForRestartAutoProvision(ctx, time.Hour) {
			t.Fatal("wait continued after cancellation")
		}
	})
	t.Run("server without the signal channel keeps the delay", func(t *testing.T) {
		srv := &Server{}
		srv.StartRestartAutoProvisionNow()
		if !srv.waitForRestartAutoProvision(context.Background(), 10*time.Millisecond) {
			t.Fatal("wait reported cancellation")
		}
	})
}
