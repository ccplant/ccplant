package services

import (
	"context"
	"fmt"
	"log"

	"github.com/takutakahashi/agentapi-proxy/internal/domain/entities"
	"github.com/takutakahashi/agentapi-proxy/pkg/proxybinary"
	"github.com/takutakahashi/agentapi-proxy/pkg/sessionsettings"
	corev1 "k8s.io/api/core/v1"
)

func useRunnerCredentialSecret(envVars []corev1.EnvVar, req *entities.RunServerRequest, sessionID string) {
	if req.Environment["AGENTAPI_SESSION_RUNNER_TOKEN"] == "" {
		return
	}
	for i := range envVars {
		if envVars[i].Name == "AGENTAPI_SESSION_RUNNER_TOKEN" {
			envVars[i].Value = ""
			envVars[i].ValueFrom = &corev1.EnvVarSource{SecretKeyRef: &corev1.SecretKeySelector{
				LocalObjectReference: corev1.LocalObjectReference{Name: runnerCredentialSecretName(sessionID)},
				Key:                  "runner-token",
			}}
		}
	}
}

func (m *KubernetesSessionManager) sessionResources() corev1.ResourceRequirements {
	return buildResourceRequirements(m.k8sConfig.CPURequest, m.k8sConfig.CPULimit, m.k8sConfig.MemoryRequest, m.k8sConfig.MemoryLimit)
}

func (m *KubernetesSessionManager) workdirPermissionInitContainer() *corev1.Container {
	if !m.isPVCEnabled() {
		return nil
	}
	command := []string{"chown", "-R", "999:999", "/home/agentapi/workdir"}
	if m.config.SessionPersistence.Backend == "volume" {
		command = []string{"/bin/sh", "-ec", "mkdir -p /home/agentapi/workdir/.agentapi/persistent-home/claude-projects /home/agentapi/workdir/.agentapi/persistent-home/codex /home/agentapi/workdir/.agentapi/persistent-home/pi-sessions /home/agentapi/workdir/.agentapi/persistent-home/herdr-core && chown -R 999:999 /home/agentapi/workdir"}
	}
	rootUID, runAsNonRoot := int64(0), false
	return &corev1.Container{
		Name: "prepare-workdir-permissions", Image: m.k8sConfig.Image, ImagePullPolicy: corev1.PullPolicy(m.k8sConfig.ImagePullPolicy), Command: command,
		SecurityContext: &corev1.SecurityContext{RunAsUser: &rootUID, RunAsNonRoot: &runAsNonRoot},
		VolumeMounts:    []corev1.VolumeMount{{Name: "workdir", MountPath: "/home/agentapi/workdir"}},
	}
}

func (m *KubernetesSessionManager) sessionDinD(req *entities.RunServerRequest) (*corev1.Container, []corev1.EnvVar, []corev1.Volume) {
	if req.Docker == nil || !req.Docker.Enabled {
		return nil, nil, nil
	}
	registries := make([]sessionsettings.RegistryConfig, 0, len(req.Docker.Registries))
	for _, registry := range req.Docker.Registries {
		registries = append(registries, sessionsettings.RegistryConfig{
			Server: registry.Server, Username: registry.Username, Password: registry.Password,
			SecretName: registry.SecretName, Insecure: registry.Insecure,
		})
	}
	return m.buildDinDContainers(&sessionsettings.DockerConfig{Enabled: true, Registries: registries})
}

type mainSessionContainerConfig struct {
	session                  *KubernetesSession
	request                  *entities.RunServerRequest
	env, sandboxEnv, dindEnv []corev1.EnvVar
	envFrom                  []corev1.EnvFromSource
}

func (m *KubernetesSessionManager) buildMainSessionContainer(config mainSessionContainerConfig) corev1.Container {
	container := corev1.Container{
		Name: "agentapi", Image: m.k8sConfig.Image, ImagePullPolicy: corev1.PullPolicy(m.k8sConfig.ImagePullPolicy), WorkingDir: "/home/agentapi/workdir",
		Env: append(append(config.env, config.sandboxEnv...), config.dindEnv...), EnvFrom: config.envFrom, Resources: m.sessionResources(),
		VolumeMounts: m.buildMainContainerVolumeMounts(config.session, config.request), Command: []string{"/usr/bin/tini"},
		Args:           []string{"-g", "--", "/bin/sh", "-c", "exec " + proxybinary.ShellReference() + " agent-provisioner --socket " + provisionerSocketPath},
		LivenessProbe:  &corev1.Probe{ProbeHandler: localUnixHTTPProbe("/livez", provisionerSocketPath), InitialDelaySeconds: 5, PeriodSeconds: 5},
		ReadinessProbe: &corev1.Probe{ProbeHandler: localUnixHTTPProbe("/healthz", provisionerSocketPath), InitialDelaySeconds: 2, PeriodSeconds: 2},
	}
	container.Env = append(container.Env,
		corev1.EnvVar{Name: "AGENTAPI_BIND_HOST", Value: "127.0.0.1"},
		corev1.EnvVar{Name: "AGENTAPI_LOCAL_SOCKET", Value: agentSocketPath},
		corev1.EnvVar{Name: "PROVISIONER_LOCAL_SOCKET", Value: provisionerSocketPath},
		corev1.EnvVar{Name: "NETWORK_FILTER_CONTROL_SOCKET", Value: nfaControlSocketPath},
	)
	return container
}

func (m *KubernetesSessionManager) sandboxContainers(ctx context.Context, req *entities.RunServerRequest) ([]corev1.Container, *corev1.Container, []corev1.EnvVar) {
	if req.Sandbox == nil {
		req.Sandbox = &entities.SandboxParams{Enabled: true}
	} else {
		req.Sandbox.Enabled = true
	}
	return m.buildSandboxContainers(m.resolveSandboxParams(ctx, req))
}

type sciaWorkload struct {
	initContainer *corev1.Container
	sidecar       *corev1.Container
	sandboxEnv    []corev1.EnvVar
}

func (m *KubernetesSessionManager) sessionSciaWorkload(ctx context.Context, req *entities.RunServerRequest) sciaWorkload {
	if !m.sciaSessionSidecarEnabled(req) {
		return sciaWorkload{}
	}
	var userToken string
	if apiKey := m.ensurePersonalAPIKey(ctx, req.UserID); apiKey != nil {
		userToken = apiKey.APIKey()
	}
	initContainer, sidecar, env := m.buildSciaSidecarContainers(req, true, userToken)
	return sciaWorkload{initContainer: initContainer, sidecar: sidecar, sandboxEnv: provisionerSciaEnvVars(env)}
}

func containersWithSidecars(main corev1.Container, sidecars ...*corev1.Container) []corev1.Container {
	containers := []corev1.Container{main}
	for _, sidecar := range sidecars {
		if sidecar != nil {
			containers = append(containers, *sidecar)
		}
	}
	return containers
}

func (m *KubernetesSessionManager) githubEnvFrom(sessionID string, req *entities.RunServerRequest) []corev1.EnvFromSource {
	var envFrom []corev1.EnvFromSource
	if req.GithubToken == "" && m.k8sConfig.GitHubSecretName != "" {
		envFrom = append(envFrom, secretEnvFrom(m.k8sConfig.GitHubSecretName))
	}
	if m.k8sConfig.GitHubConfigSecretName != "" {
		envFrom = append(envFrom, secretEnvFrom(m.k8sConfig.GitHubConfigSecretName))
		log.Printf("[K8S_SESSION] Mounting GitHub config Secret %s for session %s", m.k8sConfig.GitHubConfigSecretName, sessionID)
	}
	return envFrom
}

func secretEnvFrom(name string) corev1.EnvFromSource {
	return corev1.EnvFromSource{SecretRef: &corev1.SecretEnvSource{
		LocalObjectReference: corev1.LocalObjectReference{Name: name},
		Optional:             boolPtr(true),
	}}
}

func (m *KubernetesSessionManager) sessionTolerations() []corev1.Toleration {
	tolerations := make([]corev1.Toleration, 0, len(m.k8sConfig.Tolerations))
	for _, configured := range m.k8sConfig.Tolerations {
		tolerations = append(tolerations, corev1.Toleration{
			Key: configured.Key, Operator: corev1.TolerationOperator(configured.Operator), Value: configured.Value,
			Effect: corev1.TaintEffect(configured.Effect), TolerationSeconds: configured.TolerationSeconds,
		})
	}
	return tolerations
}

func (m *KubernetesSessionManager) sessionPodAnnotations() map[string]string {
	annotations := make(map[string]string)
	if !m.k8sConfig.OtelCollectorEnabled {
		return annotations
	}
	exporterPort := m.k8sConfig.OtelCollectorExporterPort
	if exporterPort <= 0 {
		exporterPort = 9090
	}
	annotations["prometheus.io/scrape"] = "true"
	annotations["prometheus.io/port"] = fmt.Sprintf("%d", exporterPort)
	annotations["prometheus.io/path"] = "/metrics"
	return annotations
}

func (m *KubernetesSessionManager) sessionServiceAccount() string {
	if m.k8sConfig.ServiceAccount != "" {
		return m.k8sConfig.ServiceAccount
	}
	return "agentapi-proxy-session"
}
