package services

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestPurgeRetiresBeforeDeletingAnyResource(t *testing.T) {
	for _, code := range []int{http.StatusNoContent, http.StatusConflict, http.StatusServiceUnavailable} {
		t.Run(http.StatusText(code), func(t *testing.T) {
			m := newWorkloadTestManager(t, false)
			ctx := context.Background()
			name := "agentapi-session-runner"
			labels := map[string]string{"app.kubernetes.io/managed-by": "agentapi-proxy", "app.kubernetes.io/name": "agentapi-session", "agentapi.proxy/session-id": "runner", "agentapi.proxy/stock": "true"}
			_, err := m.client.CoreV1().Services("test-ns").Create(ctx, &corev1.Service{ObjectMeta: metav1.ObjectMeta{Name: name + "-svc", Labels: labels}}, metav1.CreateOptions{})
			require.NoError(t, err)
			_, err = m.client.CoreV1().Pods("test-ns").Create(ctx, &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: name, Labels: labels}}, metav1.CreateOptions{})
			require.NoError(t, err)
			retired := false
			parent := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if strings.HasSuffix(r.URL.Path, "/heartbeat") {
					_, _ = w.Write([]byte(`{"allocated_runner_ids":[]}`))
					return
				}
				if !strings.HasSuffix(r.URL.Path, "/runners/runner/retire") {
					t.Errorf("unexpected request %s", r.URL.Path)
					w.WriteHeader(404)
					return
				}
				retired = true
				if r.Header.Get("Authorization") != "Bearer token" {
					t.Error("missing manager credential")
				}
				if _, e := m.client.CoreV1().Services("test-ns").Get(ctx, name+"-svc", metav1.GetOptions{}); e != nil {
					t.Errorf("Service deleted before retirement: %v", e)
				}
				if _, e := m.client.CoreV1().Pods("test-ns").Get(ctx, name, metav1.GetOptions{}); e != nil {
					t.Errorf("Pod deleted before retirement: %v", e)
				}
				w.WriteHeader(code)
			}))
			defer parent.Close()
			m.ConfigureSessionRunnerPool(parent.URL, "manager", "token", "pool")
			err = m.PurgeStockSessions(ctx)
			if code == http.StatusServiceUnavailable {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}
			require.True(t, retired)
			_, podErr := m.client.CoreV1().Pods("test-ns").Get(ctx, name, metav1.GetOptions{})
			_, svcErr := m.client.CoreV1().Services("test-ns").Get(ctx, name+"-svc", metav1.GetOptions{})
			if code == http.StatusNoContent {
				require.True(t, apierrors.IsNotFound(podErr))
				require.True(t, apierrors.IsNotFound(svcErr))
			} else {
				require.NoError(t, podErr)
				require.NoError(t, svcErr)
			}
		})
	}
}
