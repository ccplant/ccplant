package services

import (
	"context"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
)

func stockPublishTestManager(t *testing.T, labels map[string]string) (*KubernetesSessionManager, *fake.Clientset) {
	t.Helper()
	client := fake.NewSimpleClientset(&corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "agentapi-session-runner-1-svc",
			Namespace: "sessions",
			Labels:    labels,
		},
	})
	return &KubernetesSessionManager{client: client, namespace: "sessions"}, client
}

func stockLabel(t *testing.T, client *fake.Clientset) (string, bool) {
	t.Helper()
	svc, err := client.CoreV1().Services("sessions").Get(context.Background(), "agentapi-session-runner-1-svc", metav1.GetOptions{})
	if err != nil {
		t.Fatal(err)
	}
	value, ok := svc.Labels["agentapi.proxy/stock"]
	return value, ok
}

func TestMarkStockServiceReadyPublishesCreatingStock(t *testing.T) {
	manager, client := stockPublishTestManager(t, map[string]string{"agentapi.proxy/stock": "creating"})

	published, err := manager.markStockServiceReady(context.Background(), "agentapi-session-runner-1-svc")
	if err != nil {
		t.Fatal(err)
	}
	if !published {
		t.Fatal("creating stock was not published")
	}
	if value, _ := stockLabel(t, client); value != "true" {
		t.Fatalf("stock label = %q, want true", value)
	}
}

func TestMarkStockServiceReadyKeepsRunnerAllocatedBeforeReady(t *testing.T) {
	manager, client := stockPublishTestManager(t, map[string]string{
		"agentapi.proxy/session-pool": "pool-a",
		"agentapi.proxy/user-id":      "user-a",
	})

	published, err := manager.markStockServiceReady(context.Background(), "agentapi-session-runner-1-svc")
	if err != nil {
		t.Fatal(err)
	}
	if published {
		t.Fatal("an allocated runner was published as stock")
	}
	if value, ok := stockLabel(t, client); ok {
		t.Fatalf("allocated runner regained stock label %q", value)
	}
}

func TestMarkStockServiceReadyKeepsClaimingStock(t *testing.T) {
	manager, client := stockPublishTestManager(t, map[string]string{"agentapi.proxy/stock": "claiming"})

	published, err := manager.markStockServiceReady(context.Background(), "agentapi-session-runner-1-svc")
	if err != nil {
		t.Fatal(err)
	}
	if published {
		t.Fatal("a claiming stock was published again")
	}
	if value, _ := stockLabel(t, client); value != "claiming" {
		t.Fatalf("stock label = %q, want claiming", value)
	}
}
