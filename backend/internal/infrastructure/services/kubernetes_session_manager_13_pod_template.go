package services

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"strings"

	"github.com/takutakahashi/agentapi-proxy/pkg/proxybinary"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/util/strategicpatch"
	"sigs.k8s.io/yaml"
)

func provisionerSciaEnvVars(envVars []corev1.EnvVar) []corev1.EnvVar {
	result := make([]corev1.EnvVar, len(envVars))
	copy(result, envVars)
	for i := range result {
		switch result[i].Name {
		case "NO_PROXY", "no_proxy":
			result[i].Value = sciaNoProxyBase
		case "SSL_CERT_FILE", "REQUESTS_CA_BUNDLE", "CURL_CA_BUNDLE", "GIT_SSL_CAINFO":
			// The combined bundle is created later by the provisioner. Use SCIA's
			// generated CA directly during the initial pull phase.
			result[i].Value = sciaCAPath
		}
	}
	return result
}

func sessionAffinity(value map[string]interface{}) (*corev1.Affinity, error) {
	if len(value) == 0 {
		return nil, nil
	}
	normalized, err := normalizeJSONValue(value)
	if err != nil {
		return nil, fmt.Errorf("failed to normalize session affinity: %w", err)
	}
	data, err := json.Marshal(normalized)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal session affinity: %w", err)
	}
	var affinity corev1.Affinity
	if err := json.Unmarshal(data, &affinity); err != nil {
		return nil, fmt.Errorf("failed to parse session affinity: %w", err)
	}
	return &affinity, nil
}

func normalizeJSONValue(value interface{}) (interface{}, error) {
	switch typed := value.(type) {
	case map[string]interface{}:
		normalized := make(map[string]interface{}, len(typed))
		for key, item := range typed {
			value, err := normalizeJSONValue(item)
			if err != nil {
				return nil, err
			}
			normalized[key] = value
		}
		return normalized, nil
	case map[interface{}]interface{}:
		normalized := make(map[string]interface{}, len(typed))
		for key, item := range typed {
			stringKey, ok := key.(string)
			if !ok {
				return nil, fmt.Errorf("affinity map key must be a string, got %T", key)
			}
			value, err := normalizeJSONValue(item)
			if err != nil {
				return nil, err
			}
			normalized[stringKey] = value
		}
		return normalized, nil
	case []interface{}:
		normalized := make([]interface{}, len(typed))
		for index, item := range typed {
			value, err := normalizeJSONValue(item)
			if err != nil {
				return nil, err
			}
			normalized[index] = value
		}
		return normalized, nil
	default:
		return value, nil
	}
}

func (m *KubernetesSessionManager) applySessionPodTemplateFile(template *corev1.PodTemplateSpec) error {
	if m.k8sConfig == nil || strings.TrimSpace(m.k8sConfig.SessionPodTemplateFile) == "" {
		return nil
	}

	filename := strings.TrimSpace(m.k8sConfig.SessionPodTemplateFile)
	data, err := os.ReadFile(filename)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("failed to read session pod template file %s: %w", filename, err)
	}
	if len(bytes.TrimSpace(data)) == 0 {
		return nil
	}

	originalJSON, err := json.Marshal(template)
	if err != nil {
		return fmt.Errorf("failed to marshal generated session pod template: %w", err)
	}
	patchJSON, err := yaml.YAMLToJSON(data)
	if err != nil {
		return fmt.Errorf("failed to parse session pod template file %s: %w", filename, err)
	}
	mergedJSON, err := strategicpatch.StrategicMergePatch(originalJSON, patchJSON, corev1.PodTemplateSpec{})
	if err != nil {
		return fmt.Errorf("failed to merge session pod template file %s: %w", filename, err)
	}

	generated := template.DeepCopy()
	var merged corev1.PodTemplateSpec
	if err := json.Unmarshal(mergedJSON, &merged); err != nil {
		return fmt.Errorf("failed to decode merged session pod template: %w", err)
	}
	restoreSessionPodTemplateInvariants(&merged, generated)
	*template = merged
	log.Printf("[K8S_SESSION] Applied session pod template file: %s", filename)
	return nil
}

func restoreSessionPodTemplateInvariants(template *corev1.PodTemplateSpec, generated *corev1.PodTemplateSpec) {
	if template.Labels == nil {
		template.Labels = map[string]string{}
	}
	for key, value := range generated.Labels {
		if strings.HasPrefix(key, "agentapi.proxy/") || key == "app.kubernetes.io/managed-by" {
			template.Labels[key] = value
		}
	}

	template.Spec.ServiceAccountName = generated.Spec.ServiceAccountName
	template.Spec.EnableServiceLinks = generated.Spec.EnableServiceLinks
	template.Spec.AutomountServiceAccountToken = generated.Spec.AutomountServiceAccountToken
	template.Spec.RestartPolicy = generated.Spec.RestartPolicy

	// Manager-owned sidecars must not be removed or weakened by a pod template.
	// Moli intentionally binds to loopback, so replacing its generated command or
	// security context could expose CDP outside the session pod.
	if moli := findContainerByName(generated.Spec.Containers, "moli"); moli != nil {
		if existing := findContainerByName(template.Spec.Containers, moli.Name); existing != nil {
			*existing = *moli
		} else {
			template.Spec.Containers = append(template.Spec.Containers, *moli)
		}
		restoreGeneratedVolume(&template.Spec.Volumes, generated.Spec.Volumes, "moli-tmp")
	}

	// Pod template customizations must retain the CLI needed to boot the runner.
	if cli := findContainerByName(generated.Spec.InitContainers, "install-ccplant-cli"); cli != nil {
		if existing := findContainerByName(template.Spec.InitContainers, cli.Name); existing != nil {
			*existing = *cli
		} else {
			template.Spec.InitContainers = append(template.Spec.InitContainers, *cli)
		}
		for _, volume := range generated.Spec.Volumes {
			if volume.Name != "ccplant-cli" {
				continue
			}
			found := false
			for i := range template.Spec.Volumes {
				if template.Spec.Volumes[i].Name == volume.Name {
					template.Spec.Volumes[i] = volume
					found = true
				}
			}
			if !found {
				template.Spec.Volumes = append(template.Spec.Volumes, volume)
			}
		}
	}

	generatedMain := findContainerByName(generated.Spec.Containers, "agentapi")
	if generatedMain == nil {
		return
	}
	for i := range template.Spec.Containers {
		if template.Spec.Containers[i].Name != "agentapi" {
			continue
		}
		if findContainerByName(generated.Spec.InitContainers, "install-ccplant-cli") != nil {
			main := &template.Spec.Containers[i]
			mount := corev1.VolumeMount{Name: "ccplant-cli", MountPath: "/opt/ccplant/bin", ReadOnly: true}
			mounts := make([]corev1.VolumeMount, 0, len(main.VolumeMounts)+1)
			for _, existing := range main.VolumeMounts {
				if existing.Name != mount.Name && existing.MountPath != mount.MountPath {
					mounts = append(mounts, existing)
				}
			}
			main.VolumeMounts = append(mounts, mount)
			env := make([]corev1.EnvVar, 0, len(main.Env)+1)
			for _, existing := range main.Env {
				if existing.Name != proxybinary.EnvName {
					env = append(env, existing)
				}
			}
			main.Env = append(env, corev1.EnvVar{Name: proxybinary.EnvName, Value: sessionCLIPath})
		}
		template.Spec.Containers[i].Image = generatedMain.Image
		template.Spec.Containers[i].ImagePullPolicy = generatedMain.ImagePullPolicy
		template.Spec.Containers[i].WorkingDir = generatedMain.WorkingDir
		template.Spec.Containers[i].Command = generatedMain.Command
		template.Spec.Containers[i].Args = generatedMain.Args
		template.Spec.Containers[i].Ports = generatedMain.Ports
		template.Spec.Containers[i].LivenessProbe = generatedMain.LivenessProbe
		template.Spec.Containers[i].ReadinessProbe = generatedMain.ReadinessProbe
		return
	}
	template.Spec.Containers = append([]corev1.Container{*generatedMain}, template.Spec.Containers...)
}

func restoreGeneratedVolume(target *[]corev1.Volume, generated []corev1.Volume, name string) {
	for _, volume := range generated {
		if volume.Name != name {
			continue
		}
		for i := range *target {
			if (*target)[i].Name == name {
				(*target)[i] = volume
				return
			}
		}
		*target = append(*target, volume)
		return
	}
}

func findContainerByName(containers []corev1.Container, name string) *corev1.Container {
	for i := range containers {
		if containers[i].Name == name {
			return &containers[i]
		}
	}
	return nil
}

// NOTE: The credentialsSyncScript / credentialsSyncSidecarImage / buildCredentialsSyncSidecar
// have been removed. Credentials sync is now handled as a goroutine inside agent-provisioner
// (pkg/provisioner/provision.go) after user context is established. This ensures the
// UserID is always available (stock pool pods have an empty UserID at pod creation time).

// NOTE: The initialMessageSenderScript and buildInitialMessageSenderSidecar have been
// removed. Initial message sending is now handled inside agent-provisioner
// (pkg/provisioner/provision.go) after agentapi starts.

// NOTE: buildSlackSidecar (claude-posts sidecar) has been removed.
// claude-posts is launched as a subprocess inside the agentapi container by
// agent-provisioner (pkg/provisioner/provision.go). Running it as a sidecar
// caused duplicate execution alongside the in-process launch.

// defaultSlackBotTokenSecretKey is the default key within the Secret that holds the Slack bot token
