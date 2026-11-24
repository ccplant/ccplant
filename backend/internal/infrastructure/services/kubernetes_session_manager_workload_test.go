package services

import (
	"context"
	"path/filepath"
	"reflect"
	"testing"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"

	"github.com/takutakahashi/agentapi-proxy/internal/domain/entities"
	"github.com/takutakahashi/agentapi-proxy/pkg/config"
	"github.com/takutakahashi/agentapi-proxy/pkg/logger"
	"github.com/takutakahashi/agentapi-proxy/pkg/sessionsettings"
)

func newWorkloadTestManager(t *testing.T, pvcEnabled bool) *KubernetesSessionManager {
	t.Helper()
	ns := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "test-ns"}}
	k8sClient := fake.NewSimpleClientset(ns)
	cfg := &config.Config{
		KubernetesSession: config.KubernetesSessionConfig{
			Namespace:      "test-ns",
			Image:          "test-image:latest",
			BasePort:       9000,
			PVCEnabled:     boolPtrForTest(pvcEnabled),
			CPURequest:     "100m",
			CPULimit:       "1",
			MemoryRequest:  "128Mi",
			MemoryLimit:    "512Mi",
			PVCStorageSize: "1Gi",
		},
	}
	manager, err := NewKubernetesSessionManagerWithClient(cfg, false, logger.NewLogger(), k8sClient)
	if err != nil {
		t.Fatalf("Failed to create manager: %v", err)
	}
	return manager
}

func newWorkloadTestSession() *KubernetesSession {
	return NewKubernetesSession(
		"test-session",
		&entities.RunServerRequest{UserID: "test-user"},
		"agentapi-session-test-session",
		"agentapi-session-test-session-svc",
		"agentapi-session-test-session-pvc",
		"test-ns",
		9000,
		nil,
		nil,
	)
}

func TestSessionEnvironmentReceivesOnlyScopedControlCredential(t *testing.T) {
	manager := newWorkloadTestManager(t, false)
	manager.k8sConfig.ProvisionerToken = "control-plane-secret"
	session := newWorkloadTestSession()

	env := manager.buildEnvVars(session, session.Request())
	values := make(map[string]string, len(env))
	for _, item := range env {
		values[item.Name] = item.Value
	}
	want := deriveSessionControlToken("control-plane-secret", session.ID())
	if values["AGENTAPI_ALLOCATED_SESSION_ID"] != session.ID() {
		t.Fatalf("allocated session ID = %q, want %q", values["AGENTAPI_ALLOCATED_SESSION_ID"], session.ID())
	}
	if values["PROVISIONER_TOKEN"] != want {
		t.Fatal("session-scoped provisioner credential was not injected")
	}
	if controlToken := values["SESSION_CONTROL_TOKEN"]; controlToken != want {
		t.Fatalf("session control credential = %q, want session-scoped token", controlToken)
	}
	if values["PROVISIONER_TOKEN"] == manager.k8sConfig.ProvisionerToken {
		t.Fatal("control-plane credential was exposed to the session environment")
	}
}

func assertOwnedByService(t *testing.T, obj metav1.Object, serviceName string) {
	t.Helper()
	owners := obj.GetOwnerReferences()
	if len(owners) != 1 {
		t.Fatalf("Expected one owner reference on %s, got %d: %+v", obj.GetName(), len(owners), owners)
	}
	if owners[0].APIVersion != "v1" || owners[0].Kind != "Service" || owners[0].Name != serviceName {
		t.Fatalf("Expected %s to be owned by Service %s, got %+v", obj.GetName(), serviceName, owners[0])
	}
}

func TestNormalizeProvisionSettingsKeepsPublicIdentityAndUsesManagerPersistence(t *testing.T) {
	manager := newWorkloadTestManager(t, false)
	manager.config.SessionPersistence.Backend = "s3"
	original := &sessionsettings.SessionSettings{Session: sessionsettings.SessionMeta{
		ID:                 "public-session",
		PersistenceEnabled: false,
	}}

	normalized := manager.normalizeProvisionSettings(original)

	if normalized.Session.ID != "public-session" {
		t.Fatalf("session ID = %q, want stable public-session", normalized.Session.ID)
	}
	if !normalized.Session.PersistenceEnabled {
		t.Fatal("manager persistence must enable implicit restore")
	}
	if original.Session.ID != "public-session" || original.Session.PersistenceEnabled {
		t.Fatal("normalization mutated parent-owned settings")
	}
}

func TestVolumePersistenceUsesPerSessionWorkdirPVC(t *testing.T) {
	manager := newWorkloadTestManager(t, false)
	manager.config.SessionPersistence.Backend = "volume"
	session := newWorkloadTestSession()
	if !manager.isPVCEnabled() {
		t.Fatal("volume persistence must enable the per-session workdir PVC")
	}
	env := manager.buildEnvVars(session, session.Request())
	for _, item := range env {
		if item.Name == "AGENTAPI_SESSION_STATE_VOLUME_PATH" {
			if item.Value != "/home/agentapi/workdir/.agentapi/session-state.tar.zst" {
				t.Fatalf("volume path = %q", item.Value)
			}
			return
		}
	}
	t.Fatal("session state volume path was not injected")
}

func TestGetSessionRepairsStockAdoptionMetadataFromService(t *testing.T) {
	manager := newWorkloadTestManager(t, true)
	manager.config.SessionPersistence.Backend = "volume"
	session := newWorkloadTestSession()
	session.SetUserID("")
	session.Request().AgentType = ""
	manager.sessions[session.ID()] = session

	_, err := manager.client.CoreV1().Services("test-ns").Create(context.Background(), &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      session.ServiceName(),
			Namespace: "test-ns",
			Labels: map[string]string{
				"agentapi.proxy/session-id": session.ID(),
				"agentapi.proxy/user-id":    "adopted-user",
			},
			Annotations: map[string]string{"agentapi.proxy/agent-type": "codex-acp"},
		},
	}, metav1.CreateOptions{})
	if err != nil {
		t.Fatal(err)
	}

	restored := manager.GetSession(session.ID())
	if restored == nil {
		t.Fatal("session was not returned")
	}
	if restored.UserID() != "adopted-user" {
		t.Fatalf("user ID = %q, want adopted-user", restored.UserID())
	}
	ks, ok := restored.(*KubernetesSession)
	if !ok {
		t.Fatalf("session type = %T, want *KubernetesSession", restored)
	}
	if ks.Request().AgentType != "codex-acp" {
		t.Fatalf("agent type = %q, want codex-acp", ks.Request().AgentType)
	}
	if !manager.requiresSessionCheckpoint(ks) {
		t.Fatal("repaired ACP session must require a checkpoint")
	}
}

func TestSessionWorkloadReadyFallsBackToReadyPodWhenDeploymentStatusLags(t *testing.T) {
	manager := newWorkloadTestManager(t, true)
	session := newWorkloadTestSession()
	ctx := context.Background()

	_, err := manager.client.AppsV1().Deployments("test-ns").Create(ctx, &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{Name: session.DeploymentName(), Namespace: "test-ns"},
	}, metav1.CreateOptions{})
	if err != nil {
		t.Fatalf("Failed to create deployment: %v", err)
	}
	_, err = manager.client.CoreV1().Pods("test-ns").Create(ctx, &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      session.DeploymentName() + "-pod",
			Namespace: "test-ns",
			Labels:    map[string]string{"agentapi.proxy/session-id": session.ID()},
		},
		Status: corev1.PodStatus{
			Phase: corev1.PodRunning,
			Conditions: []corev1.PodCondition{{
				Type:   corev1.PodReady,
				Status: corev1.ConditionTrue,
			}},
		},
	}, metav1.CreateOptions{})
	if err != nil {
		t.Fatalf("Failed to create ready pod: %v", err)
	}

	ready, err := manager.isSessionWorkloadReady(ctx, session)
	if err != nil {
		t.Fatalf("isSessionWorkloadReady() error = %v", err)
	}
	if !ready {
		t.Fatal("Expected ready Pod to make workload ready while Deployment status lags")
	}
}

func TestCreateSessionWorkloadWithoutPVCUsesPodRestartPolicyNever(t *testing.T) {
	manager := newWorkloadTestManager(t, false)
	session := newWorkloadTestSession()

	if err := manager.createSessionWorkload(context.Background(), session, session.Request()); err != nil {
		t.Fatalf("Failed to create workload: %v", err)
	}

	pod, err := manager.client.CoreV1().Pods("test-ns").Get(context.Background(), session.DeploymentName(), metav1.GetOptions{})
	if err != nil {
		t.Fatalf("Expected pod to be created: %v", err)
	}
	if pod.Spec.RestartPolicy != corev1.RestartPolicyNever {
		t.Fatalf("Expected restartPolicy Never, got %s", pod.Spec.RestartPolicy)
	}
	if pod.Spec.Volumes[0].PersistentVolumeClaim != nil {
		t.Fatal("Expected workdir volume to avoid PVC when PVC is disabled")
	}
	if pod.Spec.Volumes[0].EmptyDir == nil {
		t.Fatal("Expected workdir volume to use EmptyDir when PVC is disabled")
	}

	deployments, err := manager.client.AppsV1().Deployments("test-ns").List(context.Background(), metav1.ListOptions{})
	if err != nil {
		t.Fatalf("Failed to list deployments: %v", err)
	}
	if len(deployments.Items) != 0 {
		t.Fatalf("Expected no deployments, got %d", len(deployments.Items))
	}
}

func TestCreateSessionWorkloadWithoutPVCKeepsPodRestartPolicyNeverWithExecutor(t *testing.T) {
	manager := newWorkloadTestManager(t, false)
	manager.k8sConfig.CLIImage = "example/cli:v1"
	manager.k8sConfig.ExecutorEnabled = true
	session := newWorkloadTestSession()

	if err := manager.createSessionWorkload(context.Background(), session, session.Request()); err != nil {
		t.Fatalf("Failed to create workload: %v", err)
	}

	pod, err := manager.client.CoreV1().Pods("test-ns").Get(context.Background(), session.DeploymentName(), metav1.GetOptions{})
	if err != nil {
		t.Fatalf("Expected pod to be created: %v", err)
	}
	if pod.Spec.RestartPolicy != corev1.RestartPolicyNever {
		t.Fatalf("Expected restartPolicy Never, got %s", pod.Spec.RestartPolicy)
	}
	executor := findContainerByName(pod.Spec.InitContainers, "executor")
	if executor == nil || executor.RestartPolicy == nil || *executor.RestartPolicy != corev1.ContainerRestartPolicyAlways {
		t.Fatalf("executor restartPolicy = %v, want Always", executor)
	}
}

func TestBuildDeploymentMountsGitHubConfigSecretWithoutAuthentication(t *testing.T) {
	manager := newWorkloadTestManager(t, false)
	manager.k8sConfig.GitHubConfigSecretName = "github-config"
	session := newWorkloadTestSession()

	deployment, err := manager.buildDeployment(context.Background(), session, session.Request())
	if err != nil {
		t.Fatalf("buildDeployment() error = %v", err)
	}

	envFrom := deployment.Spec.Template.Spec.Containers[0].EnvFrom
	if len(envFrom) != 1 {
		t.Fatalf("EnvFrom = %+v, want only github-config", envFrom)
	}
	if envFrom[0].SecretRef == nil || envFrom[0].SecretRef.Name != "github-config" {
		t.Fatalf("EnvFrom[0] = %+v, want github-config SecretRef", envFrom[0])
	}
	if envFrom[0].SecretRef.Optional == nil || !*envFrom[0].SecretRef.Optional {
		t.Fatalf("github-config SecretRef Optional = %v, want true", envFrom[0].SecretRef.Optional)
	}
}

func TestBuildDeploymentPreparesPersistentWorkdirPermissions(t *testing.T) {
	t.Run("PVC", func(t *testing.T) {
		manager := newWorkloadTestManager(t, true)
		deployment, err := manager.buildDeployment(context.Background(), newWorkloadTestSession(), newWorkloadTestSession().Request())
		if err != nil {
			t.Fatal(err)
		}

		init := findContainerByName(deployment.Spec.Template.Spec.InitContainers, "prepare-workdir-permissions")
		if init == nil {
			t.Fatalf("initContainers = %#v, want workdir permission preparation", deployment.Spec.Template.Spec.InitContainers)
		}
		if got, want := init.Command, []string{"chown", "-R", "999:999", "/home/agentapi/workdir"}; !reflect.DeepEqual(got, want) {
			t.Fatalf("command = %#v, want %#v", got, want)
		}
		if init.SecurityContext == nil || init.SecurityContext.RunAsUser == nil || *init.SecurityContext.RunAsUser != 0 {
			t.Fatalf("securityContext = %#v, want root user", init.SecurityContext)
		}
		if !hasMount(init.VolumeMounts, "workdir", "/home/agentapi/workdir", "") {
			t.Fatalf("mounts = %#v, want workdir mount", init.VolumeMounts)
		}
	})

	t.Run("ephemeral", func(t *testing.T) {
		manager := newWorkloadTestManager(t, false)
		deployment, err := manager.buildDeployment(context.Background(), newWorkloadTestSession(), newWorkloadTestSession().Request())
		if err != nil {
			t.Fatal(err)
		}
		if init := findContainerByName(deployment.Spec.Template.Spec.InitContainers, "prepare-workdir-permissions"); init != nil {
			t.Fatalf("unexpected workdir permission init container: %#v", init)
		}
	})
}

func TestBuildDeploymentAddsMemoryIsolatedExecutor(t *testing.T) {
	manager := newWorkloadTestManager(t, false)
	manager.k8sConfig.CLIImage = "example/cli:v1"
	manager.k8sConfig.ExecutorEnabled = true
	manager.k8sConfig.ExecutorMemoryRequest = "256Mi"
	manager.k8sConfig.ExecutorMemoryLimit = "2Gi"
	session := newWorkloadTestSession()

	deployment, err := manager.buildDeployment(context.Background(), session, session.Request())
	if err != nil {
		t.Fatal(err)
	}
	containers := deployment.Spec.Template.Spec.Containers
	executor := findContainerByName(deployment.Spec.Template.Spec.InitContainers, "executor")
	if executor == nil {
		t.Fatalf("initContainers = %#v, want executor native sidecar", deployment.Spec.Template.Spec.InitContainers)
	}
	if executor.RestartPolicy == nil || *executor.RestartPolicy != corev1.ContainerRestartPolicyAlways {
		t.Fatalf("executor restartPolicy = %v, want Always", executor.RestartPolicy)
	}
	if got := executor.Resources.Limits.Memory().String(); got != "2Gi" {
		t.Fatalf("executor memory limit = %q", got)
	}
	main := containers[0]
	if !hasEnvValue(main.Env, "CCPLANT_EXECUTOR_REQUIRED", "1") {
		t.Fatal("agent container does not require the executor")
	}
	if !hasEnvValue(main.Env, "XDG_RUNTIME_DIR", defaultHerdrRuntimePath) {
		t.Fatal("agent container does not use the shared herdr runtime directory")
	}
	if !hasEnvValue(executor.Env, "XDG_RUNTIME_DIR", defaultHerdrRuntimePath) {
		t.Fatal("executor container does not use the shared herdr runtime directory")
	}
	if !hasMount(main.VolumeMounts, "herdr-runtime", defaultHerdrRuntimePath, "") {
		t.Fatalf("agent mounts = %#v, want shared herdr runtime", main.VolumeMounts)
	}
	if !hasMount(executor.VolumeMounts, "herdr-runtime", defaultHerdrRuntimePath, "") {
		t.Fatalf("executor mounts = %#v, want shared herdr runtime", executor.VolumeMounts)
	}
	if !hasMount(main.VolumeMounts, "agent-local-sockets", filepath.Dir(provisionerSocketPath), "") {
		t.Fatalf("agent mounts = %#v, want shared local sockets", main.VolumeMounts)
	}
	if !hasMount(executor.VolumeMounts, "agent-local-sockets", filepath.Dir(provisionerSocketPath), "") {
		t.Fatalf("executor mounts = %#v, want shared local sockets", executor.VolumeMounts)
	}
	prepareSockets := findContainerByName(deployment.Spec.Template.Spec.InitContainers, "prepare-agent-local-sockets")
	if prepareSockets == nil {
		t.Fatal("initContainers do not prepare shared local socket permissions")
	}
	if !hasMount(prepareSockets.VolumeMounts, "agent-local-sockets", filepath.Dir(provisionerSocketPath), "") {
		t.Fatalf("socket preparation mounts = %#v, want shared local sockets", prepareSockets.VolumeMounts)
	}
	if !hasMount(main.VolumeMounts, "ccplant-cli", "/bin/bash", "ccplant") {
		t.Fatalf("agent mounts = %#v, want ccplant mounted over /bin/bash", main.VolumeMounts)
	}
	if !hasMount(main.VolumeMounts, "dot-docker", "/home/agentapi/.docker", "") {
		t.Fatalf("agent mounts = %#v, want shared Docker config", main.VolumeMounts)
	}
	if !hasMount(executor.VolumeMounts, "dot-docker", "/home/agentapi/.docker", "") {
		t.Fatalf("executor mounts = %#v, want shared Docker config", executor.VolumeMounts)
	}
}
