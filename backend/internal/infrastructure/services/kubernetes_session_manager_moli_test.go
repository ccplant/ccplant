package services

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
)

func TestBuildDeploymentInjectsMoliIntoEverySession(t *testing.T) {
	manager := newPodTemplateTestManager("")
	manager.k8sConfig.MoliEnabled = true
	manager.k8sConfig.MoliImage = "browser:test"
	manager.k8sConfig.MoliPort = 9222
	manager.k8sConfig.MoliLayout = true
	manager.k8sConfig.MoliCPURequest = "100m"
	manager.k8sConfig.MoliCPULimit = "1"
	manager.k8sConfig.MoliMemoryRequest = "128Mi"
	manager.k8sConfig.MoliMemoryLimit = "512Mi"

	deployment, err := manager.buildDeployment(context.Background(), newPodTemplateTestSession(), newPodTemplateTestSession().Request())
	require.NoError(t, err)
	spec := deployment.Spec.Template.Spec
	moli := findContainerByName(spec.Containers, "moli")
	require.NotNil(t, moli)
	require.Equal(t, "browser:test", moli.Image)
	require.Equal(t, []string{"serve", "--host", "127.0.0.1", "--port", "9222", "--layout"}, moli.Args)
	require.True(t, *moli.SecurityContext.RunAsNonRoot)
	require.True(t, *moli.SecurityContext.ReadOnlyRootFilesystem)
	require.Equal(t, int64(999), *moli.SecurityContext.RunAsUser)
	require.Equal(t, "/json/version", moli.ReadinessProbe.HTTPGet.Path)
	require.Contains(t, volumeNames(spec.Volumes), "moli-tmp")

	main := findContainerByName(spec.Containers, "agentapi")
	require.Contains(t, main.Env, corev1.EnvVar{Name: "MOLI_CDP_ENDPOINT", Value: "http://127.0.0.1:9222"})
	require.Contains(t, main.Env, corev1.EnvVar{Name: "CDP_ENDPOINT", Value: "http://127.0.0.1:9222"})
}

func TestBuildDeploymentOmitsMoliByDefault(t *testing.T) {
	manager := newPodTemplateTestManager("")
	deployment, err := manager.buildDeployment(context.Background(), newPodTemplateTestSession(), newPodTemplateTestSession().Request())
	require.NoError(t, err)
	require.Nil(t, findContainerByName(deployment.Spec.Template.Spec.Containers, "moli"))
}

func TestSessionPodTemplateCannotOverrideMoli(t *testing.T) {
	manager := newPodTemplateTestManager("")
	manager.k8sConfig.MoliEnabled = true
	manager.k8sConfig.MoliImage = "browser:test"
	manager.k8sConfig.MoliPort = 9222
	manager.k8sConfig.MoliCPURequest = "100m"
	manager.k8sConfig.MoliCPULimit = "1"
	manager.k8sConfig.MoliMemoryRequest = "128Mi"
	manager.k8sConfig.MoliMemoryLimit = "512Mi"
	deployment, err := manager.buildDeployment(context.Background(), newPodTemplateTestSession(), newPodTemplateTestSession().Request())
	require.NoError(t, err)

	generated := &deployment.Spec.Template
	patched := generated.DeepCopy()
	moli := findContainerByName(patched.Spec.Containers, "moli")
	moli.Image = "untrusted:test"
	moli.Args = []string{"serve", "--host", "0.0.0.0"}
	patched.Spec.Volumes = nil
	restoreSessionPodTemplateInvariants(patched, generated)

	restored := findContainerByName(patched.Spec.Containers, "moli")
	require.Equal(t, "browser:test", restored.Image)
	require.Equal(t, "127.0.0.1", restored.Args[2])
	require.Contains(t, volumeNames(patched.Spec.Volumes), "moli-tmp")
}

func TestStockTemplateHashTracksMoliConfiguration(t *testing.T) {
	manager := newWorkloadTestManager(t, false)
	before, err := manager.stockPodTemplateHash(context.Background(), false)
	require.NoError(t, err)
	manager.k8sConfig.MoliEnabled = true
	manager.k8sConfig.MoliImage = "browser:test"
	manager.k8sConfig.MoliPort = 9222
	manager.k8sConfig.MoliCPURequest = "100m"
	manager.k8sConfig.MoliCPULimit = "1"
	manager.k8sConfig.MoliMemoryRequest = "128Mi"
	manager.k8sConfig.MoliMemoryLimit = "512Mi"
	after, err := manager.stockPodTemplateHash(context.Background(), false)
	require.NoError(t, err)
	require.NotEqual(t, before, after)
}
