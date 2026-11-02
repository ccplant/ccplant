package cmd

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"

	"github.com/takutakahashi/agentapi-proxy/internal/infrastructure/repositories"
)

func routeSecret(t *testing.T, name string, created time.Time, route sessionRouteSecretData) *corev1.Secret {
	t.Helper()
	raw, err := json.Marshal(route)
	if err != nil {
		t.Fatal(err)
	}
	return &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name: name, Namespace: "test-ns", CreationTimestamp: metav1.NewTime(created),
			Labels: map[string]string{repositories.LabelSessionRoute: "true"},
		},
		Data: map[string][]byte{repositories.SessionRouteSecretKey: raw},
	}
}

func TestFindOrphanedSessionRoutesOnlyReturnsOldMissingLocalRoutes(t *testing.T) {
	now := time.Now()
	client := fake.NewSimpleClientset(
		routeSecret(t, "orphan", now.Add(-time.Hour), sessionRouteSecretData{SessionID: "public-orphan", RemoteSessionID: "missing"}),
		routeSecret(t, "active", now.Add(-time.Hour), sessionRouteSecretData{SessionID: "public-active", RemoteSessionID: "active"}),
		routeSecret(t, "external", now.Add(-time.Hour), sessionRouteSecretData{SessionID: "public-external", RemoteSessionID: "remote", ManagerID: "manager"}),
		routeSecret(t, "pending", now.Add(-time.Hour), sessionRouteSecretData{SessionID: "public-pending"}),
		routeSecret(t, "young", now.Add(-time.Minute), sessionRouteSecretData{SessionID: "public-young", RemoteSessionID: "missing-young"}),
		routeSecret(t, "suspended", now.Add(-time.Hour), sessionRouteSecretData{SessionID: "public-suspended", RemoteSessionID: "suspended"}),
		&appsv1.Deployment{ObjectMeta: metav1.ObjectMeta{Name: "agentapi-session-active", Namespace: "workload-ns"}},
		&corev1.Service{ObjectMeta: metav1.ObjectMeta{Name: "agentapi-session-suspended-svc", Namespace: "workload-ns"}},
	)

	orphans, err := findOrphanedSessionRoutes(context.Background(), client, "test-ns", "workload-ns", 10*time.Minute, now)
	if err != nil {
		t.Fatal(err)
	}
	if len(orphans) != 1 || orphans[0].sessionID != "public-orphan" {
		t.Fatalf("orphans = %#v, want public-orphan", orphans)
	}
}

func TestPruneSessionRoutesDryRunAndDelete(t *testing.T) {
	ctx := context.Background()
	secret := routeSecret(t, "orphan", time.Now().Add(-time.Hour), sessionRouteSecretData{SessionID: "public", RemoteSessionID: "missing"})
	client := fake.NewSimpleClientset(secret)
	orphans := []orphanedSessionRoute{{secretName: "orphan", sessionID: "public", remoteSessionID: "missing"}}

	deleted, err := pruneSessionRoutes(ctx, client, "test-ns", orphans, true)
	if err != nil || deleted != 0 {
		t.Fatalf("dry-run: deleted=%d err=%v", deleted, err)
	}
	if _, err := client.CoreV1().Secrets("test-ns").Get(ctx, "orphan", metav1.GetOptions{}); err != nil {
		t.Fatalf("dry-run removed Secret: %v", err)
	}

	deleted, err = pruneSessionRoutes(ctx, client, "test-ns", orphans, false)
	if err != nil || deleted != 1 {
		t.Fatalf("delete: deleted=%d err=%v", deleted, err)
	}
}
