package services

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/takutakahashi/agentapi-proxy/internal/domain/entities"
	"github.com/takutakahashi/agentapi-proxy/pkg/proxybinary"
	"github.com/takutakahashi/agentapi-proxy/pkg/sessionsettings"
	corev1 "k8s.io/api/core/v1"
)

func TestSessionCLIInjection(t *testing.T) {
	for _, pvc := range []bool{false, true} {
		for _, stock := range []bool{false, true} {
			manager := newWorkloadTestManager(t, pvc)
			manager.k8sConfig.Image = "example/agent:assets-fixed"
			manager.k8sConfig.CLIImage = "example/cli:v2"
			manager.k8sConfig.ImagePullPolicy = "IfNotPresent"
			manager.config.BinaryPath = "/manager-only/ccplant"
			session := newWorkloadTestSession()
			req := &entities.RunServerRequest{UserID: "user", Environment: map[string]string{}}
			if stock {
				req.UserID = ""
			}
			deployment, err := manager.buildDeployment(context.Background(), session, req)
			require.NoError(t, err)
			spec := deployment.Spec.Template.Spec
			init := findContainerByName(spec.InitContainers, "install-ccplant-cli")
			require.NotNil(t, init)
			require.Equal(t, "example/cli:v2", init.Image)
			require.Equal(t, corev1.PullIfNotPresent, init.ImagePullPolicy)
			require.Contains(t, init.Command[2], "cp -f /usr/local/bin/ccplant "+sessionCLIPath)
			require.False(t, init.VolumeMounts[0].ReadOnly)
			main := findContainerByName(spec.Containers, "agentapi")
			require.Equal(t, "example/agent:assets-fixed", main.Image)
			require.Contains(t, main.VolumeMounts, corev1.VolumeMount{Name: "ccplant-cli", MountPath: "/opt/ccplant/bin", ReadOnly: true})
			require.Contains(t, main.Env, corev1.EnvVar{Name: proxybinary.EnvName, Value: sessionCLIPath})
			found := false
			for _, v := range spec.Volumes {
				if v.Name == "ccplant-cli" {
					require.NotNil(t, v.EmptyDir)
					found = true
				}
			}
			require.True(t, found)
			settings := manager.buildSessionSettings(context.Background(), session, req, nil)
			require.Equal(t, sessionCLIPath, settings.Env[proxybinary.EnvName])
		}
	}
}

func TestSessionCLIInjectionLegacyImage(t *testing.T) {
	manager := newWorkloadTestManager(t, false)
	manager.config.BinaryPath = "/custom/ccplant"
	deployment, err := manager.buildDeployment(context.Background(), newWorkloadTestSession(), &entities.RunServerRequest{Environment: map[string]string{}})
	require.NoError(t, err)
	require.Nil(t, findContainerByName(deployment.Spec.Template.Spec.InitContainers, "install-ccplant-cli"))
	require.Equal(t, "/custom/ccplant", manager.sessionBinaryPath())
}

func TestNormalizeProvisionSettingsUsesInjectedCLI(t *testing.T) {
	manager := newWorkloadTestManager(t, false)
	manager.k8sConfig.CLIImage = "example/cli:v2"
	original := &sessionsettings.SessionSettings{Env: map[string]string{proxybinary.EnvName: "/parent-only/ccplant", "KEEP": "value"}}
	normalized := manager.normalizeProvisionSettings(original)
	require.Equal(t, sessionCLIPath, normalized.Env[proxybinary.EnvName])
	require.Equal(t, "value", normalized.Env["KEEP"])
	require.Equal(t, "/parent-only/ccplant", original.Env[proxybinary.EnvName])
}

func TestSessionPodTemplateRetainsInjectedCLI(t *testing.T) {
	manager := newWorkloadTestManager(t, false)
	manager.k8sConfig.CLIImage = "example/cli:v2"
	deployment, err := manager.buildDeployment(context.Background(), newWorkloadTestSession(), &entities.RunServerRequest{Environment: map[string]string{}})
	require.NoError(t, err)
	generated := &deployment.Spec.Template
	patched := generated.DeepCopy()
	patched.Spec.InitContainers = nil
	patched.Spec.Volumes = nil
	main := findContainerByName(patched.Spec.Containers, "agentapi")
	main.VolumeMounts = nil
	main.Env = []corev1.EnvVar{{Name: proxybinary.EnvName, Value: "/wrong/ccplant"}}
	restoreSessionPodTemplateInvariants(patched, generated)
	require.NotNil(t, findContainerByName(patched.Spec.InitContainers, "install-ccplant-cli"))
	require.Contains(t, main.VolumeMounts, corev1.VolumeMount{Name: "ccplant-cli", MountPath: "/opt/ccplant/bin", ReadOnly: true})
	require.Contains(t, main.Env, corev1.EnvVar{Name: proxybinary.EnvName, Value: sessionCLIPath})
	require.Len(t, patched.Spec.Volumes, 1)
	require.NotNil(t, patched.Spec.Volumes[0].EmptyDir)
}

func TestStockTemplateHashTracksCLIRelease(t *testing.T) {
	manager := newWorkloadTestManager(t, false)
	manager.k8sConfig.CLIImage = "example/cli:v1"
	before, err := manager.stockPodTemplateHash(context.Background(), false)
	require.NoError(t, err)
	manager.k8sConfig.CLIImage = "example/cli:v2"
	after, err := manager.stockPodTemplateHash(context.Background(), false)
	require.NoError(t, err)
	require.NotEqual(t, before, after, "stock runners must refresh when only the CLI release changes")
	require.Equal(t, "test-image:latest", manager.k8sConfig.Image)
}
