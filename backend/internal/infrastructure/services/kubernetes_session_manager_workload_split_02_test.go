package services

import (
	"context"
	"testing"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	coreallocation "github.com/takutakahashi/agentapi-proxy/internal/core/sessionallocation"
	"github.com/takutakahashi/agentapi-proxy/pkg/sessionsettings"
)

func hasEnvValue(env []corev1.EnvVar, name, value string) bool {
	for _, item := range env {
		if item.Name == name && item.Value == value {
			return true
		}
	}
	return false
}

func hasMount(mounts []corev1.VolumeMount, name, path, subPath string) bool {
	for _, mount := range mounts {
		if mount.Name == name && mount.MountPath == path && mount.SubPath == subPath {
			return true
		}
	}
	return false
}

func TestCreateSessionWorkloadWithPVCUsesDeploymentRestartPolicyAlways(t *testing.T) {
	manager := newWorkloadTestManager(t, true)
	session := newWorkloadTestSession()

	if err := manager.createSessionWorkload(context.Background(), session, session.Request()); err != nil {
		t.Fatalf("Failed to create workload: %v", err)
	}

	deployment, err := manager.client.AppsV1().Deployments("test-ns").Get(context.Background(), session.DeploymentName(), metav1.GetOptions{})
	if err != nil {
		t.Fatalf("Expected deployment to be created: %v", err)
	}
	if deployment.Spec.Template.Spec.RestartPolicy != corev1.RestartPolicyAlways {
		t.Fatalf("Expected restartPolicy Always, got %s", deployment.Spec.Template.Spec.RestartPolicy)
	}
	if deployment.Spec.Template.Spec.Volumes[0].PersistentVolumeClaim == nil {
		t.Fatal("Expected workdir volume to mount PVC when PVC is enabled")
	}

	pods, err := manager.client.CoreV1().Pods("test-ns").List(context.Background(), metav1.ListOptions{})
	if err != nil {
		t.Fatalf("Failed to list pods: %v", err)
	}
	if len(pods.Items) != 0 {
		t.Fatalf("Expected no standalone pods, got %d", len(pods.Items))
	}
}

func TestSessionResourcesUseServiceOwnerReferenceWithoutPVC(t *testing.T) {
	manager := newWorkloadTestManager(t, false)
	session := newWorkloadTestSession()
	ctx := context.Background()

	if err := manager.createService(ctx, session); err != nil {
		t.Fatalf("Failed to create service: %v", err)
	}
	if err := manager.createSessionWorkload(ctx, session, session.Request()); err != nil {
		t.Fatalf("Failed to create workload: %v", err)
	}
	if err := manager.createOneshotSettingsSecret(ctx, session); err != nil {
		t.Fatalf("Failed to create oneshot settings secret: %v", err)
	}
	settings := &sessionsettings.SessionSettings{}
	session.SetProvisionSettings(settings)
	if err := manager.CreateProvisionRequest(ctx, session); err != nil {
		t.Fatalf("Failed to create provision request: %v", err)
	}
	if err := manager.createSessionSettingsSecretFromSettings(ctx, session, session.Request(), settings); err != nil {
		t.Fatalf("Failed to create session settings secret: %v", err)
	}

	pod, err := manager.client.CoreV1().Pods("test-ns").Get(ctx, session.DeploymentName(), metav1.GetOptions{})
	if err != nil {
		t.Fatalf("Expected pod to be created: %v", err)
	}
	assertOwnedByService(t, pod, session.ServiceName())

	for _, secretName := range []string{
		session.ServiceName() + "-oneshot-settings",
		"agentapi-provision-request-" + session.ID(),
		"agentapi-session-" + session.ID() + "-settings",
	} {
		secret, err := manager.client.CoreV1().Secrets("test-ns").Get(ctx, secretName, metav1.GetOptions{})
		if err != nil {
			t.Fatalf("Expected secret %s to be created: %v", secretName, err)
		}
		assertOwnedByService(t, secret, session.ServiceName())
	}
}

func TestSessionResourcesUseServiceOwnerReferenceWithPVC(t *testing.T) {
	manager := newWorkloadTestManager(t, true)
	session := newWorkloadTestSession()
	ctx := context.Background()

	if err := manager.createService(ctx, session); err != nil {
		t.Fatalf("Failed to create service: %v", err)
	}
	if err := manager.createPVC(ctx, session); err != nil {
		t.Fatalf("Failed to create PVC: %v", err)
	}
	if err := manager.createSessionWorkload(ctx, session, session.Request()); err != nil {
		t.Fatalf("Failed to create workload: %v", err)
	}

	pvc, err := manager.client.CoreV1().PersistentVolumeClaims("test-ns").Get(ctx, session.PVCName(), metav1.GetOptions{})
	if err != nil {
		t.Fatalf("Expected PVC to be created: %v", err)
	}
	assertOwnedByService(t, pvc, session.ServiceName())

	deployment, err := manager.client.AppsV1().Deployments("test-ns").Get(ctx, session.DeploymentName(), metav1.GetOptions{})
	if err != nil {
		t.Fatalf("Expected deployment to be created: %v", err)
	}
	assertOwnedByService(t, deployment, session.ServiceName())
}

func TestDeleteSessionResourcesDeletesAllSessionLabeledSecrets(t *testing.T) {
	manager := newWorkloadTestManager(t, false)
	session := newWorkloadTestSession()
	ctx := context.Background()

	for _, secret := range []*corev1.Secret{
		{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "agentapi-provision-request-" + session.ID(),
				Namespace: "test-ns",
				Labels: map[string]string{
					"agentapi.proxy/session-id":        session.ID(),
					"agentapi.proxy/provision-request": "true",
				},
			},
		},
		{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "agentapi-session-configuration-deadbeef",
				Namespace: "test-ns",
				Labels:    map[string]string{"agentapi.proxy/session-id": session.ID()},
			},
		},
		{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "other-session-secret",
				Namespace: "test-ns",
				Labels:    map[string]string{"agentapi.proxy/session-id": "other-session"},
			},
		},
	} {
		if _, err := manager.client.CoreV1().Secrets("test-ns").Create(ctx, secret, metav1.CreateOptions{}); err != nil {
			t.Fatalf("create Secret %s: %v", secret.Name, err)
		}
	}

	if err := manager.deleteSessionResources(ctx, session); err != nil {
		t.Fatalf("deleteSessionResources() error = %v", err)
	}

	for _, name := range []string{"agentapi-provision-request-" + session.ID(), "agentapi-session-configuration-deadbeef"} {
		if _, err := manager.client.CoreV1().Secrets("test-ns").Get(ctx, name, metav1.GetOptions{}); !errors.IsNotFound(err) {
			t.Errorf("expected session Secret %s to be deleted, got %v", name, err)
		}
	}
	if _, err := manager.client.CoreV1().Secrets("test-ns").Get(ctx, "other-session-secret", metav1.GetOptions{}); err != nil {
		t.Errorf("expected another session's Secret to remain, got %v", err)
	}
}

func TestPurgeStockSessionsDeletesMixedWorkloadKindsAndPVC(t *testing.T) {
	manager := newWorkloadTestManager(t, false)
	ctx := context.Background()
	sessionID := "stock-session"
	name := "agentapi-session-" + sessionID
	orphanSessionID := "orphan-stock-session"
	orphanName := "agentapi-session-" + orphanSessionID
	labels := map[string]string{
		"app.kubernetes.io/managed-by":      "agentapi-proxy",
		"agentapi.proxy/stock":              "true",
		"agentapi.proxy/session-id":         sessionID,
		"agentapi.proxy/capability-sandbox": "false",
		"agentapi.proxy/capability-dind":    "false",
	}

	_, err := manager.client.CoreV1().Services("test-ns").Create(ctx, &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name + "-svc",
			Namespace: "test-ns",
			Labels:    labels,
		},
	}, metav1.CreateOptions{})
	if err != nil {
		t.Fatalf("Failed to create stock service: %v", err)
	}
	_, err = manager.client.AppsV1().Deployments("test-ns").Create(ctx, &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: "test-ns",
			Labels:    labels,
		},
	}, metav1.CreateOptions{})
	if err != nil {
		t.Fatalf("Failed to create legacy stock deployment: %v", err)
	}
	_, err = manager.client.CoreV1().Pods("test-ns").Create(ctx, &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: "test-ns",
			Labels:    labels,
		},
	}, metav1.CreateOptions{})
	if err != nil {
		t.Fatalf("Failed to create stock pod: %v", err)
	}
	_, err = manager.client.CoreV1().PersistentVolumeClaims("test-ns").Create(ctx, &corev1.PersistentVolumeClaim{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name + "-pvc",
			Namespace: "test-ns",
			Labels:    labels,
		},
		Spec: corev1.PersistentVolumeClaimSpec{
			AccessModes: []corev1.PersistentVolumeAccessMode{corev1.ReadWriteOnce},
			Resources: corev1.VolumeResourceRequirements{
				Requests: corev1.ResourceList{
					corev1.ResourceStorage: resource.MustParse("1Gi"),
				},
			},
		},
	}, metav1.CreateOptions{})
	if err != nil {
		t.Fatalf("Failed to create legacy stock PVC: %v", err)
	}
	orphanLabels := map[string]string{
		"app.kubernetes.io/managed-by":      "agentapi-proxy",
		"agentapi.proxy/stock":              "true",
		"agentapi.proxy/session-id":         orphanSessionID,
		"agentapi.proxy/capability-sandbox": "false",
		"agentapi.proxy/capability-dind":    "false",
	}
	_, err = manager.client.AppsV1().Deployments("test-ns").Create(ctx, &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      orphanName,
			Namespace: "test-ns",
			Labels:    orphanLabels,
		},
	}, metav1.CreateOptions{})
	if err != nil {
		t.Fatalf("Failed to create orphaned stock deployment: %v", err)
	}
	_, err = manager.client.CoreV1().PersistentVolumeClaims("test-ns").Create(ctx, &corev1.PersistentVolumeClaim{
		ObjectMeta: metav1.ObjectMeta{
			Name:      orphanName + "-pvc",
			Namespace: "test-ns",
			Labels:    orphanLabels,
		},
		Spec: corev1.PersistentVolumeClaimSpec{
			AccessModes: []corev1.PersistentVolumeAccessMode{corev1.ReadWriteOnce},
			Resources: corev1.VolumeResourceRequirements{
				Requests: corev1.ResourceList{
					corev1.ResourceStorage: resource.MustParse("1Gi"),
				},
			},
		},
	}, metav1.CreateOptions{})
	if err != nil {
		t.Fatalf("Failed to create orphaned stock PVC: %v", err)
	}

	if err := manager.PurgeStockSessions(ctx); err != nil {
		t.Fatalf("PurgeStockSessions failed: %v", err)
	}

	if _, err := manager.client.CoreV1().Services("test-ns").Get(ctx, name+"-svc", metav1.GetOptions{}); !errors.IsNotFound(err) {
		t.Fatalf("Expected service to be deleted, got err=%v", err)
	}
	if _, err := manager.client.AppsV1().Deployments("test-ns").Get(ctx, name, metav1.GetOptions{}); !errors.IsNotFound(err) {
		t.Fatalf("Expected deployment to be deleted, got err=%v", err)
	}
	if _, err := manager.client.CoreV1().Pods("test-ns").Get(ctx, name, metav1.GetOptions{}); !errors.IsNotFound(err) {
		t.Fatalf("Expected pod to be deleted, got err=%v", err)
	}
	if _, err := manager.client.CoreV1().PersistentVolumeClaims("test-ns").Get(ctx, name+"-pvc", metav1.GetOptions{}); !errors.IsNotFound(err) {
		t.Fatalf("Expected PVC to be deleted, got err=%v", err)
	}
	if _, err := manager.client.AppsV1().Deployments("test-ns").Get(ctx, orphanName, metav1.GetOptions{}); !errors.IsNotFound(err) {
		t.Fatalf("Expected orphaned deployment to be deleted, got err=%v", err)
	}
	if _, err := manager.client.CoreV1().PersistentVolumeClaims("test-ns").Get(ctx, orphanName+"-pvc", metav1.GetOptions{}); !errors.IsNotFound(err) {
		t.Fatalf("Expected orphaned PVC to be deleted, got err=%v", err)
	}
}

func TestStockInventoryIsScopedToSessionManager(t *testing.T) {
	manager := newWorkloadTestManager(t, false)
	manager.config.SessionManager.ID = "manager-a"
	ctx := context.Background()

	for _, managerID := range []string{"manager-a", "manager-b"} {
		sessionID := "stock-" + managerID
		name := "agentapi-session-" + sessionID
		resourceLabels := map[string]string{
			"app.kubernetes.io/name":            "agentapi-session",
			"app.kubernetes.io/managed-by":      "agentapi-proxy",
			"agentapi.proxy/session-id":         sessionID,
			"agentapi.proxy/session-manager-id": managerID,
			"agentapi.proxy/stock":              "true",
			"agentapi.proxy/capability-sandbox": "true",
			"agentapi.proxy/capability-dind":    "false",
		}
		if _, err := manager.client.CoreV1().Services("test-ns").Create(ctx, &corev1.Service{
			ObjectMeta: metav1.ObjectMeta{Name: name + "-svc", Namespace: "test-ns", Labels: resourceLabels},
		}, metav1.CreateOptions{}); err != nil {
			t.Fatalf("create stock Service for %s: %v", managerID, err)
		}
		if _, err := manager.client.AppsV1().Deployments("test-ns").Create(ctx, &appsv1.Deployment{
			ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: "test-ns", Labels: resourceLabels},
		}, metav1.CreateOptions{}); err != nil {
			t.Fatalf("create stock Deployment for %s: %v", managerID, err)
		}
	}

	count, err := manager.CountStockSessions(ctx, false)
	if err != nil {
		t.Fatalf("CountStockSessions() error = %v", err)
	}
	if count != 1 {
		t.Fatalf("CountStockSessions() = %d, want 1", count)
	}
	stock, err := manager.findStockSession(ctx, coreallocation.Requirements{})
	if err != nil {
		t.Fatalf("findStockSession() error = %v", err)
	}
	if stock == nil || stock.Labels["agentapi.proxy/session-manager-id"] != "manager-a" {
		t.Fatalf("findStockSession() = %#v, want manager-a stock", stock)
	}

	if err := manager.PurgeStockSessions(ctx); err != nil {
		t.Fatalf("PurgeStockSessions() error = %v", err)
	}
	if _, err := manager.client.CoreV1().Services("test-ns").Get(ctx, "agentapi-session-stock-manager-a-svc", metav1.GetOptions{}); !errors.IsNotFound(err) {
		t.Fatalf("manager-a stock Service should be deleted, got %v", err)
	}
	if _, err := manager.client.CoreV1().Services("test-ns").Get(ctx, "agentapi-session-stock-manager-b-svc", metav1.GetOptions{}); err != nil {
		t.Fatalf("manager-b stock Service should remain, got %v", err)
	}
	if _, err := manager.client.AppsV1().Deployments("test-ns").Get(ctx, "agentapi-session-stock-manager-b", metav1.GetOptions{}); err != nil {
		t.Fatalf("manager-b stock Deployment should remain, got %v", err)
	}
}
