package services

import (
	"context"
	"testing"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestDeleteRunnerSessionsNotRegisteredDeletesUnrestorableResources(t *testing.T) {
	ctx := context.Background()
	manager := newTestManagerForCycle(t)
	id := "orphan-runner"
	prefix := "agentapi-session-" + id
	labels := map[string]string{
		"app.kubernetes.io/managed-by": "agentapi-proxy",
		"app.kubernetes.io/name":       "agentapi-session",
		"agentapi.proxy/session-pool":  "linux",
		"agentapi.proxy/session-id":    id,
	}

	// This deliberately incomplete Service is visible to runner inventory but
	// cannot be restored into a KubernetesSession by DeleteSession.
	_, err := manager.client.CoreV1().Services(manager.namespace).Create(ctx, &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{Name: prefix + "-svc", Namespace: manager.namespace, Labels: labels},
	}, metav1.CreateOptions{})
	if err != nil {
		t.Fatal(err)
	}
	_, err = manager.client.AppsV1().Deployments(manager.namespace).Create(ctx, &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{Name: prefix, Namespace: manager.namespace, Labels: labels},
	}, metav1.CreateOptions{})
	if err != nil {
		t.Fatal(err)
	}
	_, err = manager.client.CoreV1().PersistentVolumeClaims(manager.namespace).Create(ctx, &corev1.PersistentVolumeClaim{
		ObjectMeta: metav1.ObjectMeta{Name: prefix + "-pvc", Namespace: manager.namespace, Labels: labels},
	}, metav1.CreateOptions{})
	if err != nil {
		t.Fatal(err)
	}

	if err := manager.DeleteRunnerSessionsNotRegistered(ctx, map[string]struct{}{}); err != nil {
		t.Fatal(err)
	}
	if _, err = manager.client.CoreV1().Services(manager.namespace).Get(ctx, prefix+"-svc", metav1.GetOptions{}); !errors.IsNotFound(err) {
		t.Fatalf("orphan service still exists: %v", err)
	}
	if _, err = manager.client.AppsV1().Deployments(manager.namespace).Get(ctx, prefix, metav1.GetOptions{}); !errors.IsNotFound(err) {
		t.Fatalf("orphan deployment still exists: %v", err)
	}
	if _, err = manager.client.CoreV1().PersistentVolumeClaims(manager.namespace).Get(ctx, prefix+"-pvc", metav1.GetOptions{}); !errors.IsNotFound(err) {
		t.Fatalf("orphan PVC still exists: %v", err)
	}
}
