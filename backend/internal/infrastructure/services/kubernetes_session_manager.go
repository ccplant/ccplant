package services

import (
	"context"
	stderrors "errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	coreallocation "github.com/takutakahashi/agentapi-proxy/internal/core/sessionallocation"
	coresessioncontrol "github.com/takutakahashi/agentapi-proxy/internal/core/sessioncontrol"
	"github.com/takutakahashi/agentapi-proxy/internal/domain/entities"
	infrasessionallocation "github.com/takutakahashi/agentapi-proxy/internal/infrastructure/sessionallocation"
	portrepos "github.com/takutakahashi/agentapi-proxy/internal/usecases/ports/repositories"
	"github.com/takutakahashi/agentapi-proxy/pkg/config"
	"github.com/takutakahashi/agentapi-proxy/pkg/logger"
	"github.com/takutakahashi/agentapi-proxy/pkg/sessionsettings"
	"github.com/takutakahashi/agentapi-proxy/pkg/settingspatch"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	"golang.org/x/sync/singleflight"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/cache"
	ctrl "sigs.k8s.io/controller-runtime"
)

// provisionerPort is the TCP port on which agent-provisioner listens inside session Pods.
// It serves local health/status and sandbox-domain endpoints; provisioning work
// is pulled from the proxy internal API by the session Pod.
const provisionerPort = 9001

const (
	agentSocketPath       = "/tmp/ccplant/agent.sock"
	provisionerSocketPath = "/tmp/ccplant/provisioner.sock"
	nfaControlSocketPath  = "/run/nfa/control.sock"
	nfaLogPath            = "/run/nfa/network-filter.log"
)

const stockPodTemplateHashLabel = "agentapi.proxy/pod-template-hash"

var instrumentedHTTPClient = &http.Client{Transport: otelhttp.NewTransport(http.DefaultTransport)}

const sessionControlConnectionTimeout = time.Second

const (
	sessionSuspendAtAnnotation               = "agentapi.proxy/suspend-at"
	sessionSuspendedAtAnnotation             = "agentapi.proxy/suspended-at"
	sessionAutoSuspendEnabledAnnotation      = "agentapi.proxy/auto-suspend-enabled"
	sessionAutoSuspendIdleSecondsAnnotation  = "agentapi.proxy/auto-suspend-idle-seconds"
	sessionAutoSuspendPolicySourceAnnotation = "agentapi.proxy/auto-suspend-policy-source"
	sessionSuspendOwnerAnnotation            = "agentapi.proxy/suspend-owner"
	sessionSuspendOwnerExpiresAtAnnotation   = "agentapi.proxy/suspend-owner-expires-at"
)

// Ownership must outlive the longest queued checkpoint command (30 minutes)
// so another replica cannot start a duplicate while the first is still waiting.
const sessionSuspendOwnershipTTL = 35 * time.Minute

const podRuntimeStatusUnknownGrace = 2 * time.Minute

var ErrInvalidRunnerToken = stderrors.New("invalid runner token")

// ProvisionerPort is the exported version of provisionerPort for use by other packages
// (e.g. the session controller error handler that checks provisioner /status).
const ProvisionerPort = provisionerPort

func localHTTPProbe(path string, port int) corev1.ProbeHandler {
	return corev1.ProbeHandler{Exec: &corev1.ExecAction{Command: []string{
		"curl", "--fail", "--silent", "--show-error", "--max-time", "1",
		fmt.Sprintf("http://127.0.0.1:%d%s", port, path),
	}}}
}

func localUnixHTTPProbe(path, socketPath string) corev1.ProbeHandler {
	return corev1.ProbeHandler{Exec: &corev1.ExecAction{Command: []string{
		"curl", "--fail", "--silent", "--show-error", "--max-time", "1",
		"--unix-socket", socketPath, "http://localhost" + path,
	}}}
}

// nfaProxyPort is the default forward proxy port exposed by takutakahashi/nfa.
const nfaProxyPort = 3128

// KubernetesSessionManager manages sessions using Kubernetes Deployments
// ServiceAccountEnsurer ensures a service account exists for a team.
// Implementations must be safe to call concurrently.
type ServiceAccountEnsurer interface {
	EnsureServiceAccount(ctx context.Context, teamID string) error
}

// PersonalAPIKeyLoader registers a newly-created personal API key in the
// authentication layer so it can be used immediately without a proxy restart.
// Implementations must be safe to call concurrently.
type PersonalAPIKeyLoader interface {
	LoadPersonalAPIKey(ctx context.Context, apiKey *entities.PersonalAPIKey) error
}

// SessionStatusEvent represents a proxy-level status change event for any session.
// It is emitted by KubernetesSessionManager whenever a session's status changes,
// and is delivered to all active SSE/long-poll subscribers.
type SessionStatusEvent = portrepos.SessionStatusEvent

// SessionMessageEvent represents a message_update event for a specific session.
// It is emitted by KubernetesSessionManager whenever the agentapi backend for a session
// sends a message_update SSE event, and is delivered to per-session long-poll subscribers.
type SessionMessageEvent = portrepos.SessionMessageEvent

// SessionDeletedHandler is a callback invoked just before a session's Kubernetes resources
// are removed. At this point the session's Service endpoint is still reachable, so handlers
// can safely call GetMessages or other in-session APIs.
// Handlers are called synchronously and their errors are intentionally ignored so that
// session deletion always proceeds regardless of handler failures.
type SessionDeletedHandler func(ctx context.Context, session entities.Session)

type KubernetesSessionManager struct {
	restartHTTPClient   *http.Client
	podStatusHTTPClient *http.Client
	sessionProfileRepo  portrepos.SessionProfileRepository
	config              *config.Config
	k8sConfig           *config.KubernetesSessionConfig
	configProvider      interface {
		Current() *config.Config
		AgentDefaults() settingspatch.SettingsPatch
	}
	client             kubernetes.Interface
	verbose            bool
	logger             *logger.Logger
	sessions           map[string]*KubernetesSession
	mutex              sync.RWMutex
	authNotificationMu sync.Mutex
	authNotifiedAt     map[string]time.Time
	authNotifier       func(userID, title, body, notificationType string, data map[string]interface{}) error
	namespace          string
	settingsRepo       portrepos.SettingsRepository
	teamConfigRepo     portrepos.TeamConfigRepository
	personalAPIKeyRepo portrepos.PersonalAPIKeyRepository
	sandboxPolicyRepo  portrepos.SandboxPolicyRepository
	userFileRepo       portrepos.UserFileRepository

	settingsSecretClient    kubernetes.Interface
	settingsSecretNamespace string

	slackTokenClient    kubernetes.Interface
	slackTokenNamespace string

	// credentialsRepo reads managed credential files (e.g. ~/.codex/auth.json,
	// ~/.claude/.credentials.json) from the application KV store, which is the
	// canonical store written to by the credentials/Codex device-auth flows.
	credentialsRepo       portrepos.CredentialsRepository
	personalAPIKeyLoader  PersonalAPIKeyLoader
	serviceAccountEnsurer ServiceAccountEnsurer
	// onSessionDeletedHandlers holds callbacks registered via AddSessionDeletedHandler.
	// Protected by handlersMutex.
	onSessionDeletedHandlers []SessionDeletedHandler
	handlersMutex            sync.RWMutex

	// Proxy-wide status change pub/sub.
	// globalSubs maps subscriber ID → buffered channel of SessionStatusEvent.
	// Protected by globalSubsMu.
	globalSubsMu    sync.Mutex
	globalSubs      map[uint64]chan SessionStatusEvent
	nextGlobalSubID uint64

	// Per-session message update pub/sub.
	// messageSubs[sessionID][subscriberID] = buffered channel of SessionMessageEvent.
	// Protected by messageSubsMu.
	messageSubsMu    sync.Mutex
	messageSubs      map[string]map[uint64]chan SessionMessageEvent
	nextMessageSubID uint64

	// statusEventRepo is the cross-pod status synchronisation backend.
	// When Redis is configured this is a RedisStatusRepository; otherwise it is
	// a NoopStatusRepository that keeps the existing single-pod behaviour.
	statusEventRepo portrepos.StatusEventRepository
	// podID uniquely identifies this proxy instance for Pub/Sub deduplication.
	podID string
	// statusSubCtx / statusSubCancel control the lifetime of the Redis subscriber goroutine.
	statusSubCtx    context.Context
	statusSubCancel context.CancelFunc
	suspendCancel   context.CancelFunc

	// sessionListCacheRepo is retained for invalidating legacy Redis list keys
	// written by older replicas during a rolling upgrade. ListSessions itself
	// reads from the informer stores below.
	sessionListCacheRepo portrepos.SessionListCacheRepository
	sessionInformerOnce  sync.Once
	sessionInformerCtx   context.Context
	sessionInformerStop  context.CancelFunc
	serviceInformer      cache.SharedIndexInformer
	workloadInformer     cache.SharedIndexInformer
	allocationInformer   cache.SharedIndexInformer
	allocationListMu     sync.RWMutex
	allocationListCache  *corev1.SecretList
	allocationListUntil  time.Time
	allocationListLoad   singleflight.Group

	// sessionAllocatorEnabled routes CreateSession through the leader-elected
	// SessionAllocator when the server has started that worker.
	sessionAllocatorEnabled bool

	sessionAllocationNotifier coreallocation.Notifier
	sessionControlStore       coresessioncontrol.Store
	sessionRouteRepo          portrepos.SessionRouteRepository
	runnerParentURL           string
	runnerManagerID           string
	runnerManagerToken        string
	runnerDefaultPool         string
	inheritedRuntimeProfile   *sessionsettings.RuntimeProfile
}

// RuntimeTrafficRequiresOutbound prevents controllers from falling back to a
// Kubernetes Service address when no reverse-RPC connection is available.
func (*KubernetesSessionManager) RuntimeTrafficRequiresOutbound() bool { return true }

// SetCodexAuthNotifier configures user notification delivery for expired Codex
// credentials. Delivery is deduplicated per user for one hour.
func (m *KubernetesSessionManager) SetCodexAuthNotifier(notifier func(string, string, string, string, map[string]interface{}) error) {
	m.authNotificationMu.Lock()
	m.authNotifier = notifier
	m.authNotificationMu.Unlock()
}

func (m *KubernetesSessionManager) notifyCodexAuthRequired(session *KubernetesSession) {
	if session == nil || session.Scope() != entities.ScopeUser || session.UserID() == "" {
		return
	}
	m.authNotificationMu.Lock()
	last := m.authNotifiedAt[session.UserID()]
	if !last.IsZero() && time.Since(last) < time.Hour {
		m.authNotificationMu.Unlock()
		return
	}
	notifier := m.authNotifier
	if notifier == nil {
		m.authNotificationMu.Unlock()
		return
	}
	if m.authNotifiedAt == nil {
		m.authNotifiedAt = make(map[string]time.Time)
	}
	m.authNotifiedAt[session.UserID()] = time.Now()
	m.authNotificationMu.Unlock()
	data := map[string]interface{}{"session_id": session.ID(), "url": "/settings#codex-device-auth", "error_code": "codex_auth_required"}
	if err := notifier(session.UserID(), "Codex の再認証が必要です", "Codex の認証期限が切れています。再認証してください。", "error", data); err != nil {
		log.Printf("[K8S_SESSION] Failed to notify user about Codex authentication: %v", err)
	}
}

func (m *KubernetesSessionManager) ConfigureSessionRunnerPool(parentURL, managerID, managerToken, defaultPool string) {
	m.mutex.Lock()
	defer m.mutex.Unlock()
	m.runnerParentURL = strings.TrimRight(parentURL, "/")
	m.runnerManagerID = managerID
	m.runnerManagerToken = managerToken
	m.runnerDefaultPool = defaultPool
}

func (m *KubernetesSessionManager) SetConfigProvider(provider interface {
	Current() *config.Config
	AgentDefaults() settingspatch.SettingsPatch
}) {
	m.configProvider = provider
	m.refreshConfig()
}

func (m *KubernetesSessionManager) refreshConfig() {
	if m.configProvider == nil {
		return
	}
	current := m.configProvider.Current()
	// The provisioner token is startup-only secret material. Runtime settings
	// snapshots may intentionally omit secrets, so never replace a token that
	// was loaded from the environment or Kubernetes Secret with an empty value.
	if current.KubernetesSession.ProvisionerToken == "" && m.k8sConfig != nil {
		current.KubernetesSession.ProvisionerToken = m.k8sConfig.ProvisionerToken
	}
	resolveLegacySessionRuntimeImages(current)
	m.config = current
	m.k8sConfig = &current.KubernetesSession
	if m.inheritedRuntimeProfile != nil {
		m.applyRuntimeProfileLocked(m.inheritedRuntimeProfile)
	}
}

// resolveLegacySessionRuntimeImages upgrades the old single-image defaults in
// memory. This lets a manager create new Pods with immutable agent assets and
// inject the ccplant CLI from its own release without allowing the CLI image
// repository or tag to drift from the manager image.
func resolveLegacySessionRuntimeImages(cfg *config.Config) {
	if cfg == nil {
		return
	}
	repository := strings.TrimSuffix(strings.TrimSpace(cfg.SessionManager.ImageRepository), ":")
	version := strings.TrimSpace(cfg.SessionManager.CurrentVersion)
	if repository == "" || version == "" {
		return
	}
	managerImage := repository + ":" + version
	if strings.TrimSpace(cfg.KubernetesSession.Image) == managerImage {
		cfg.KubernetesSession.Image = config.DefaultKubernetesSessionImage
	}
	cfg.KubernetesSession.CLIImage = managerImage
}

func (m *KubernetesSessionManager) SetSessionControlStore(store coresessioncontrol.Store) {
	m.mutex.Lock()
	defer m.mutex.Unlock()
	m.sessionControlStore = store
}

// SetSessionRouteRepository enables the outbound direct-runtime transport for
// sessions allocated by the built-in manager. The Session Pod connects back to
// the parent; the parent never dials the Pod's agent port.
func (m *KubernetesSessionManager) SetSessionRouteRepository(repo portrepos.SessionRouteRepository) {
	m.mutex.Lock()
	defer m.mutex.Unlock()
	m.sessionRouteRepo = repo
}

// NewKubernetesSessionManager creates a new KubernetesSessionManager
func NewKubernetesSessionManager(
	cfg *config.Config,
	verbose bool,
	lgr *logger.Logger,
) (*KubernetesSessionManager, error) {
	// Get config using controller-runtime (supports in-cluster and kubeconfig)
	restConfig, err := ctrl.GetConfig()
	if err != nil {
		return nil, fmt.Errorf("failed to get kubernetes config: %w", err)
	}

	client, err := kubernetes.NewForConfig(restConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to create kubernetes client: %w", err)
	}

	return NewKubernetesSessionManagerWithClient(cfg, verbose, lgr, client)
}

// NewKubernetesSessionManagerWithClient creates a new KubernetesSessionManager with a custom client
// This is useful for testing with a fake client
func NewKubernetesSessionManagerWithClient(
	cfg *config.Config,
	verbose bool,
	lgr *logger.Logger,
	client kubernetes.Interface,
) (*KubernetesSessionManager, error) {
	resolveLegacySessionRuntimeImages(cfg)
	k8sConfig := &cfg.KubernetesSession

	// Determine namespace
	namespace := resolveKubernetesNamespace(k8sConfig.Namespace)

	log.Printf("[K8S_SESSION] Initialized KubernetesSessionManager in namespace: %s", namespace)

	// Determine pod identity for Pub/Sub deduplication.
	podID, _ := os.Hostname()
	if podID == "" {
		podID = "unknown"
	}

	subCtx, subCancel := context.WithCancel(context.Background())
	informerCtx, informerCancel := context.WithCancel(context.Background())

	manager := &KubernetesSessionManager{
		config:                    cfg,
		k8sConfig:                 k8sConfig,
		client:                    client,
		verbose:                   verbose,
		logger:                    lgr,
		sessions:                  make(map[string]*KubernetesSession),
		namespace:                 namespace,
		globalSubs:                make(map[uint64]chan SessionStatusEvent),
		messageSubs:               make(map[string]map[uint64]chan SessionMessageEvent),
		podID:                     podID,
		statusSubCtx:              subCtx,
		statusSubCancel:           subCancel,
		sessionInformerCtx:        informerCtx,
		sessionInformerStop:       informerCancel,
		sessionAllocationNotifier: infrasessionallocation.NewLocalNotifier(),
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := manager.ensureProvisionerToken(ctx); err != nil {
		return nil, fmt.Errorf("failed to ensure provisioner token: %w", err)
	}

	// Ensure OpenTelemetry Collector ConfigMap exists
	if err := manager.ensureOtelcolConfigMap(ctx); err != nil {
		log.Printf("[K8S_SESSION] Warning: Failed to ensure otelcol ConfigMap: %v", err)
		// Don't fail initialization if ConfigMap creation fails
	}
	if cfg.SessionPersistence.Backend != "" {
		if cfg.SessionPersistence.SuspendAfter != "" && cfg.SessionPersistence.SuspendAfter != "0" {
			if _, err := time.ParseDuration(cfg.SessionPersistence.SuspendAfter); err != nil {
				return nil, fmt.Errorf("invalid session_persistence.suspend_after: %w", err)
			}
		}
		suspendCtx, suspendCancel := context.WithCancel(context.Background())
		manager.suspendCancel = suspendCancel
		go manager.runSessionSuspendReconciler(suspendCtx)
	}

	return manager, nil
}
