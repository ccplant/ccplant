package services

import (
	"fmt"

	"github.com/takutakahashi/agentapi-proxy/internal/domain/entities"
	"github.com/takutakahashi/agentapi-proxy/pkg/sessionsettings"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
)

// The DinD daemon listens on loopback TCP port 2375 (no TLS) so it is reachable
// only by containers in the same Pod network namespace.
// The main container gets DOCKER_HOST=tcp://127.0.0.1:2375 to connect to the daemon.
func (m *KubernetesSessionManager) buildDinDContainers(docker *sessionsettings.DockerConfig) (*corev1.Container, []corev1.EnvVar, []corev1.Volume) {
	dindImage := m.k8sConfig.DinDImage
	if dindImage == "" {
		dindImage = "docker:dind"
	}

	trueVal := true
	falseVal := false
	rootUID := int64(0)

	dindArgs := []string{"dockerd", "--host=tcp://127.0.0.1:2375", "--tls=false"}
	if docker != nil {
		for _, reg := range docker.Registries {
			if reg.Insecure && reg.Server != "" {
				dindArgs = append(dindArgs, "--insecure-registry="+reg.Server)
			}
		}
	}

	sidecar := corev1.Container{
		Name:            "docker-dind",
		Image:           dindImage,
		ImagePullPolicy: corev1.PullPolicy(m.k8sConfig.ImagePullPolicy),
		Args:            dindArgs,
		SecurityContext: &corev1.SecurityContext{
			Privileged:   &trueVal,
			RunAsUser:    &rootUID,
			RunAsNonRoot: &falseVal,
		},
		Resources: buildResourceRequirements(
			defaultIfEmpty(m.k8sConfig.DinDCPURequest, "2"),
			defaultIfEmpty(m.k8sConfig.DinDCPULimit, "2"),
			defaultIfEmpty(m.k8sConfig.DinDMemoryRequest, "2Gi"),
			defaultIfEmpty(m.k8sConfig.DinDMemoryLimit, "2Gi"),
		),
		VolumeMounts: []corev1.VolumeMount{
			{
				Name:      "docker-storage",
				MountPath: "/var/lib/docker",
			},
			{
				Name:      "workdir",
				MountPath: "/home/agentapi/workdir",
			},
		},
	}

	volumes := []corev1.Volume{
		{
			Name: "docker-storage",
			VolumeSource: corev1.VolumeSource{
				EmptyDir: &corev1.EmptyDirVolumeSource{},
			},
		},
	}

	// Resolve registry secret from per-session config.
	var registrySecretName string
	if docker != nil {
		for _, reg := range docker.Registries {
			if reg.SecretName != "" {
				registrySecretName = reg.SecretName
				break
			}
		}
	}

	if registrySecretName != "" {
		sidecar.VolumeMounts = append(sidecar.VolumeMounts, corev1.VolumeMount{
			Name:      "docker-registry-config",
			MountPath: "/root/.docker/config.json",
			SubPath:   "config.json",
			ReadOnly:  true,
		})
		volumes = append(volumes, corev1.Volume{
			Name: "docker-registry-config",
			VolumeSource: corev1.VolumeSource{
				Secret: &corev1.SecretVolumeSource{
					SecretName: registrySecretName,
					Optional:   boolPtr(true),
				},
			},
		})
	}

	// Set DOCKER_HOST in the main container so docker CLI commands connect to the DinD daemon.
	mainEnvVars := []corev1.EnvVar{
		{Name: "DOCKER_HOST", Value: "tcp://127.0.0.1:2375"},
	}

	return &sidecar, mainEnvVars, volumes
}

func defaultIfEmpty(s, def string) string {
	if s == "" {
		return def
	}
	return s
}

// buildResourceRequirements constructs a corev1.ResourceRequirements from string quantities.
// Any empty string is silently omitted, allowing partial specification.
func buildResourceRequirements(cpuReq, cpuLim, memReq, memLim string) corev1.ResourceRequirements {
	requests := corev1.ResourceList{}
	limits := corev1.ResourceList{}
	if cpuReq != "" {
		requests[corev1.ResourceCPU] = resource.MustParse(cpuReq)
	}
	if memReq != "" {
		requests[corev1.ResourceMemory] = resource.MustParse(memReq)
	}
	if cpuLim != "" {
		limits[corev1.ResourceCPU] = resource.MustParse(cpuLim)
	}
	if memLim != "" {
		limits[corev1.ResourceMemory] = resource.MustParse(memLim)
	}
	return corev1.ResourceRequirements{Requests: requests, Limits: limits}
}

// buildVolumes builds the volume configuration for the session pod
func (m *KubernetesSessionManager) buildVolumes(session *KubernetesSession) []corev1.Volume {
	// Build workdir volume - use PVC if enabled, otherwise EmptyDir
	var workdirVolume corev1.Volume
	if m.isPVCEnabled() {
		workdirVolume = corev1.Volume{
			Name: "workdir",
			VolumeSource: corev1.VolumeSource{
				PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{
					ClaimName: session.PVCName(),
				},
			},
		}
	} else {
		workdirVolume = corev1.Volume{
			Name: "workdir",
			VolumeSource: corev1.VolumeSource{
				EmptyDir: &corev1.EmptyDirVolumeSource{},
			},
		}
	}

	// Note: credentials are no longer mounted as a volume. Instead they are
	// embedded in SessionSettings.Credentials and written to ~/.codex/auth.json
	// by the provisioner at startup. The runCredentialsSync goroutine then
	// watches the file and syncs any changes back to the Secret.

	volumes := []corev1.Volume{
		// Workdir volume (PVC or EmptyDir based on configuration)
		workdirVolume,
		// dot-claude EmptyDir – used by main container for Claude Code settings
		{
			Name: "dot-claude",
			VolumeSource: corev1.VolumeSource{
				EmptyDir: &corev1.EmptyDirVolumeSource{},
			},
		},
		// Shared Docker CLI configuration. The provisioner writes registry
		// credentials here and executor-side docker commands consume them.
		{
			Name: "dot-docker",
			VolumeSource: corev1.VolumeSource{
				EmptyDir: &corev1.EmptyDirVolumeSource{},
			},
		},
	}

	// Add notification subscription Secret volume (source for init container)
	// Secret name follows the pattern: notification-subscriptions-{userID}
	notificationSecretName := fmt.Sprintf("notification-subscriptions-%s", sanitizeLabelValue(session.Request().UserID))
	volumes = append(volumes, corev1.Volume{
		Name: "notification-subscriptions-source",
		VolumeSource: corev1.VolumeSource{
			Secret: &corev1.SecretVolumeSource{
				SecretName: notificationSecretName,
				Optional:   boolPtr(true), // Optional - user may not have subscriptions
			},
		},
	})
	if request := session.Request(); request != nil {
		for index, storage := range request.Storages {
			if storage.Kind != entities.StorageKindObjectSnapshot || storage.Managed {
				continue
			}
			volume := corev1.Volume{Name: storageVolumeName(index)}
			volume.EmptyDir = &corev1.EmptyDirVolumeSource{}
			volumes = append(volumes, volume)
		}
	}

	// session-settings Secret – optional volume for Pod restart auto-provisioning.
	// Created after successful provisioning; not present on first startup.
	sessionSettingsSecretName := fmt.Sprintf("agentapi-session-%s-settings", session.id)
	optionalTrue := true
	volumes = append(volumes, corev1.Volume{
		Name: "session-settings",
		VolumeSource: corev1.VolumeSource{
			Secret: &corev1.SecretVolumeSource{
				SecretName: sessionSettingsSecretName,
				Optional:   &optionalTrue,
			},
		},
	})

	// Note: The "initial-message-state" EmptyDir volume is no longer needed because
	// the initial-message-sender sidecar has been removed. Initial message sending
	// is now handled internally by agent-provisioner.

	// No otelcol ConfigMap volume needed: otelcol runs as an in-process subprocess
	// and generates its config file at /tmp/otelcol-config.yaml at provisioning time.

	return volumes
}

// createService creates a Service for the session
