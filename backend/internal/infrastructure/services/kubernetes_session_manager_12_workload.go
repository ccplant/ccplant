package services

import (
	"context"
	"fmt"
	"path/filepath"

	"github.com/takutakahashi/agentapi-proxy/internal/domain/entities"
	portrepos "github.com/takutakahashi/agentapi-proxy/internal/usecases/ports/repositories"
	"github.com/takutakahashi/agentapi-proxy/pkg/proxybinary"
	"github.com/takutakahashi/agentapi-proxy/pkg/telemetry"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// GetMessages retrieves conversation history from a session
func (m *KubernetesSessionManager) GetMessages(ctx context.Context, id string) ([]portrepos.Message, error) {
	return telemetry.Operation(ctx, "services.KubernetesSessionManager.GetMessages", func(ctx context.Context) ([]portrepos.Message, error) {
		return m.getMessages(ctx, id)
	})
}

func (m *KubernetesSessionManager) getMessages(ctx context.Context, id string) ([]portrepos.Message, error) {
	if m.GetSession(id) == nil {
		return nil, fmt.Errorf("session not found: %s", id)
	}
	return nil, fmt.Errorf("session history is available only through the outbound runtime connection")
}

// createPVC creates a PersistentVolumeClaim for the session
func (m *KubernetesSessionManager) createPVC(ctx context.Context, session *KubernetesSession) error {
	storageSize := resource.MustParse(m.k8sConfig.PVCStorageSize)

	pvc := &corev1.PersistentVolumeClaim{
		ObjectMeta: metav1.ObjectMeta{
			Name:            session.PVCName(),
			Namespace:       m.namespace,
			Labels:          m.buildLabels(session),
			OwnerReferences: m.sessionServiceOwnerReferences(ctx, session.id),
		},
		Spec: corev1.PersistentVolumeClaimSpec{
			AccessModes: []corev1.PersistentVolumeAccessMode{
				corev1.ReadWriteOnce,
			},
			Resources: corev1.VolumeResourceRequirements{
				Requests: corev1.ResourceList{
					corev1.ResourceStorage: storageSize,
				},
			},
		},
	}

	// Set storage class if specified
	if m.k8sConfig.PVCStorageClass != "" {
		pvc.Spec.StorageClassName = &m.k8sConfig.PVCStorageClass
	}

	created, err := m.client.CoreV1().PersistentVolumeClaims(m.namespace).Create(ctx, pvc, metav1.CreateOptions{})
	if err != nil {
		return err
	}
	if created == nil {
		return fmt.Errorf("create session PVC returned no object")
	}
	return nil
}

// createSessionWorkload creates the Kubernetes workload for the session.
// PVC-backed sessions keep using a Deployment so they can recover from pod
// restarts. Sessions without a PVC are intentionally ephemeral and run as a
// single Pod with restartPolicy=Never.
func (m *KubernetesSessionManager) createSessionWorkload(ctx context.Context, session *KubernetesSession, req *entities.RunServerRequest) error {
	if m.isPVCEnabled() {
		return m.createDeployment(ctx, session, req)
	}
	return m.createPod(ctx, session, req)
}

// createDeployment creates a Deployment for the session
func (m *KubernetesSessionManager) createDeployment(ctx context.Context, session *KubernetesSession, req *entities.RunServerRequest) error {
	deployment, err := m.buildDeployment(ctx, session, req)
	if err != nil {
		return err
	}
	created, err := m.client.AppsV1().Deployments(m.namespace).Create(ctx, deployment, metav1.CreateOptions{})
	if err != nil {
		return err
	}
	if created == nil {
		return fmt.Errorf("create session Deployment returned no object")
	}
	return nil
}

// createPod creates a standalone Pod for an ephemeral session.
func (m *KubernetesSessionManager) createPod(ctx context.Context, session *KubernetesSession, req *entities.RunServerRequest) error {
	deployment, err := m.buildDeployment(ctx, session, req)
	if err != nil {
		return err
	}
	podTemplate := deployment.Spec.Template
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:            session.DeploymentName(),
			Namespace:       m.namespace,
			Labels:          podTemplate.Labels,
			Annotations:     podTemplate.Annotations,
			OwnerReferences: m.sessionServiceOwnerReferences(ctx, session.id),
		},
		Spec: podTemplate.Spec,
	}
	pod.Spec.RestartPolicy = corev1.RestartPolicyNever

	created, err := m.client.CoreV1().Pods(m.namespace).Create(ctx, pod, metav1.CreateOptions{})
	if err != nil {
		return err
	}
	if created == nil {
		return fmt.Errorf("create session Pod returned no object")
	}
	return nil
}

const sessionCLIPath = "/opt/ccplant/bin/ccplant"
const defaultExecutorSocketPath = "/run/ccplant-executor/executor.sock"
const defaultHerdrRuntimePath = "/run/ccplant-herdr"

func (m *KubernetesSessionManager) sessionBinaryPath() string {
	if m.k8sConfig.CLIImage != "" {
		return sessionCLIPath
	}
	return proxybinary.Resolve(m.config.BinaryPath)
}

// buildDeployment builds the Deployment object used directly for PVC-backed
// sessions and as a Pod template source for ephemeral sessions.
func (m *KubernetesSessionManager) buildDeployment(ctx context.Context, session *KubernetesSession, req *entities.RunServerRequest) (*appsv1.Deployment, error) {
	labels := m.buildLabels(session)
	envVars := m.buildEnvVars(session, req)
	useRunnerCredentialSecret(envVars, req, session.id)
	replicas := int32(1)

	// Build init containers and sandbox sidecar.
	// Sandbox (network filter) is now always enabled - it cannot be opted out.
	sandboxEnabled := true
	initContainers, sandboxSidecar, sandboxEnvVars := m.sandboxContainers(ctx, req)
	if permissionContainer := m.workdirPermissionInitContainer(); permissionContainer != nil {
		initContainers = append(initContainers, *permissionContainer)
	}

	scia := m.sessionSciaWorkload(ctx, req)
	if scia.initContainer != nil {
		initContainers = append(initContainers, *scia.initContainer)
		envVars = append(envVars, corev1.EnvVar{Name: "AGENTAPI_SCIA_SESSION_SIDECAR_ENABLED", Value: "true"})
		sandboxEnvVars = scia.sandboxEnv
	}

	// Build DinD sidecar if Docker-in-Docker is enabled.
	dindEnabled := req.Docker != nil && req.Docker.Enabled
	dindSidecar, dindEnvVars, dindVolumes := m.sessionDinD(req)

	// Determine working directory
	// Always use /home/agentapi/workdir as base; clone-repo will create /home/agentapi/workdir/repo
	// Setting workingDir to repo path would cause Kubernetes to pre-create the dir as root
	workingDir := "/home/agentapi/workdir"

	envFrom := m.githubEnvFrom(session.id, req)

	// Build container spec.
	// The container runs agent-provisioner, which serves local health/status
	// endpoints and pulls provision requests from the proxy internal API.
	container := m.buildMainSessionContainer(mainSessionContainerConfig{
		session: session, request: req, env: envVars, sandboxEnv: sandboxEnvVars, dindEnv: dindEnvVars, envFrom: envFrom,
	})
	var moliSidecar *corev1.Container
	if m.k8sConfig.MoliEnabled {
		moliSidecar = m.buildMoliSidecar()
		endpoint := fmt.Sprintf("http://127.0.0.1:%d", m.k8sConfig.MoliPort)
		container.Env = append(container.Env,
			corev1.EnvVar{Name: "MOLI_CDP_ENDPOINT", Value: endpoint},
			corev1.EnvVar{Name: "CDP_ENDPOINT", Value: endpoint},
		)
	}
	executorEnabled := m.k8sConfig.ExecutorEnabled && m.k8sConfig.CLIImage != ""
	if executorEnabled {
		container.Env = append(container.Env,
			corev1.EnvVar{Name: "CCPLANT_EXECUTOR_REQUIRED", Value: "1"},
			corev1.EnvVar{Name: "CCPLANT_EXECUTOR_SOCKET", Value: defaultExecutorSocketPath},
			// Shell commands run in the executor sidecar. Share herdr-core's
			// Unix socket so hc launched there reaches the session's core instead
			// of starting an isolated core inside the sidecar.
			corev1.EnvVar{Name: "XDG_RUNTIME_DIR", Value: defaultHerdrRuntimePath},
		)
		container.VolumeMounts = append(container.VolumeMounts,
			corev1.VolumeMount{Name: "executor-socket", MountPath: filepath.Dir(defaultExecutorSocketPath)},
			corev1.VolumeMount{Name: "herdr-runtime", MountPath: defaultHerdrRuntimePath},
			corev1.VolumeMount{Name: "agent-local-sockets", MountPath: filepath.Dir(provisionerSocketPath)},
		)
	}

	// Build volumes
	volumes := m.buildVolumes(session)
	if sandboxEnabled {
		volumes = append(volumes,
			corev1.Volume{Name: "sandbox-iptables", VolumeSource: corev1.VolumeSource{EmptyDir: &corev1.EmptyDirVolumeSource{}}},
			corev1.Volume{Name: "nfa-runtime", VolumeSource: corev1.VolumeSource{EmptyDir: &corev1.EmptyDirVolumeSource{}}},
		)
		container.VolumeMounts = append(container.VolumeMounts, corev1.VolumeMount{Name: "nfa-runtime", MountPath: "/run/nfa"})
		if sandboxSidecar != nil {
			sandboxSidecar.VolumeMounts = append(sandboxSidecar.VolumeMounts, corev1.VolumeMount{Name: "nfa-runtime", MountPath: "/run/nfa"})
		}
	}
	if m.sciaSessionSidecarEnabled(req) {
		volumes = append(volumes,
			corev1.Volume{
				Name: "scia-config",
				VolumeSource: corev1.VolumeSource{
					EmptyDir: &corev1.EmptyDirVolumeSource{},
				},
			},
			corev1.Volume{
				Name: "scia-mitm-ca",
				VolumeSource: corev1.VolumeSource{
					EmptyDir: &corev1.EmptyDirVolumeSource{},
				},
			},
		)
	}
	if dindEnabled {
		volumes = append(volumes, dindVolumes...)
	}
	if moliSidecar != nil {
		volumes = append(volumes, corev1.Volume{Name: "moli-tmp", VolumeSource: corev1.VolumeSource{EmptyDir: &corev1.EmptyDirVolumeSource{}}})
	}

	// Copy the release-specific CLI into a writable volume as the Pod user.
	// Agent assets and their image tag remain independent of application releases.
	// Mount the destination outside /opt/ccplant/bin in the init container: the
	// compatibility image's /usr/local/bin/ccplant symlink points there, and
	// mounting an empty volume over it would hide the binary we need to copy.
	if m.k8sConfig.CLIImage != "" {
		volumes = append(volumes, corev1.Volume{
			Name:         "ccplant-cli",
			VolumeSource: corev1.VolumeSource{EmptyDir: &corev1.EmptyDirVolumeSource{}},
		})
		initContainers = append(initContainers, corev1.Container{
			Name:            "install-ccplant-cli",
			Image:           m.k8sConfig.CLIImage,
			ImagePullPolicy: corev1.PullPolicy(m.k8sConfig.ImagePullPolicy),
			Command:         []string{"/bin/sh", "-ec", "cp -f /usr/local/bin/ccplant /ccplant-cli/ccplant && chmod 0555 /ccplant-cli/ccplant"},
			VolumeMounts:    []corev1.VolumeMount{{Name: "ccplant-cli", MountPath: "/ccplant-cli"}},
		})
		container.VolumeMounts = append(container.VolumeMounts, corev1.VolumeMount{
			Name: "ccplant-cli", MountPath: "/opt/ccplant/bin", ReadOnly: true,
		})
		if executorEnabled {
			container.VolumeMounts = append(container.VolumeMounts,
				corev1.VolumeMount{Name: "ccplant-cli", MountPath: "/bin/bash", SubPath: "ccplant", ReadOnly: true},
				corev1.VolumeMount{Name: "ccplant-cli", MountPath: "/usr/bin/bash", SubPath: "ccplant", ReadOnly: true},
			)
		}
	}
	if executorEnabled {
		volumes = append(volumes,
			corev1.Volume{Name: "executor-socket", VolumeSource: corev1.VolumeSource{EmptyDir: &corev1.EmptyDirVolumeSource{}}},
			corev1.Volume{Name: "herdr-runtime", VolumeSource: corev1.VolumeSource{EmptyDir: &corev1.EmptyDirVolumeSource{}}},
			corev1.Volume{Name: "agent-local-sockets", VolumeSource: corev1.VolumeSource{EmptyDir: &corev1.EmptyDirVolumeSource{}}},
		)
		rootUID := int64(0)
		falseVal := false
		initContainers = append(initContainers, corev1.Container{
			Name:            "prepare-agent-local-sockets",
			Image:           m.k8sConfig.Image,
			ImagePullPolicy: corev1.PullPolicy(m.k8sConfig.ImagePullPolicy),
			Command:         []string{"/bin/sh", "-ec", "chown 999:999 /tmp/ccplant && chmod 0700 /tmp/ccplant"},
			SecurityContext: &corev1.SecurityContext{RunAsUser: &rootUID, RunAsNonRoot: &falseVal},
			VolumeMounts: []corev1.VolumeMount{{
				Name: "agent-local-sockets", MountPath: filepath.Dir(provisionerSocketPath),
			}},
		})
	}

	// Run the executor as a native sidecar so kubelet restarts it independently
	// after failures such as OOM kills, even for ephemeral Pods whose Pod-level
	// restart policy remains Never.
	if executorEnabled {
		executorMounts := m.buildMainContainerVolumeMounts(session, req)
		executorMounts = append(executorMounts,
			corev1.VolumeMount{Name: "executor-socket", MountPath: filepath.Dir(defaultExecutorSocketPath)},
			corev1.VolumeMount{Name: "herdr-runtime", MountPath: defaultHerdrRuntimePath},
			corev1.VolumeMount{Name: "agent-local-sockets", MountPath: filepath.Dir(provisionerSocketPath)},
			corev1.VolumeMount{Name: "ccplant-cli", MountPath: "/opt/ccplant/bin", ReadOnly: true},
		)
		restartAlways := corev1.ContainerRestartPolicyAlways
		initContainers = append(initContainers, corev1.Container{
			Name: "executor", Image: m.k8sConfig.Image,
			ImagePullPolicy: corev1.PullPolicy(m.k8sConfig.ImagePullPolicy),
			WorkingDir:      workingDir,
			Command:         []string{"/usr/bin/tini"},
			Args:            []string{"-g", "--", sessionCLIPath, "executor-server", "--socket", defaultExecutorSocketPath},
			Env:             []corev1.EnvVar{{Name: "XDG_RUNTIME_DIR", Value: defaultHerdrRuntimePath}},
			VolumeMounts:    executorMounts,
			RestartPolicy:   &restartAlways,
			Resources: buildResourceRequirements(
				defaultIfEmpty(m.k8sConfig.ExecutorCPURequest, "10m"),
				defaultIfEmpty(m.k8sConfig.ExecutorCPULimit, "2"),
				defaultIfEmpty(m.k8sConfig.ExecutorMemoryRequest, "512Mi"),
				defaultIfEmpty(m.k8sConfig.ExecutorMemoryLimit, "4Gi"),
			),
		})
	}

	// Build containers list.
	// Note: credentials-sync is now handled as a goroutine inside agent-provisioner
	// (pkg/provisioner/provision.go) after user context is established, so the
	// UserID is always set correctly even for stock pool pods.
	containers := containersWithSidecars(container, sandboxSidecar, scia.sidecar, dindSidecar, moliSidecar)

	// Note: Initial message is now sent by agent-provisioner internally after agentapi
	// becomes ready. The initial-message-sender sidecar has been removed.

	// Note: The slack-integration sidecar (claude-posts) has been removed.
	// claude-posts is now launched as a subprocess inside the agentapi container
	// by agent-provisioner (pkg/provisioner/provision.go) after agentapi starts.
	// This avoids running claude-posts twice when stock sessions are used.

	// otelcol runs as a subprocess inside the agentapi container (in-process mode).
	// No sidecar is added here; the provisioner starts otelcol after user context
	// is established so that metrics labels are correct even with stock sessions.

	tolerations := m.sessionTolerations()
	podAnnotations := m.sessionPodAnnotations()

	affinity, err := sessionAffinity(m.k8sConfig.Affinity)
	if err != nil {
		return nil, err
	}
	sessionServiceAccount := m.sessionServiceAccount()

	deployment := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:            session.DeploymentName(),
			Namespace:       m.namespace,
			Labels:          labels,
			OwnerReferences: m.sessionServiceOwnerReferences(ctx, session.id),
		},
		Spec: appsv1.DeploymentSpec{
			Replicas: &replicas,
			Selector: &metav1.LabelSelector{
				MatchLabels: map[string]string{
					"agentapi.proxy/session-id": session.id,
				},
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels:      labels,
					Annotations: podAnnotations,
				},
				Spec: corev1.PodSpec{
					ServiceAccountName:           sessionServiceAccount,
					EnableServiceLinks:           boolPtr(!m.k8sConfig.DisableServiceLinks),
					AutomountServiceAccountToken: boolPtr(!m.k8sConfig.DisableServiceAccountToken),
					RestartPolicy:                corev1.RestartPolicyAlways,
					SecurityContext: &corev1.PodSecurityContext{
						FSGroup:    int64Ptr(999),
						RunAsUser:  int64Ptr(999),
						RunAsGroup: int64Ptr(999),
					},
					InitContainers: initContainers,
					Containers:     containers,
					Volumes:        volumes,
					NodeSelector:   m.k8sConfig.NodeSelector,
					Affinity:       affinity,
					Tolerations:    tolerations,
				},
			},
		},
	}
	if err := m.applySessionPodTemplateFile(&deployment.Spec.Template); err != nil {
		return nil, err
	}
	return deployment, nil
}
