package sessionsettings

import (
	"sort"
	"strconv"
	"strings"

	"github.com/takutakahashi/agentapi-proxy/pkg/modelprovider"
)

// FileTypeCodexAuth is the type name for ~/.codex/auth.json (Codex CLI credentials).
const FileTypeCodexAuth = "codex_auth"

// FileTypeClaudeCredentials is the type name for ~/.claude/.credentials.json (Claude Code credentials).
const FileTypeClaudeCredentials = "claude_credentials"

// FileTypeAntigravityAuth stores the portable file-backed token bundle created
// by `agy acp --login` on headless hosts. The provisioner expands the bundle
// into ~/.gemini/antigravity-cli before starting Antigravity.
const FileTypeAntigravityAuth = "antigravity_auth"

// FileTypeGoogleADC is the standard Google Application Default Credentials
// file consumed by Antigravity when AGY_ADC_AUTH=true.
const FileTypeGoogleADC = "google_adc"

// ManagedFileTypes maps a file type name to its absolute path inside the agent container.
// This is the single source of truth shared by the provisioner (runFilesSync) and
// the credentials controller (PUT /credentials/{name}).
var ManagedFileTypes = map[string]string{
	FileTypeCodexAuth:         "/home/agentapi/.codex/auth.json",
	FileTypeClaudeCredentials: "/home/agentapi/.claude/.credentials.json",
	FileTypeAntigravityAuth:   "/home/agentapi/.ccplant/antigravity-auth.json",
	FileTypeGoogleADC:         "/home/agentapi/.config/gcloud/application_default_credentials.json",
}

// ManagedFileTypeOrder defines the canonical ordering of file types used when
// serialising ManagedFile slices to/from Kubernetes Secret data.
var ManagedFileTypeOrder = []string{
	FileTypeCodexAuth,
	FileTypeClaudeCredentials,
	FileTypeAntigravityAuth,
	FileTypeGoogleADC,
}

// ManagedFile represents a file path and its contents, used to persist arbitrary
// files across sessions via the agentapi-agent-files-{userID} Kubernetes Secret.
type ManagedFile struct {
	Path        string `yaml:"path"                  json:"path"`
	Content     string `yaml:"content"               json:"content"`
	Permissions string `yaml:"permissions,omitempty" json:"permissions,omitempty"`
}

// FilesToSecretData converts a slice of ManagedFile into a flat map suitable for
// storing in a Kubernetes Secret.  The format is index-based:
//
//	"0.path"    → files[0].Path
//	"0.content" → files[0].Content
//	"1.path"    → files[1].Path
//	…
func FilesToSecretData(files []ManagedFile) map[string][]byte {
	data := make(map[string][]byte, len(files)*2)
	for i, f := range files {
		prefix := strconv.Itoa(i)
		data[prefix+".path"] = []byte(f.Path)
		data[prefix+".content"] = []byte(f.Content)
	}
	return data
}

// SecretDataToFiles reconstructs a slice of ManagedFile from the flat index-based
// map produced by FilesToSecretData.  Entries that do not match the expected format
// are silently skipped.  The result is ordered by ascending index so that the
// output is deterministic regardless of Go's map iteration order.
func SecretDataToFiles(data map[string][]byte) []ManagedFile {
	// Collect unique indices first.
	indexSet := map[int]struct{}{}
	for k := range data {
		if idx, ok := parseFileSecretKey(k); ok {
			indexSet[idx] = struct{}{}
		}
	}

	// Sort indices for deterministic output.
	indices := make([]int, 0, len(indexSet))
	for idx := range indexSet {
		indices = append(indices, idx)
	}
	sort.Ints(indices)

	files := make([]ManagedFile, 0, len(indices))
	for _, idx := range indices {
		prefix := strconv.Itoa(idx)
		path, hasPath := data[prefix+".path"]
		content, hasContent := data[prefix+".content"]
		if !hasPath || !hasContent {
			continue
		}
		files = append(files, ManagedFile{
			Path:    string(path),
			Content: string(content),
		})
	}
	return files
}

// parseFileSecretKey parses a key of the form "<index>.path" or "<index>.content"
// and returns the index.  Returns (0, false) if the key does not match.
// LastIndex is used instead of Index so that the suffix ("path" or "content")
// is always the part after the final dot, which correctly handles any future
// index values that might themselves contain dots.
func parseFileSecretKey(k string) (int, bool) {
	dot := strings.LastIndex(k, ".")
	if dot < 0 {
		return 0, false
	}
	suffix := k[dot+1:]
	if suffix != "path" && suffix != "content" {
		return 0, false
	}
	idx, err := strconv.Atoi(k[:dot])
	if err != nil {
		return 0, false
	}
	return idx, true
}

// SandboxConfig holds network sandbox configuration for a session Pod.
// When Enabled is true, an init container sets up iptables rules and a sidecar
// acts as a transparent HTTP/HTTPS proxy.
// AllowedDomains (allowlist) takes precedence over DeniedDomains (denylist).
type SandboxConfig struct {
	Enabled        bool     `yaml:"enabled"                   json:"enabled"`
	PolicyID       string   `yaml:"policy_id,omitempty"       json:"policy_id,omitempty"`
	AllowedDomains []string `yaml:"allowed_domains,omitempty" json:"allowed_domains,omitempty"`
	DeniedDomains  []string `yaml:"denied_domains,omitempty"  json:"denied_domains,omitempty"`
	CountMode      bool     `yaml:"count_mode,omitempty"      json:"count_mode,omitempty"`
}

// SandboxLocalAddressRanges are pre-allowed in allowlist mode so that
// cluster-local services remain reachable through the network-filter proxy.
var SandboxLocalAddressRanges = []string{"127.0.0.0/8", "10.0.0.0/8", "172.16.0.0/12", "192.168.0.0/16"}

// SandboxAllowedDomains returns the effective allowlist sent to the running
// network filter. Adding an allowlist while in denylist mode would change the
// policy semantics, so local ranges are added only when no deny-only policy is
// configured.
func SandboxAllowedDomains(allowed, denied []string) []string {
	if len(allowed) == 0 && len(denied) > 0 {
		return allowed
	}
	effective := append([]string(nil), allowed...)
	return append(effective, SandboxLocalAddressRanges...)
}

// DockerConfig holds Docker-in-Docker (DinD) configuration for a session Pod.
// When Enabled is true, a DinD sidecar is added to the session Pod and
// DOCKER_HOST is set so the main container can communicate with the docker daemon.
type DockerConfig struct {
	Enabled    bool             `yaml:"enabled"              json:"enabled"`
	Registries []RegistryConfig `yaml:"registries,omitempty" json:"registries,omitempty"`
}

// RegistryConfig holds authentication configuration for a container registry.
type RegistryConfig struct {
	// Server is the registry server address (e.g., "ghcr.io", "registry.example.com")
	Server string `yaml:"server"                json:"server"`
	// Username is the registry username for inline credentials
	Username string `yaml:"username,omitempty"    json:"username,omitempty"`
	// Password is the registry password or access token for inline credentials
	Password string `yaml:"password,omitempty"    json:"password,omitempty"`
	// SecretName is the name of a K8s Secret containing a "config.json" key
	// with docker config JSON. When set, Username/Password are ignored for mounting
	// (inline credentials are still used for docker login by the provisioner).
	SecretName string `yaml:"secret_name,omitempty" json:"secret_name,omitempty"`
	// Insecure allows the DinD daemon to communicate with this registry over plain HTTP.
	// When true, --insecure-registry=<Server> is added to the dockerd args.
	Insecure bool `yaml:"insecure,omitempty"    json:"insecure,omitempty"`
}

// StorageFile describes one object_snapshot entry restored by the provisioner.
type StorageFile struct {
	Path     string `yaml:"path" json:"path"`
	Digest   string `yaml:"digest" json:"digest"`
	Size     int64  `yaml:"size,omitempty" json:"size,omitempty"`
	Mode     uint32 `yaml:"mode,omitempty" json:"mode,omitempty"`
	Revision int64  `yaml:"revision" json:"revision"`
}

// StorageAttachment is the resolved persistent mount contract delivered to a
// Session Pod. Provider credentials are deliberately excluded.
type StorageAttachment struct {
	StorageID   string        `yaml:"storage_id" json:"storage_id"`
	StoragePath string        `yaml:"storage_path,omitempty" json:"storage_path,omitempty"`
	MountPath   string        `yaml:"mount_path" json:"mount_path"`
	ReadOnly    bool          `yaml:"read_only,omitempty" json:"read_only,omitempty"`
	Managed     bool          `yaml:"managed,omitempty" json:"managed,omitempty"`
	Kind        string        `yaml:"kind" json:"kind"`
	Revision    int64         `yaml:"revision,omitempty" json:"revision,omitempty"`
	Files       []StorageFile `yaml:"files,omitempty" json:"files,omitempty"`
}

// SessionSettings is the top-level unified settings YAML structure.
// It consolidates all configuration needed for a session Pod.
type SessionSettings struct {
	Paused           bool   `json:"paused,omitempty" yaml:"paused,omitempty"`
	RestartID        string `json:"restart_id,omitempty" yaml:"restart_id,omitempty"`
	RestartInPlace   bool   `json:"-" yaml:"-"`
	WorkspaceKept    bool   `json:"-" yaml:"-"`
	CredentialOwner  string `json:"credential_owner,omitempty" yaml:"credential_owner,omitempty"`
	CredentialSyncID string `json:"credential_sync_id,omitempty" yaml:"credential_sync_id,omitempty"`
	// Restart requires restoration of the same conversation and suppresses first-launch actions.
	Restart bool `json:"restart,omitempty" yaml:"restart,omitempty"`

	CodexConnection       *modelprovider.Connection `json:"codex_connection,omitempty" yaml:"codex_connection,omitempty"`
	ClaudeConnection      *modelprovider.Connection `json:"claude_connection,omitempty" yaml:"claude_connection,omitempty"`
	UnsetEnv              []string                  `json:"unset_env,omitempty" yaml:"unset_env,omitempty"`
	RemoveFiles           []string                  `json:"remove_files,omitempty" yaml:"remove_files,omitempty"`
	Session               SessionMeta               `yaml:"session"                   json:"session"`
	Env                   map[string]string         `yaml:"env,omitempty"             json:"env,omitempty"`
	Claude                ClaudeConfig              `yaml:"claude,omitempty"          json:"claude,omitempty"`
	Codex                 CodexConfig               `yaml:"codex,omitempty"           json:"codex,omitempty"`
	Pi                    PiConfig                  `yaml:"pi,omitempty"              json:"pi,omitempty"`
	Repository            *RepositoryConfig         `yaml:"repository,omitempty"      json:"repository,omitempty"`
	Repositories          []RepositoryConfig        `yaml:"repositories,omitempty"    json:"repositories,omitempty"`
	InitialMessage        string                    `yaml:"initial_message,omitempty" json:"initial_message,omitempty"`
	WebhookPayload        string                    `yaml:"webhook_payload,omitempty" json:"webhook_payload,omitempty"`
	Startup               StartupConfig             `yaml:"startup,omitempty"         json:"startup,omitempty"`
	Github                *GithubConfig             `yaml:"github,omitempty"          json:"github,omitempty"`
	SlackParams           *SlackParams              `yaml:"slack_params,omitempty"    json:"slack_params,omitempty"`
	OtelCollector         *OtelCollectorConfig      `yaml:"otel_collector,omitempty"  json:"otel_collector,omitempty"`
	UsageReportingEnabled bool                      `yaml:"usage_reporting_enabled,omitempty" json:"usage_reporting_enabled,omitempty"`
	Sandbox               *SandboxConfig            `yaml:"sandbox,omitempty"         json:"sandbox,omitempty"`
	Docker                *DockerConfig             `yaml:"docker,omitempty"          json:"docker,omitempty"`
	Storages              []StorageAttachment       `yaml:"storages,omitempty"        json:"storages,omitempty"`
	// Files holds the managed files to be restored at session startup.
	// They are read from the agentapi-agent-files-{userID} Secret at session creation
	// time and written to their respective paths by the provisioner.
	// The runFilesSync goroutine watches those paths and syncs changes back to the Secret.
	Files []ManagedFile `yaml:"files,omitempty" json:"files,omitempty"`
	// Credentials is deprecated: use Files instead.
	// Kept for backward compatibility with sessions provisioned before the Files field
	// was introduced.  The provisioner falls back to this field when Files is empty.
	Credentials       string   `yaml:"credentials,omitempty" json:"credentials,omitempty"`
	UnsyncedFilePaths []string `yaml:"unsynced_file_paths,omitempty" json:"unsynced_file_paths,omitempty"`
	// ParentRuntime configures the Session Pod's outbound reverse-RPC connection
	// to the parent proxy. It is populated only for externally allocated sessions.
	ParentRuntime *ParentRuntimeConfig `yaml:"parent_runtime,omitempty" json:"parent_runtime,omitempty"`
}

// ParentRuntimeConfig is the per-session bootstrap material used by the
// provisioner to connect directly to the parent proxy after local provisioning.
type ParentRuntimeConfig struct {
	Enabled    bool   `yaml:"enabled" json:"enabled"`
	Endpoint   string `yaml:"endpoint" json:"endpoint"`
	SessionID  string `yaml:"session_id" json:"session_id"`
	ManagerID  string `yaml:"manager_id" json:"manager_id"`
	Token      string `yaml:"token" json:"token"`
	Generation int64  `yaml:"generation" json:"generation"`
}

// RuntimeProfile is the parent-owned Kubernetes session runtime configuration
// inherited by an External Session Manager for every allocation. ESMs do not
// expose independent overrides for these fields.
type RuntimeProfile struct {
	Version    int                      `yaml:"version" json:"version"`
	Kubernetes KubernetesRuntimeProfile `yaml:"kubernetes" json:"kubernetes"`
	Scia       SciaRuntimeProfile       `yaml:"scia" json:"scia"`
	Moli       MoliRuntimeProfile       `yaml:"moli" json:"moli"`
}

type MoliRuntimeProfile struct {
	Enabled       bool   `yaml:"enabled" json:"enabled"`
	Image         string `yaml:"image" json:"image"`
	Port          int    `yaml:"port" json:"port"`
	Layout        bool   `yaml:"layout" json:"layout"`
	CPURequest    string `yaml:"cpu_request" json:"cpu_request"`
	CPULimit      string `yaml:"cpu_limit" json:"cpu_limit"`
	MemoryRequest string `yaml:"memory_request" json:"memory_request"`
	MemoryLimit   string `yaml:"memory_limit" json:"memory_limit"`
}

type KubernetesRuntimeProfile struct {
	ServiceAccount                 string `yaml:"service_account" json:"service_account"`
	NetworkFilterImage             string `yaml:"network_filter_image" json:"network_filter_image"`
	NetworkFilterCPURequest        string `yaml:"network_filter_cpu_request" json:"network_filter_cpu_request"`
	NetworkFilterCPULimit          string `yaml:"network_filter_cpu_limit" json:"network_filter_cpu_limit"`
	NetworkFilterMemoryRequest     string `yaml:"network_filter_memory_request" json:"network_filter_memory_request"`
	NetworkFilterMemoryLimit       string `yaml:"network_filter_memory_limit" json:"network_filter_memory_limit"`
	NetworkFilterInitCPURequest    string `yaml:"network_filter_init_cpu_request" json:"network_filter_init_cpu_request"`
	NetworkFilterInitCPULimit      string `yaml:"network_filter_init_cpu_limit" json:"network_filter_init_cpu_limit"`
	NetworkFilterInitMemoryRequest string `yaml:"network_filter_init_memory_request" json:"network_filter_init_memory_request"`
	NetworkFilterInitMemoryLimit   string `yaml:"network_filter_init_memory_limit" json:"network_filter_init_memory_limit"`
}

type SciaRuntimeProfile struct {
	Enabled                   bool     `yaml:"enabled" json:"enabled"`
	SessionSidecarEnabled     bool     `yaml:"session_sidecar_enabled" json:"session_sidecar_enabled"`
	SessionSidecarImage       string   `yaml:"session_sidecar_image" json:"session_sidecar_image"`
	SessionSidecarConfigImage string   `yaml:"session_sidecar_config_image" json:"session_sidecar_config_image"`
	SessionSidecarPort        int      `yaml:"session_sidecar_port" json:"session_sidecar_port"`
	Credential                string   `yaml:"credential" json:"credential"`
	UserNamespace             string   `yaml:"user_namespace" json:"user_namespace"`
	NoProxy                   string   `yaml:"no_proxy" json:"no_proxy"`
	GoogleHosts               []string `yaml:"google_hosts" json:"google_hosts"`
	GooglePaths               []string `yaml:"google_paths" json:"google_paths"`
	TodoistCredential         string   `yaml:"todoist_credential" json:"todoist_credential"`
	TodoistHosts              []string `yaml:"todoist_hosts" json:"todoist_hosts"`
	TodoistPaths              []string `yaml:"todoist_paths" json:"todoist_paths"`
}

// OtelCollectorConfig holds OpenTelemetry Collector configuration for in-process mode.
// When set, the provisioner will launch otelcol as a subprocess after user context
// is established, ensuring metrics labels (user_id, session_id, etc.) are correct
// even when using the stock inventory feature.
type OtelCollectorConfig struct {
	Enabled        bool   `yaml:"enabled"          json:"enabled"`
	ScrapeInterval string `yaml:"scrape_interval"  json:"scrape_interval"`
	ClaudeCodePort int    `yaml:"claude_code_port" json:"claude_code_port"`
	ExporterPort   int    `yaml:"exporter_port"    json:"exporter_port"`
	// Label values resolved at session creation time (not startup time)
	SessionID  string `yaml:"session_id"  json:"session_id"`
	UserID     string `yaml:"user_id"     json:"user_id"`
	TeamID     string `yaml:"team_id"     json:"team_id"`
	ScheduleID string `yaml:"schedule_id" json:"schedule_id"`
	WebhookID  string `yaml:"webhook_id"  json:"webhook_id"`
	AgentType  string `yaml:"agent_type"  json:"agent_type"`
}

// SlackParams holds Slack integration parameters for the provisioner subprocess.
// When set, the provisioner will launch claude-posts as a subprocess to forward
// agent output to the specified Slack channel/thread.
type SlackParams struct {
	Channel  string `yaml:"channel"            json:"channel"`
	ThreadTS string `yaml:"thread_ts,omitempty" json:"thread_ts,omitempty"`
	BotToken string `yaml:"bot_token"          json:"bot_token"`
}

// SessionMeta contains session identification metadata.
type SessionMeta struct {
	ID                 string   `yaml:"id"                  json:"id"`
	UserID             string   `yaml:"user_id"             json:"user_id"`
	Scope              string   `yaml:"scope"               json:"scope"`
	TeamID             string   `yaml:"team_id,omitempty"   json:"team_id,omitempty"`
	AgentType          string   `yaml:"agent_type,omitempty" json:"agent_type,omitempty"`
	Oneshot            bool     `yaml:"oneshot,omitempty"   json:"oneshot,omitempty"`
	Teams              []string `yaml:"teams,omitempty"     json:"teams,omitempty"`
	ResumeFrom         string   `yaml:"resume_from,omitempty" json:"resume_from,omitempty"`
	PersistenceEnabled bool     `yaml:"persistence_enabled,omitempty" json:"persistence_enabled,omitempty"`
	AutoSuspendEnabled *bool    `yaml:"auto_suspend_enabled,omitempty" json:"auto_suspend_enabled,omitempty"`
	AutoSuspendMinutes int      `yaml:"auto_suspend_minutes,omitempty" json:"auto_suspend_minutes,omitempty"`
}

// ClaudeConfig holds Claude-related configuration data.
