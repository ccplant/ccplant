package services

import (
	"context"
	"fmt"
	"log"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/takutakahashi/agentapi-proxy/internal/domain/entities"
	"github.com/takutakahashi/agentapi-proxy/pkg/proxybinary"
	"github.com/takutakahashi/agentapi-proxy/pkg/sessionsettings"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
)

// GetSession returns a session by ID
// If the session is not in memory, it attempts to restore from Kubernetes Service
func (m *KubernetesSessionManager) GetSession(id string) entities.Session {
	// First, check memory
	m.mutex.RLock()
	session, exists := m.sessions[id]
	m.mutex.RUnlock()

	// Try to restore from Kubernetes Service to check for stale adoption metadata
	// even when the session is in memory. A session cached while it was a stock pod
	// has an empty user-id and agent type; both are required after adoption. In
	// particular, suspension uses the agent type to decide whether an ACP checkpoint
	// must be written before deleting the workload.
	if exists && session.UserID() != "" && session.Request() != nil && session.Request().AgentType != "" {
		return session
	}

	// Try to restore from Kubernetes Service
	serviceName := fmt.Sprintf("agentapi-session-%s-svc", id)
	svc, err := m.client.CoreV1().Services(m.namespace).Get(
		context.Background(), serviceName, metav1.GetOptions{})
	if err != nil {
		if !errors.IsNotFound(err) {
			log.Printf("[K8S_SESSION] Failed to get service %s: %v", serviceName, err)
		}
		// Pool runners belong to the Session Manager that created their
		// Kubernetes resources. A different Session Manager may share the
		// namespace and even observe the runner through aggregate pool inventory,
		// but must not restore it into its local session map or operate on it.
		if errors.IsNotFound(err) && m.config != nil && m.config.SessionManager.ID != "" {
			if m.config.SessionManager.RunnerPool != "" {
				log.Printf("[K8S_SESSION] Runner service missing for session=%s namespace=%s local_manager=%s instance=%s", id, m.namespace, sanitizeLabelValue(m.config.SessionManager.ID), os.Getenv("HOSTNAME"))
			}
			return nil
		}
		// If we can't reach K8s but have the session in memory, still return it
		if exists {
			return session
		}
		return nil
	}

	// Don't restore if Service is being deleted
	if svc.DeletionTimestamp != nil {
		if exists {
			return session
		}
		return nil
	}
	if m.config != nil && m.config.SessionManager.RunnerPool != "" {
		owner := svc.Labels["agentapi.proxy/session-manager-id"]
		if owner != "" && owner != sanitizeLabelValue(m.config.SessionManager.ID) {
			log.Printf("[K8S_SESSION] Refusing foreign runner session=%s owner_manager=%s local_manager=%s", id, owner, sanitizeLabelValue(m.config.SessionManager.ID))
			return nil
		}
	}

	// Repair adoption metadata from the canonical Service instead of doing a full
	// re-restore, which would start duplicate watcher goroutines.
	if exists {
		if session.UserID() == "" {
			if svcUserID := svc.Labels["agentapi.proxy/user-id"]; svcUserID != "" {
				log.Printf("[K8S_SESSION] GetSession: repairing stale user-id for session %s (was empty, now %s)", id, svcUserID)
				session.SetUserID(svcUserID)
			}
		}
		if session.Request() == nil {
			session.SetRequest(&entities.RunServerRequest{})
		}
		if session.Request().AgentType == "" {
			if agentType := restoreAgentTypeFromService(svc); agentType != "" {
				log.Printf("[K8S_SESSION] GetSession: repairing stale agent type for session %s (was empty, now %s)", id, agentType)
				session.Request().AgentType = agentType
			}
		}
		return session
	}

	// Restore session from Service
	restored := m.restoreSessionFromService(svc)
	if restored == nil {
		return nil
	}

	// Phase 2: overlay agentapi runtime status from Redis if available.
	// The Kubernetes Deployment can only tell us "active" or "unhealthy".
	// Redis may have a more precise runtime status (running/error/timeout).
	if m.statusEventRepo != nil {
		runtimeStatusOverrideFromRedis(m.statusEventRepo, restored)
	}

	return restored
}

// EnsureSessionWorkload lazily recreates a missing Pod/Deployment while the
// canonical Service, settings Secret, snapshot, and (when enabled) PVC remain.
// It is invoked by authenticated GET traffic to the session chat routes.
func (m *KubernetesSessionManager) EnsureSessionWorkload(ctx context.Context, id string) (entities.Session, bool, error) {
	session := m.GetSession(id)
	if session == nil {
		return nil, false, fmt.Errorf("session %s not found", id)
	}
	ks, ok := session.(*KubernetesSession)
	if !ok {
		return session, false, nil
	}
	svc, serviceErr := m.client.CoreV1().Services(m.namespace).Get(ctx, ks.ServiceName(), metav1.GetOptions{})
	if serviceErr == nil && svc.Annotations[restartHoldAnnotation] == "true" && ctx.Value(restartContextKey{}) != true {
		return session, false, fmt.Errorf("session is manually paused or restarting")
	}
	if err := serviceErr; err != nil {
		if errors.IsNotFound(err) {
			return nil, false, fmt.Errorf("canonical service for session %s not found", id)
		}
		return session, false, fmt.Errorf("get canonical service: %w", err)
	}
	readyEndpoint, err := m.sessionHasReadyEndpoint(ctx, ks.ServiceName())
	if err != nil {
		return session, false, err
	}
	if session.Status() == "suspended" {
		// EndpointSlice/Endpoints updates can lag behind workload deletion. The
		// persisted suspended state is authoritative during that window.
		readyEndpoint = false
	}

	if m.isPVCEnabled() {
		_, err := m.client.AppsV1().Deployments(m.namespace).Get(ctx, ks.DeploymentName(), metav1.GetOptions{})
		switch {
		case err == nil:
			if readyEndpoint {
				return session, false, nil
			}
			ks.SetStatus("starting")
			m.startWorkloadStatusWatchers(context.Background(), ks)
			return session, true, nil
		case !errors.IsNotFound(err):
			return session, false, fmt.Errorf("get deployment: %w", err)
		}
		if err := m.requireSessionSettingsSecret(ctx, ks); err != nil {
			return session, false, err
		}
		if err := m.preparePersistentWorkloadResume(ctx, ks); err != nil {
			return session, false, err
		}
		if err := m.clearSessionSuspendState(ctx, ks.ServiceName()); err != nil {
			return session, false, err
		}
		if err := m.createDeployment(ctx, ks, ks.Request()); err != nil && !errors.IsAlreadyExists(err) {
			return session, false, fmt.Errorf("recreate deployment: %w", err)
		}
	} else {
		_, err := m.client.CoreV1().Pods(m.namespace).Get(ctx, ks.DeploymentName(), metav1.GetOptions{})
		switch {
		case err == nil:
			if readyEndpoint {
				return session, false, nil
			}
			ks.SetStatus("starting")
			m.startWorkloadStatusWatchers(context.Background(), ks)
			return session, true, nil
		case !errors.IsNotFound(err):
			return session, false, fmt.Errorf("get pod: %w", err)
		}
		if err := m.requireSessionSettingsSecret(ctx, ks); err != nil {
			return session, false, err
		}
		if err := m.preparePersistentWorkloadResume(ctx, ks); err != nil {
			return session, false, err
		}
		if err := m.clearSessionSuspendState(ctx, ks.ServiceName()); err != nil {
			return session, false, err
		}
		if err := m.createPod(ctx, ks, ks.Request()); err != nil && !errors.IsAlreadyExists(err) {
			return session, false, fmt.Errorf("recreate pod: %w", err)
		}
	}

	ks.SetStatus("starting")
	log.Printf("[K8S_SESSION] Lazily recreated workload for session %s after chat access", id)
	m.startWorkloadStatusWatchers(context.Background(), ks)
	go m.scheduleSuspendWhenRestoredWorkloadReady(ks)
	return session, true, nil
}

// preparePersistentWorkloadResume makes a recreated persistent workload fail
// closed if its saved ACP conversation cannot be loaded. Initial workloads have
// no ResumeFrom value and may create a new conversation; once the workload has
// been removed, the stable proxy session ID becomes the required snapshot key.
func (m *KubernetesSessionManager) preparePersistentWorkloadResume(ctx context.Context, session *KubernetesSession) error {
	if ctx.Value(parentResumeContextKey{}) == true {
		return nil
	}
	settings, err := m.CurrentSessionSettings(ctx, session.ID())
	if err != nil {
		return fmt.Errorf("load persistent resume settings: %w", err)
	}
	if m.config.SessionPersistence.Backend == "" {
		return nil
	}
	if strings.TrimSpace(settings.Session.ResumeFrom) != "" {
		return nil
	}
	// The manager-local session ID is the allocation ID. Persisted ACP state is
	// keyed by the stable public session ID carried in the provision settings so
	// it remains available when a replacement allocation is created.
	settings.Session.ResumeFrom = settings.Session.ID
	if err := m.PrepareSessionResume(ctx, session.ID(), settings); err != nil {
		return fmt.Errorf("persist required session resume: %w", err)
	}
	return nil
}

func (m *KubernetesSessionManager) PrepareSessionResume(ctx context.Context, id string, settings *sessionsettings.SessionSettings) error {
	session, ok := m.GetSession(id).(*KubernetesSession)
	if !ok || session == nil {
		return fmt.Errorf("session %s not found", id)
	}
	settings = m.normalizeProvisionSettings(settings)
	name := strings.TrimSuffix(session.ServiceName(), "-svc") + "-settings"
	existing, getErr := m.client.CoreV1().Secrets(m.namespace).Get(ctx, name, metav1.GetOptions{})
	if getErr != nil && !errors.IsNotFound(getErr) {
		return fmt.Errorf("get restart settings secret %s: %w", name, getErr)
	}
	req := &entities.RunServerRequest{UserID: settings.Session.UserID, Scope: entities.ResourceScope(settings.Session.Scope), TeamID: settings.Session.TeamID, AgentType: settings.Session.AgentType, Teams: settings.Session.Teams, InitialMessage: settings.InitialMessage, ProvisionSettings: settings}
	for _, storage := range settings.Storages {
		resolved := entities.ResolvedStorageAttachment{
			StorageAttachment: entities.StorageAttachment{StorageID: storage.StorageID, StoragePath: storage.StoragePath, MountPath: storage.MountPath, ReadOnly: storage.ReadOnly, Managed: storage.Managed},
			Kind:              entities.StorageKind(storage.Kind), Revision: storage.Revision,
		}
		for _, file := range storage.Files {
			resolved.Files = append(resolved.Files, entities.StorageFile{Path: file.Path, Digest: file.Digest, Size: file.Size, Mode: file.Mode, Revision: file.Revision})
		}
		req.Storages = append(req.Storages, resolved)
	}
	if settings.Sandbox != nil {
		req.Sandbox = &entities.SandboxParams{Enabled: settings.Sandbox.Enabled, PolicyID: settings.Sandbox.PolicyID, AllowedDomains: settings.Sandbox.AllowedDomains, DeniedDomains: settings.Sandbox.DeniedDomains, CountMode: settings.Sandbox.CountMode}
	}
	if settings.Docker != nil {
		req.Docker = &entities.DockerParams{Enabled: settings.Docker.Enabled}
		for _, r := range settings.Docker.Registries {
			req.Docker.Registries = append(req.Docker.Registries, entities.DockerRegistry{Server: r.Server, Username: r.Username, Password: r.Password, SecretName: r.SecretName, Insecure: r.Insecure})
		}
	}

	if settings.Repository != nil {
		req.RepoInfo = &entities.RepositoryInfo{FullName: settings.Repository.FullName, CloneDir: settings.Repository.CloneDir, Branch: settings.Repository.Branch, PR: settings.Repository.PR}
	}
	session.SetRequest(req)
	session.SetProvisionSettings(settings)
	if settings.Session.AutoSuspendEnabled != nil {
		enabled := strconv.FormatBool(*settings.Session.AutoSuspendEnabled)
		seconds := strconv.FormatInt(int64(time.Duration(settings.Session.AutoSuspendMinutes)*time.Minute/time.Second), 10)
		patch := []byte(fmt.Sprintf(`{"metadata":{"annotations":{"%s":%q,"%s":%q,"%s":"resume"}}}`, sessionAutoSuspendEnabledAnnotation, enabled, sessionAutoSuspendIdleSecondsAnnotation, seconds, sessionAutoSuspendPolicySourceAnnotation))
		if _, err := m.client.CoreV1().Services(m.namespace).Patch(ctx, session.ServiceName(), types.MergePatchType, patch, metav1.PatchOptions{}); err != nil {
			return fmt.Errorf("persist resume auto-suspend policy: %w", err)
		}
	}
	if getErr == nil {
		yamlData, err := marshalMountedSessionSettings(settings)
		if err != nil {
			return fmt.Errorf("marshal restart settings: %w", err)
		}
		if existing.Data == nil {
			existing.Data = map[string][]byte{}
		}
		existing.Data["settings.yaml"] = yamlData
		_, err = m.client.CoreV1().Secrets(m.namespace).Update(ctx, existing, metav1.UpdateOptions{})
		return err
	}
	return m.createSessionSettingsSecretFromSettings(ctx, session, req, settings)
}

// normalizeProvisionSettings applies execution-plane capabilities to settings
// resolved by the parent API. Keep the parent's public session ID as the stable
// snapshot key so state survives allocation to a different local runner.
func (m *KubernetesSessionManager) normalizeProvisionSettings(settings *sessionsettings.SessionSettings) *sessionsettings.SessionSettings {
	if settings == nil {
		return nil
	}
	normalized := *settings
	normalized.Env = cloneStringMap(settings.Env)
	if normalized.Env == nil {
		normalized.Env = make(map[string]string)
	}
	normalized.Env[proxybinary.EnvName] = m.sessionBinaryPath()
	normalized.Session = settings.Session
	normalized.Session.PersistenceEnabled = m.config.SessionPersistence.Backend != ""
	normalized.ParentRuntime = m.executionPlaneParentRuntime(settings.ParentRuntime)
	m.mutex.RLock()
	parentURL := m.runnerParentURL
	m.mutex.RUnlock()
	sessionsettings.RouteEnvToParent(normalized.Env, parentURL)
	return &normalized
}

func (m *KubernetesSessionManager) executionPlaneParentRuntime(runtime *sessionsettings.ParentRuntimeConfig) *sessionsettings.ParentRuntimeConfig {
	m.mutex.RLock()
	parentURL := m.runnerParentURL
	m.mutex.RUnlock()
	if runtime == nil || parentURL == "" {
		return runtime
	}
	next := *runtime
	next.Endpoint = parentURL
	return &next
}

func (m *KubernetesSessionManager) scheduleSuspendWhenRestoredWorkloadReady(session *KubernetesSession) {
	timeout := time.Duration(m.k8sConfig.PodStartTimeout) * time.Second
	if timeout <= 0 {
		timeout = 5 * time.Minute
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		ready, err := m.isSessionWorkloadReady(ctx, session)
		if err == nil && ready {
			if err := m.ScheduleSessionSuspend(ctx, session.ID()); err != nil {
				log.Printf("[K8S_SESSION] Failed to schedule post-resume suspend for session %s: %v", session.ID(), err)
			}
			return
		}
		select {
		case <-ctx.Done():
			log.Printf("[K8S_SESSION] Timed out waiting to schedule post-resume suspend for session %s", session.ID())
			return
		case <-ticker.C:
		}
	}
}

func (m *KubernetesSessionManager) clearSessionSuspendState(ctx context.Context, serviceName string) error {
	patch := []byte(fmt.Sprintf(`{"metadata":{"annotations":{"%s":null,"%s":null}}}`, sessionSuspendAtAnnotation, sessionSuspendedAtAnnotation))
	if _, err := m.client.CoreV1().Services(m.namespace).Patch(ctx, serviceName, types.MergePatchType, patch, metav1.PatchOptions{}); err != nil {
		return fmt.Errorf("clear suspend state: %w", err)
	}
	return nil
}

func (m *KubernetesSessionManager) requireSessionSettingsSecret(ctx context.Context, session *KubernetesSession) error {
	name := strings.TrimSuffix(session.ServiceName(), "-svc") + "-settings"
	if _, err := m.client.CoreV1().Secrets(m.namespace).Get(ctx, name, metav1.GetOptions{}); err != nil {
		return fmt.Errorf("get restart settings secret %s: %w", name, err)
	}
	return nil
}

func (m *KubernetesSessionManager) sessionHasReadyEndpoint(ctx context.Context, serviceName string) (bool, error) {
	endpoints, err := m.client.CoreV1().Endpoints(m.namespace).Get(ctx, serviceName, metav1.GetOptions{})
	if errors.IsNotFound(err) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("get session endpoints: %w", err)
	}
	for _, subset := range endpoints.Subsets {
		if len(subset.Addresses) > 0 {
			return true, nil
		}
	}
	return false, nil
}

// runtimeStatusOverrideFromRedis reads the latest agentapi runtime status from
// Redis and, when it is more informative than the current in-memory status,
// applies it to the session via SetStatusSilent so no additional broadcast is
// triggered (the broadcasting pod already did that).
//
// The "infrastructure" statuses (creating, starting, unhealthy, stopped,
// unknown) are authoritative from Kubernetes and are never overridden by Redis.
