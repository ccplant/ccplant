package app

import (
	"context"
	"testing"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"

	"github.com/takutakahashi/agentapi-proxy/pkg/config"
)

func TestReconcileSessionManagerVersionUpgradesDeploymentAndFutureSessions(t *testing.T) {
	client := fake.NewSimpleClientset(&appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{Name: "manager", Namespace: "sessions"},
		Spec: appsv1.DeploymentSpec{Template: corev1.PodTemplateSpec{Spec: corev1.PodSpec{Containers: []corev1.Container{{
			Name: "session-manager", Image: "example/manager:v1.2.3", Env: []corev1.EnvVar{
				{Name: "AGENTAPI_K8S_SESSION_IMAGE", Value: "example/manager:v1.2.3"},
				{Name: "AGENTAPI_SESSION_MANAGER_CURRENT_VERSION", Value: "v1.2.3"},
			},
		}}}}},
	})
	cfg := &config.Config{SessionManager: config.SessionManagerConfig{
		AutoUpgrade: true, DeploymentName: "manager", ImageRepository: "example/manager", CurrentVersion: "v1.2.3",
	}}
	if err := reconcileSessionManagerVersion(context.Background(), cfg, client, "sessions", "v1.3.0"); err != nil {
		t.Fatal(err)
	}
	got, err := client.AppsV1().Deployments("sessions").Get(context.Background(), "manager", metav1.GetOptions{})
	if err != nil {
		t.Fatal(err)
	}
	container := got.Spec.Template.Spec.Containers[0]
	if container.Image != "example/manager:v1.3.0" {
		t.Fatalf("image = %q", container.Image)
	}
	if container.Env[0].Value != "example/manager:v1.3.0" {
		t.Fatalf("session image = %q", container.Env[0].Value)
	}
	if container.Env[1].Value != "v1.3.0" {
		t.Fatalf("current version = %q", container.Env[1].Value)
	}
}

func TestReconcileSessionManagerVersionDoesNotDowngrade(t *testing.T) {
	client := fake.NewSimpleClientset()
	cfg := &config.Config{SessionManager: config.SessionManagerConfig{
		AutoUpgrade: true, DeploymentName: "manager", ImageRepository: "example/manager", CurrentVersion: "v1.3.0",
	}}
	if err := reconcileSessionManagerVersion(context.Background(), cfg, client, "sessions", "v1.2.3"); err != nil {
		t.Fatal(err)
	}
	if len(client.Actions()) != 0 {
		t.Fatalf("unexpected Kubernetes actions: %v", client.Actions())
	}
}

func TestSessionManagerUpgradeRequiredForDevelopmentCommit(t *testing.T) {
	desired := "dev.ccplant.0123456789abcdef0123456789abcdef01234567"
	upgrade, err := sessionManagerUpgradeRequired("dev.ccplant.aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", desired)
	if err != nil || !upgrade {
		t.Fatalf("upgrade=%v err=%v", upgrade, err)
	}
	upgrade, err = sessionManagerUpgradeRequired(desired, desired)
	if err != nil || upgrade {
		t.Fatalf("same commit upgrade=%v err=%v", upgrade, err)
	}
}
