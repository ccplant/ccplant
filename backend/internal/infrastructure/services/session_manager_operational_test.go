package services

import (
	"context"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"

	"github.com/takutakahashi/agentapi-proxy/pkg/config"
)

func TestOperationalStatusUsesLiveInventoryAndFiltersPools(t *testing.T) {
	service := func(id, pool, stock string) *corev1.Service {
		return &corev1.Service{ObjectMeta: metav1.ObjectMeta{Name: id, Namespace: "test", Labels: map[string]string{
			"app.kubernetes.io/managed-by": "agentapi-proxy", "app.kubernetes.io/name": "agentapi-session",
			"agentapi.proxy/session-id": id, "agentapi.proxy/session-pool": pool, "agentapi.proxy/stock": stock,
		}}}
	}
	manager := &KubernetesSessionManager{
		client:    fake.NewSimpleClientset(service("idle-a", "allowed", "true"), service("used-a", "allowed", "false"), service("hidden", "hidden", "false")),
		namespace: "test", config: &config.Config{SessionManager: config.SessionManagerConfig{CurrentVersion: "v1.2.3"}},
	}
	status, err := manager.OperationalStatus(context.Background(), []string{"allowed"})
	if err != nil {
		t.Fatal(err)
	}
	if status["version"] != "v1.2.3" || status["running_runners"] != 2 || status["used_runners"] != 1 {
		t.Fatalf("status=%+v", status)
	}
	running := status["running_runner_ids"].([]string)
	used := status["used_runner_ids"].([]string)
	if len(running) != 2 || running[0] != "idle-a" || running[1] != "used-a" || len(used) != 1 || used[0] != "used-a" {
		t.Fatalf("running=%v used=%v", running, used)
	}
}
