package services

import (
	"context"
	"fmt"
	"io"
	"log"
	"os"
	"sort"
	"strings"
	"time"

	portrepos "github.com/takutakahashi/agentapi-proxy/internal/usecases/ports/repositories"
	"github.com/takutakahashi/agentapi-proxy/pkg/sessionsettings"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

// GetNamespace returns the Kubernetes namespace (used by subscription secret syncer)
func (m *KubernetesSessionManager) GetNamespace() string {
	return m.namespace
}

func (m *KubernetesSessionManager) OperationalStatus(ctx context.Context, pools []string) (map[string]interface{}, error) {
	// Fetch every managed session service. Pool-scoped callers are filtered
	// below, while an unfiltered administrative inventory must also see direct
	// sessions which deliberately have no session-pool label.
	services, err := m.client.CoreV1().Services(m.namespace).List(ctx, metav1.ListOptions{LabelSelector: m.managerScopedSelector("app.kubernetes.io/managed-by=agentapi-proxy,app.kubernetes.io/name=agentapi-session")})
	if err != nil {
		return nil, err
	}
	// The pool label is assigned to the runner Service when stock is created;
	// older and adopted Pod templates do not necessarily carry it. Join Pods to
	// Services by session ID instead of filtering Pods by pool.
	pods, err := m.client.CoreV1().Pods(m.namespace).List(ctx, metav1.ListOptions{LabelSelector: m.managerScopedSelector("app.kubernetes.io/managed-by=agentapi-proxy,app.kubernetes.io/name=agentapi-session")})
	if err != nil {
		return nil, err
	}
	live := make(map[string]bool, len(pods.Items))
	for i := range pods.Items {
		pod := &pods.Items[i]
		if pod.DeletionTimestamp == nil && pod.Status.Phase == corev1.PodRunning {
			live[pod.Labels["agentapi.proxy/session-id"]] = true
		}
	}
	running, used := 0, 0
	runningIDs, usedIDs := make([]string, 0), make([]string, 0)
	allowedPools := make(map[string]bool, len(pools))
	for _, pool := range pools {
		allowedPools[pool] = true
	}
	for i := range services.Items {
		service := &services.Items[i]
		if len(allowedPools) > 0 && !allowedPools[service.Labels["agentapi.proxy/session-pool"]] {
			continue
		}
		id := service.Labels["agentapi.proxy/session-id"]
		if service.DeletionTimestamp != nil || !live[id] {
			continue
		}
		running++
		if id != "" {
			runningIDs = append(runningIDs, id)
		}
		if service.Labels["agentapi.proxy/stock"] != "true" {
			used++
			if id != "" {
				usedIDs = append(usedIDs, id)
			}
		}
	}
	sort.Strings(runningIDs)
	sort.Strings(usedIDs)
	version := "unknown"
	if m.config != nil && m.config.SessionManager.CurrentVersion != "" {
		version = m.config.SessionManager.CurrentVersion
	}
	return map[string]interface{}{"status": "online", "version": version, "running_runners": running, "used_runners": used, "running_runner_ids": runningIDs, "used_runner_ids": usedIDs}, nil
}

func (m *KubernetesSessionManager) OperationalLogs(ctx context.Context, runnerID, sessionID string, tail int) ([]string, string, error) {
	podName := strings.TrimSpace(os.Getenv("HOSTNAME"))
	container := "session-manager"
	if runnerID != "" {
		sessionID = runnerID
	}
	if sessionID != "" {
		pods, err := m.client.CoreV1().Pods(m.namespace).List(ctx, metav1.ListOptions{LabelSelector: "agentapi.proxy/session-id=" + sessionID})
		if err != nil {
			return nil, "", err
		}
		if len(pods.Items) == 0 {
			return nil, "", fmt.Errorf("runner %s not found", sessionID)
		}
		podName, container = pods.Items[0].Name, "agentapi"
		if len(pods.Items[0].Spec.Containers) > 0 {
			container = pods.Items[0].Spec.Containers[0].Name
		}
	}
	if podName == "" {
		return nil, "", fmt.Errorf("pod name is unavailable")
	}
	count := int64(tail)
	stream, err := m.client.CoreV1().Pods(m.namespace).GetLogs(podName, &corev1.PodLogOptions{Container: container, TailLines: &count}).Stream(ctx)
	if err != nil {
		return nil, podName + "/" + container, err
	}
	defer func() { _ = stream.Close() }()
	data, err := io.ReadAll(io.LimitReader(stream, 2<<20))
	if err != nil {
		return nil, podName + "/" + container, err
	}
	lines := strings.Split(strings.TrimRight(string(data), "\r\n"), "\n")
	return lines, podName + "/" + container, nil
}

// SetSettingsRepository sets the settings repository for Bedrock configuration
func (m *KubernetesSessionManager) SetSettingsRepository(repo portrepos.SettingsRepository) {
	m.settingsRepo = repo
}

// SetSettingsSecretClient selects the application persistence store for raw
// settings patches, including the configured base Secret. The API settings
// builder has no workload client; its persistence client may be a KV adapter.
func (m *KubernetesSessionManager) SetSettingsSecretClient(client kubernetes.Interface, namespace string) {
	m.settingsSecretClient = client
	m.settingsSecretNamespace = namespace
}

// SetSlackTokenClient selects the application store holding Slack credentials.
// API-side settings builders have no runtime Kubernetes client; tokens must be
// resolved here before the complete provision settings are sent to a runner.
func (m *KubernetesSessionManager) SetSlackTokenClient(client kubernetes.Interface, namespace string) {
	m.slackTokenClient = client
	m.slackTokenNamespace = namespace
}

// SetTeamConfigRepository sets the team config repository for service account configuration
func (m *KubernetesSessionManager) SetTeamConfigRepository(repo portrepos.TeamConfigRepository) {
	m.teamConfigRepo = repo
}

// SetCredentialsRepository sets the credentials repository used to load managed
// credential files (Codex auth.json, Claude .credentials.json) from the
// application KV store when embedding files into session pods.
func (m *KubernetesSessionManager) SetCredentialsRepository(repo portrepos.CredentialsRepository) {
	m.credentialsRepo = repo
}

// SetUserFileRepository sets the application KV-backed repository used to load
// user-managed files when building session provision settings.
func (m *KubernetesSessionManager) SetUserFileRepository(repo portrepos.UserFileRepository) {
	m.userFileRepo = repo
}

// loadCredentialFiles retrieves the managed credential files for a credential
// owner. It reads from the credentials repository (backed by the application KV
// store, where the Codex device-auth flow and credentials API persist files) and
// merges in any legacy files that still exist only in the agentapi-agent-files-*
// Kubernetes Secret, so that pre-KV credentials are not lost during the
// transition. Files already present from the KV store take precedence.
func (m *KubernetesSessionManager) loadCredentialFiles(ctx context.Context, owner string) ([]sessionsettings.ManagedFile, string) {
	secretName := fmt.Sprintf("agentapi-agent-files-%s", sanitizeLabelValue(owner))
	var files []sessionsettings.ManagedFile

	if m.credentialsRepo != nil {
		if creds, err := m.credentialsRepo.FindByName(ctx, owner); err == nil {
			files = append(files, creds.Files()...)
		}
	}

	if secret, err := m.client.CoreV1().Secrets(m.namespace).Get(ctx, secretName, metav1.GetOptions{}); err == nil && len(secret.Data) > 0 {
		legacy := sessionsettings.SecretDataToFiles(secret.Data)
		if len(legacy) > 0 {
			seen := make(map[string]bool, len(files))
			for _, f := range files {
				seen[f.Path] = true
			}
			for _, f := range legacy {
				if !seen[f.Path] {
					files = append(files, f)
				}
			}
		}
	}

	return files, secretName
}

// SetSandboxPolicyRepository sets the sandbox policy repository for policy resolution at session creation.
func (m *KubernetesSessionManager) SetSandboxPolicyRepository(repo portrepos.SandboxPolicyRepository) {
	m.sandboxPolicyRepo = repo
}

// SetServiceAccountEnsurer sets the service account ensurer for team-scoped session creation
func (m *KubernetesSessionManager) SetServiceAccountEnsurer(ensurer ServiceAccountEnsurer) {
	m.serviceAccountEnsurer = ensurer
}

// SetPersonalAPIKeyLoader wires in the auth-layer loader so that personal API
// keys created on-the-fly in buildSessionSettings are immediately available for
// authentication (without requiring a proxy restart to re-run bootstrap).
func (m *KubernetesSessionManager) SetPersonalAPIKeyLoader(loader PersonalAPIKeyLoader) {
	m.personalAPIKeyLoader = loader
}

// AddSessionDeletedHandler registers a handler that is invoked when a session is deleted,
// before its Kubernetes resources are removed. Multiple handlers can be registered and
// they are called in registration order.
func (m *KubernetesSessionManager) AddSessionDeletedHandler(handler SessionDeletedHandler) {
	m.handlersMutex.Lock()
	defer m.handlersMutex.Unlock()
	m.onSessionDeletedHandlers = append(m.onSessionDeletedHandlers, handler)
}

// broadcastStatusChangeLocal broadcasts a SessionStatusEvent to all local
// (in-process) SSE subscribers without publishing to Redis.
// Called by consumeStatusEvents when replaying a cross-pod event so the event
// reaches local SSE clients without causing a Redis re-publish loop.
func (m *KubernetesSessionManager) broadcastStatusChangeLocal(sessionID, status string) {
	evt := SessionStatusEvent{
		SessionID: sessionID,
		Status:    status,
		Timestamp: time.Now(),
	}
	m.globalSubsMu.Lock()
	log.Printf("[SSE_BROADCAST] session=%s status=%s subscribers=%d (cross-pod relay)", sessionID, status, len(m.globalSubs))
	for _, ch := range m.globalSubs {
		select {
		case ch <- evt:
		default:
		}
	}
	m.globalSubsMu.Unlock()

}

// broadcastStatusChange broadcasts a SessionStatusEvent to all active proxy-wide subscribers.
// It is registered as the statusChangeCallback on each KubernetesSession.
// Non-blocking: slow subscribers are skipped to avoid stalling SetStatus.
// When a StatusEventRepository is configured it also persists the status and
// publishes an event for other pods to pick up.
func (m *KubernetesSessionManager) broadcastStatusChange(sessionID, status string) {
	evt := SessionStatusEvent{
		SessionID: sessionID,
		Status:    status,
		Timestamp: time.Now(),
	}
	m.globalSubsMu.Lock()
	log.Printf("[SSE_BROADCAST] session=%s status=%s subscribers=%d", sessionID, status, len(m.globalSubs))
	for _, ch := range m.globalSubs {
		select {
		case ch <- evt:
		default:
			// subscriber channel is full; drop to avoid blocking the caller
		}
	}
	m.globalSubsMu.Unlock()

	// Persist and publish to Redis so other pods are notified.
	if m.statusEventRepo != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		if err := m.statusEventRepo.SetStatus(ctx, sessionID, status, m.podID); err != nil {
			log.Printf("[K8S_SESSION] Warning: failed to persist status to Redis session=%s: %v", sessionID, err)
		}
		crossEvt := portrepos.StatusChangeEvent{
			SessionID: sessionID,
			Status:    status,
			UpdatedAt: time.Now(),
			PodID:     m.podID,
		}
		if err := m.statusEventRepo.PublishStatusChange(ctx, crossEvt); err != nil {
			log.Printf("[K8S_SESSION] Warning: failed to publish status to Redis session=%s: %v", sessionID, err)
		}
	}

}

// broadcastMessageUpdate notifies all active per-session subscribers that a message_update
// event was received from the agentapi backend for the given session.
// Also updates the session's lastMessageAt so late-arriving pollers can detect the update.
// Non-blocking: slow subscribers are skipped to avoid stalling the SSE reader goroutine.
func (m *KubernetesSessionManager) broadcastMessageUpdate(sessionID string) {
	now := time.Now()
	m.mutex.RLock()
	session, exists := m.sessions[sessionID]
	m.mutex.RUnlock()
	if exists {
		session.SetLastMessageAt(now)
	}

	evt := SessionMessageEvent{SessionID: sessionID, Timestamp: now}
	m.messageSubsMu.Lock()
	defer m.messageSubsMu.Unlock()
	subs, ok := m.messageSubs[sessionID]
	if !ok {
		return
	}
	log.Printf("[MSG_BROADCAST] session=%s subscribers=%d", sessionID, len(subs))
	for _, ch := range subs {
		select {
		case ch <- evt:
		default:
			// subscriber channel is full; drop to avoid blocking the caller
		}
	}
}

// SubscribeMessageEvents registers a new per-session subscriber for message_update events.
// Returns a channel that receives SessionMessageEvent values whenever the agentapi backend
// for the given session emits a message_update event, and a cancel func that must be called
// to unsubscribe (closes the channel).
func (m *KubernetesSessionManager) SubscribeMessageEvents(sessionID string) (<-chan SessionMessageEvent, func()) {
	m.messageSubsMu.Lock()
	defer m.messageSubsMu.Unlock()
	if _, ok := m.messageSubs[sessionID]; !ok {
		m.messageSubs[sessionID] = make(map[uint64]chan SessionMessageEvent)
	}
	id := m.nextMessageSubID
	m.nextMessageSubID++
	ch := make(chan SessionMessageEvent, 32)
	m.messageSubs[sessionID][id] = ch
	cancel := func() {
		m.messageSubsMu.Lock()
		defer m.messageSubsMu.Unlock()
		if subs, ok := m.messageSubs[sessionID]; ok {
			if c, ok := subs[id]; ok {
				close(c)
				delete(subs, id)
			}
			if len(subs) == 0 {
				delete(m.messageSubs, sessionID)
			}
		}
	}
	return ch, cancel
}

// cleanupMessageSubs closes all active message subscribers for a session and removes them.
// Called during session deletion to prevent goroutine leaks.
func (m *KubernetesSessionManager) cleanupMessageSubs(sessionID string) {
	m.messageSubsMu.Lock()
	defer m.messageSubsMu.Unlock()
	if subs, ok := m.messageSubs[sessionID]; ok {
		for _, ch := range subs {
			close(ch)
		}
		delete(m.messageSubs, sessionID)
	}
}

// SubscribeStatusEvents registers a new proxy-wide subscriber for session status changes.
// Returns a channel that receives SessionStatusEvent values whenever any session's status
// changes, and a cancel func that must be called to unsubscribe (closes the channel).
func (m *KubernetesSessionManager) SubscribeStatusEvents() (<-chan SessionStatusEvent, func()) {
	m.globalSubsMu.Lock()
	defer m.globalSubsMu.Unlock()
	id := m.nextGlobalSubID
	m.nextGlobalSubID++
	ch := make(chan SessionStatusEvent, 32)
	m.globalSubs[id] = ch
	cancel := func() {
		m.globalSubsMu.Lock()
		defer m.globalSubsMu.Unlock()
		if ch, ok := m.globalSubs[id]; ok {
			close(ch)
			delete(m.globalSubs, id)
		}
	}
	return ch, cancel
}
