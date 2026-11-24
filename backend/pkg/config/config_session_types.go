package config

// KubernetesSessionConfig represents Kubernetes session manager configuration
type KubernetesSessionConfig struct {
	// Namespace is the Kubernetes namespace where session resources are created
	Namespace string `json:"namespace" mapstructure:"namespace"`
	// Image is the container image for session pods
	Image string `json:"image" mapstructure:"image"`
	// CLIImage supplies /usr/local/bin/ccplant through an initContainer. Empty disables injection.
	CLIImage string `json:"cli_image" mapstructure:"cli_image"`
	// ImagePullPolicy is the image pull policy for session pods
	ImagePullPolicy string `json:"image_pull_policy" mapstructure:"image_pull_policy"`
	// ServiceAccount is the service account for session pods
	ServiceAccount string `json:"service_account" mapstructure:"service_account"`
	// DisableServiceLinks prevents Kubernetes Service environment variables from being injected into session pods.
	DisableServiceLinks bool `json:"disable_service_links" mapstructure:"disable_service_links"`
	// DisableServiceAccountToken prevents Kubernetes service account credentials from being mounted into session pods.
	DisableServiceAccountToken bool `json:"disable_service_account_token" mapstructure:"disable_service_account_token"`
	// BasePort is the pod-local loopback port that agentapi listens on. It is not
	// exposed by the session Service.
	BasePort int `json:"base_port" mapstructure:"base_port"`
	// CPURequest is the CPU request for session pods
	CPURequest string `json:"cpu_request" mapstructure:"cpu_request"`
	// CPULimit is the CPU limit for session pods
	CPULimit string `json:"cpu_limit" mapstructure:"cpu_limit"`
	// MemoryRequest is the memory request for session pods
	MemoryRequest string `json:"memory_request" mapstructure:"memory_request"`
	// MemoryLimit is the memory limit for session pods
	MemoryLimit string `json:"memory_limit" mapstructure:"memory_limit"`
	// ExecutorEnabled runs model-generated shell commands in a memory-isolated sidecar.
	ExecutorEnabled       bool   `json:"executor_enabled" mapstructure:"executor_enabled"`
	ExecutorCPURequest    string `json:"executor_cpu_request" mapstructure:"executor_cpu_request"`
	ExecutorCPULimit      string `json:"executor_cpu_limit" mapstructure:"executor_cpu_limit"`
	ExecutorMemoryRequest string `json:"executor_memory_request" mapstructure:"executor_memory_request"`
	ExecutorMemoryLimit   string `json:"executor_memory_limit" mapstructure:"executor_memory_limit"`
	// MoliEnabled injects a Pod-local Moli CDP browser into every session.
	MoliEnabled       bool   `json:"moli_enabled" mapstructure:"moli_enabled"`
	MoliImage         string `json:"moli_image" mapstructure:"moli_image"`
	MoliPort          int    `json:"moli_port" mapstructure:"moli_port"`
	MoliLayout        bool   `json:"moli_layout" mapstructure:"moli_layout"`
	MoliCPURequest    string `json:"moli_cpu_request" mapstructure:"moli_cpu_request"`
	MoliCPULimit      string `json:"moli_cpu_limit" mapstructure:"moli_cpu_limit"`
	MoliMemoryRequest string `json:"moli_memory_request" mapstructure:"moli_memory_request"`
	MoliMemoryLimit   string `json:"moli_memory_limit" mapstructure:"moli_memory_limit"`
	// PVCEnabled enables PersistentVolumeClaim for session pods workdir
	// When disabled, EmptyDir is used instead (data is not persisted across pod restarts)
	PVCEnabled *bool `json:"pvc_enabled,omitempty" mapstructure:"pvc_enabled"`
	// PVCStorageClass is the storage class for session PVCs
	PVCStorageClass string `json:"pvc_storage_class" mapstructure:"pvc_storage_class"`
	// PVCStorageSize is the storage size for session PVCs
	PVCStorageSize string `json:"pvc_storage_size" mapstructure:"pvc_storage_size"`
	// PodStartTimeout is the timeout in seconds for pod startup
	PodStartTimeout int `json:"pod_start_timeout" mapstructure:"pod_start_timeout"`
	// PodStopTimeout is the timeout in seconds for pod termination
	PodStopTimeout int `json:"pod_stop_timeout" mapstructure:"pod_stop_timeout"`
	// ProvisionerToken authenticates session Pod calls to the internal
	// provisioner API.
	ProvisionerToken string `json:"provisioner_token" mapstructure:"provisioner_token"`
	// ProvisionerProxyURL is the base URL session Pods use to reach this proxy.
	ProvisionerProxyURL string `json:"provisioner_proxy_url" mapstructure:"provisioner_proxy_url"`
	// GitHubSecretName is the name of the Kubernetes Secret containing GitHub authentication credentials
	// This Secret is used by the clone-repo init container for repository cloning
	// Expected keys: GITHUB_TOKEN, GITHUB_APP_ID, GITHUB_APP_PEM, GITHUB_INSTALLATION_ID
	GitHubSecretName string `json:"github_secret_name" mapstructure:"github_secret_name"`
	// GitHubConfigSecretName is the name of the Kubernetes Secret containing GitHub configuration (non-auth)
	// This Secret contains GITHUB_API and GITHUB_URL for GitHub Enterprise Server support
	// It is kept separate from GitHubSecretName so that params.github_token can override authentication
	// without losing Enterprise Server URL settings
	GitHubConfigSecretName string `json:"github_config_secret_name" mapstructure:"github_config_secret_name"`
	// ConfigFile is the path to an external configuration file for kubernetes session settings
	// This file can contain node_selector and tolerations settings
	ConfigFile string `json:"config_file,omitempty" mapstructure:"config_file"`
	// SessionPodTemplateFile is the path to a PodTemplateSpec YAML file merged into every session Pod.
	SessionPodTemplateFile string `json:"session_pod_template_file,omitempty" mapstructure:"session_pod_template_file"`
	// NodeSelector is a selector which must be true for the pod to fit on a node
	// Example: {"disktype": "ssd", "kubernetes.io/arch": "amd64"}
	NodeSelector map[string]string `json:"node_selector,omitempty" mapstructure:"node_selector" yaml:"node_selector"`
	// Affinity configures Kubernetes affinity rules for session pods.
	Affinity map[string]interface{} `json:"affinity,omitempty" mapstructure:"affinity" yaml:"affinity"`
	// Tolerations are tolerations for session pods to schedule onto nodes with matching taints
	Tolerations []Toleration `json:"tolerations,omitempty" mapstructure:"tolerations" yaml:"tolerations"`

	// SettingsBaseSecret is the single base Kubernetes Secret shared by all sessions.
	// It contains settings.json in the agentapi settings format (env_vars, auth_mode, bedrock,
	// mcp_servers, marketplaces, enabled_plugins, hooks) and is merged at the lowest priority
	// level during session settings generation. Team and user settings can override it.
	SettingsBaseSecret string `json:"settings_base_secret" mapstructure:"settings_base_secret"`
	// MCPServersBaseSecret is the legacy organization-wide MCP Secret. It contains
	// mcp-servers.json in the {"mcpServers": {...}} format. Keep reading it as a
	// lower-priority compatibility layer while installations migrate to settings.json.
	MCPServersBaseSecret string `json:"mcp_servers_base_secret" mapstructure:"mcp_servers_base_secret"`
	// MCPServersBaseFile is an optional mounted copy of mcp-servers.json. It lets
	// libSQL-only API Pods backfill a Kubernetes Secret without API credentials.
	MCPServersBaseFile string `json:"mcp_servers_base_file" mapstructure:"mcp_servers_base_file"`

	// OpenTelemetry Collector configuration
	// OtelCollectorEnabled enables OpenTelemetry Collector sidecar for metrics collection
	OtelCollectorEnabled bool `json:"otel_collector_enabled" mapstructure:"otel_collector_enabled"`
	// OtelCollectorScrapeInterval is the scrape interval for Claude Code metrics
	OtelCollectorScrapeInterval string `json:"otel_collector_scrape_interval" mapstructure:"otel_collector_scrape_interval"`
	// OtelCollectorClaudeCodePort is the port where Claude Code exposes metrics
	OtelCollectorClaudeCodePort int `json:"otel_collector_claude_code_port" mapstructure:"otel_collector_claude_code_port"`
	// OtelCollectorExporterPort is the port where otelcol exposes labeled metrics
	OtelCollectorExporterPort int `json:"otel_collector_exporter_port" mapstructure:"otel_collector_exporter_port"`

	// Slack Integration configuration
	// SlackBotTokenSecretName is the Kubernetes Secret name containing the Slack bot token
	// Used by agent-provisioner to pass the token to the claude-posts subprocess
	SlackBotTokenSecretName string `json:"slack_bot_token_secret_name" mapstructure:"slack_bot_token_secret_name"`
	// SlackBotTokenSecretKey is the key within the Secret that holds the Slack bot token
	// Defaults to "bot-token"
	SlackBotTokenSecretKey string `json:"slack_bot_token_secret_key" mapstructure:"slack_bot_token_secret_key"`

	// NetworkFilterImage is the container image for the iptables rule generation init
	// container and the network-filter sidecar. Defaults to ghcr.io/takutakahashi/nfa:0.12.6.
	// The init container reads the generated policy config and runs "nfa setup-iptables --output";
	// the sidecar runs "nfa proxy --deferred-policy".
	NetworkFilterImage string `json:"network_filter_image" mapstructure:"network_filter_image"`

	// Network filter sidecar resource configuration
	NetworkFilterCPURequest    string `json:"network_filter_cpu_request" mapstructure:"network_filter_cpu_request"`
	NetworkFilterCPULimit      string `json:"network_filter_cpu_limit" mapstructure:"network_filter_cpu_limit"`
	NetworkFilterMemoryRequest string `json:"network_filter_memory_request" mapstructure:"network_filter_memory_request"`
	NetworkFilterMemoryLimit   string `json:"network_filter_memory_limit" mapstructure:"network_filter_memory_limit"`

	// Network filter init container resource configuration
	NetworkFilterInitCPURequest    string `json:"network_filter_init_cpu_request" mapstructure:"network_filter_init_cpu_request"`
	NetworkFilterInitCPULimit      string `json:"network_filter_init_cpu_limit" mapstructure:"network_filter_init_cpu_limit"`
	NetworkFilterInitMemoryRequest string `json:"network_filter_init_memory_request" mapstructure:"network_filter_init_memory_request"`
	NetworkFilterInitMemoryLimit   string `json:"network_filter_init_memory_limit" mapstructure:"network_filter_init_memory_limit"`

	// DinD (Docker in Docker) sidecar configuration.
	// Sessions with docker.enabled=true in their params get a privileged DinD sidecar
	// and DOCKER_HOST set so the main container can run docker commands.

	// DinDImage is the container image for the DinD sidecar.
	// Defaults to "docker:dind" if not specified.
	DinDImage string `json:"dind_image" mapstructure:"dind_image"`

	// DinD sidecar resource configuration
	DinDCPURequest    string `json:"dind_cpu_request" mapstructure:"dind_cpu_request"`
	DinDCPULimit      string `json:"dind_cpu_limit" mapstructure:"dind_cpu_limit"`
	DinDMemoryRequest string `json:"dind_memory_request" mapstructure:"dind_memory_request"`
	DinDMemoryLimit   string `json:"dind_memory_limit" mapstructure:"dind_memory_limit"`
}
