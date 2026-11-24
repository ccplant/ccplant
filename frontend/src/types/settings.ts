import { MessageTemplate } from './messageTemplate';

// Settings type for routing
export type SettingsType = 'personal' | 'team'

// Marketplace 設定 (OpenAPI仕様準拠)
export interface MarketplaceConfig {
  url: string;                    // Git repository URL (必須)
}

// Bedrock 設定 (OpenAPI仕様に準拠)
export interface BedrockConfig {
  enabled: boolean;           // Bedrock を有効にするか（必須）
  model?: string;             // モデル ID（オプション、デフォルト: global.anthropic.claude-sonnet-4-5-20250929-v1:0）
  access_key_id?: string;     // AWS アクセスキー ID（チーム設定のみ）
  secret_access_key?: string; // AWS シークレットアクセスキー（チーム設定のみ）
}

// API用のMCPサーバー設定（OpenAPI仕様準拠）
export interface APIMCPServerConfig {
  type: 'stdio' | 'http' | 'sse';
  command?: string;                   // stdio用コマンド
  url?: string;                       // http/sse用URL
  args?: string[];                    // stdio用コマンド引数
  env?: Record<string, string>;       // 環境変数（書き込み時）
  env_keys?: string[];                // 環境変数キーのみ（API読み取り時）
  headers?: Record<string, string>;   // http/sse用ヘッダー（書き込み時）
  header_keys?: string[];             // ヘッダーキーのみ（API読み取り時）
}

// APIレスポンス用（セキュリティ対策：env/headersは値を含まない）
export interface APIMCPServerResponse {
  type: 'stdio' | 'http' | 'sse';
  command?: string;
  url?: string;
  args?: string[];
  env_keys?: string[];      // 環境変数キーのみ
  header_keys?: string[];   // ヘッダーキーのみ
}

// 認証モード
export type AuthMode = 'oauth' | 'bedrock' | 'anthropic_compatible';

export type ModelConnectionMode = 'auth_json' | 'openai_compatible' | 'oauth' | 'bedrock' | 'anthropic_compatible'

export interface ModelConnection {
 mode: ModelConnectionMode;
 base_url?: string; model?: string; authentication?: 'api_key' | 'none';
 default_models?: Partial<Record<ModelConnectionMode, string>>;
 api_key?: string; clear_api_key?: boolean; has_api_key?: boolean;
 context_window?: number | null; auto_compact_token_limit?: number | null; supports_reasoning_summaries?: boolean | null;
 web_search_enabled?: boolean | null;
 endpoint_path?: string;
 model_aliases?: Partial<Record<'sonnet' | 'opus' | 'haiku', string>> | null;
}


export interface GoogleOAuthStatus {
  enabled: boolean;
  health_ok: boolean;
  health_status?: string;
  client_configured: boolean;
  connected: boolean;
  credential?: string;
  user_namespace?: string;
  oauth_start_url?: string;
  authorization_url_endpoint?: string;
  proxy_configured: boolean;
}

export interface IntegrationScope {
  id: string;
  name: string;
  desc?: string;
  group?: string;
  group_name?: string;
  group_desc?: string;
  enabled: boolean;
}

export interface SciaIntegration {
  id: string;
  provider: string;
  namespace?: string;
  credential_id: string;
  name: string;
  icon_url?: string;
  description?: string;
  released: boolean;
  source?: string;
  start_url: string;
  authorization_url_endpoint?: string;
  setup?: Record<string, string>;
  scopes: IntegrationScope[];
  connected: boolean;
}

export interface SciaIntegrationsResponse {
  enabled: boolean;
  health_ok: boolean;
  health_status?: string;
  integrations: SciaIntegration[];
}

export interface SciaAuthorizationURLResponse {
  credential_id: string;
  authorization_url: string;
  auth_url?: string;
  redirect_uri?: string;
  scope?: string;
}

export interface SciaRevokeResponse {
  revoked: boolean;
  credential_id?: string;
}

// 設定データ（API で保存）
export interface SettingsData {
 codex_connection?: ModelConnection;
 claude_connection?: ModelConnection;
  default_openai_base_url?: string;
  default_anthropic_base_url?: string;
  bedrock?: BedrockConfig;
  mcp_servers?: Record<string, APIMCPServerConfig>;
  marketplaces?: Record<string, MarketplaceConfig>;
  enabled_plugins?: string[];  // "plugin@marketplace" 形式のプラグインリスト (例: "commit@claude-plugins-official")
  skills?: string[];  // skills.sh package source (例: "vercel-labs/agent-skills")
  claude_code_oauth_token?: string;  // Claude Code OAuth トークン（保存時のみ使用）
  has_claude_code_oauth_token?: boolean;  // OAuth トークンが設定されているか（読み取り時のみ）
  auth_mode?: AuthMode;  // 認証モード（oauth または bedrock）
  env_vars?: Record<string, string>;  // カスタム環境変数（保存時のみ使用）
  env_var_keys?: string[];  // 環境変数のキーのみ（読み取り時のみ、セキュリティのため値は含まない）
  preferred_team_id?: string;  // 使用するチームの ID（"org/team-slug" 形式）
  github_app_installation_id?: string;  // Team sessions で使用する GitHub App installation ID
  github_repositories?: string[];  // clone を許可する owner/repository 全体に適用する RE2 正規表現
  slack_user_id?: string;  // Slack User ID（設定すると Slack DM 通知が有効になる）
  notification_channels?: string[];  // Active notification channels (e.g. ["web", "slack"])
  external_session_managers?: ExternalSessionManagerConfig[];  // External session managers
  default_session_profile_id?: string;  // Default session profile ID for this settings scope
  enabled_team_session_profile_ids?: string[]; // Team profiles opted into personal-session use
  default_agent_type?: Exclude<AgentApiType, 'default' | 'claude-legacy'>;  // Personal or team default for sessions without an explicit agent type
  auto_suspend?: {
    enabled: boolean;
    idle_timeout_minutes: number;
  };
}

// External session manager configuration
export interface ExternalSessionManagerConfig {
  id?: string;          // Auto-generated by server if empty
  instance_id?: string; // Stable native host installation ID
  name: string;         // Human-readable name
  has_connection_token?: boolean; // Response flag for allocator connection token
  connection_token?: string; // Returned only immediately after token generation
  labels?: Record<string, string>; // Matches allocator.* session tags
  version?: string;
  active_sessions?: number;
  last_heartbeat_at?: string;
}

export interface ExternalSessionManagerOperationalStatus {
  status: 'online' | 'offline'
  version: string
  active_sessions: number
  uptime_seconds: number
  capabilities: string[]
}

export interface ExternalSessionManagerLogs {
  lines: string[]
  source: string
}

// Available manager entry from GET /settings/managers
export interface AvailableManager {
  id: string;
  name: string;
  pool: string;
  pool_enabled?: boolean;
  automatic_assignment_enabled?: boolean;
  source: 'user' | 'team';  // Where this manager comes from
  source_name: string;       // User ID or team ID
}

export interface ExternalSessionManagerRegistrationToken {
  manager_id: string;
  registration_token: string;
  expires_at: string;
}

// Personal settings
export type PersonalSettings = SettingsData

// Team settings
export interface TeamSettings extends SettingsData {
  teamId?: string
  teamName?: string
}

export interface MCPServerConfig {
  id: string;
  name: string;
  endpoint: string;
  enabled: boolean;
  transport: 'stdio' | 'sse' | 'websocket';
  command?: string;
  args?: string[];
  env?: Record<string, string>;
  timeout?: number;
}

export interface RepositoryHistoryItem {
  repository: string;
  lastUsed: Date;
}

export interface AgentApiProxySettings {
  endpoint: string
  enabled: boolean
  timeout: number
  apiKey: string
}

// Enter キーの動作設定
export type EnterKeyBehavior = 'send' | 'newline'

// Font settings
export type FontFamily = 'sans-serif' | 'monospace'

export interface FontSettings {
  fontSize: number  // Font size in px (12-20)
  fontFamily: FontFamily
}

// セッション作成時に使用するエージェント種別
// 'default': agent_type を送信しない（バックエンドのデフォルト動作）
// 'auto': Codex auth.json があれば codex-acp、それ以外は claude-acp
// 'claude-legacy': 従来の agentapi + Claude CLI を使用
// 'claude-acp': claude-acp を使用
// 'codex-acp': codex-acp を使用
// 'pi': Pi + pi-acp + pi-cloud を使用
// 'cursor': Cursor ACP を使用
// 'antigravity': Google Antigravity を使用
export type AgentApiType = 'default' | 'auto' | 'claude-legacy' | 'claude-acp' | 'codex-acp' | 'pi' | 'cursor' | 'antigravity'

export interface GlobalSettings {
  agentApiProxy: AgentApiProxySettings
  mcpServers: MCPServerConfig[]
  repositoryHistory: RepositoryHistoryItem[]
  messageTemplates: MessageTemplate[]
  agentApiType?: AgentApiType  // デフォルト 'auto'
  enterKeyBehavior?: EnterKeyBehavior  // デフォルト 'newline' (Enter で改行、Command/Ctrl+Enter で送信)
  fontSettings?: FontSettings  // デフォルト { fontSize: 14, fontFamily: 'sans-serif' }
  acpServerEnabled?: boolean  // デフォルト false（グローバル ACP サーバーモードの有効/無効）
  created_at: string
  updated_at: string
}

export interface SettingsFormData {
  mcpServers: MCPServerConfig[]
}

const trimRecord = (values?: Record<string, string>): Record<string, string> | undefined => {
  if (!values) return undefined
  const trimmed = Object.entries(values).reduce<Record<string, string>>((result, [key, value]) => {
    if (key.trim() && value.trim()) result[key.trim()] = value.trim()
    return result
  }, {})
  return Object.keys(trimmed).length > 0 ? trimmed : undefined
}

const trimRecordKeys = (values: Record<string, string>): Record<string, string> | undefined => {
  const trimmed = Object.entries(values).reduce<Record<string, string>>((result, [key, value]) => {
    if (key.trim()) result[key.trim()] = value.trim()
    return result
  }, {})
  return Object.keys(trimmed).length > 0 ? trimmed : undefined
}

const prepareBedrock = (source: BedrockConfig): BedrockConfig => ({
  enabled: source.enabled,
  ...(source.model?.trim() ? { model: source.model.trim() } : {}),
  ...(source.access_key_id?.trim() ? { access_key_id: source.access_key_id.trim() } : {}),
  ...(source.secret_access_key?.trim() ? { secret_access_key: source.secret_access_key.trim() } : {}),
})

const prepareMCPServer = (server: APIMCPServerConfig): APIMCPServerConfig => {
  const prepared: APIMCPServerConfig = { type: server.type }
  if (server.type === 'stdio') {
    const args = server.args?.filter(value => value.trim())
    if (server.command?.trim()) prepared.command = server.command.trim()
    if (args?.length) prepared.args = args
  } else {
    if (server.url?.trim()) prepared.url = server.url.trim()
    const headers = trimRecord(server.headers)
    if (headers) prepared.headers = headers
  }
  const env = trimRecord(server.env)
  if (env) prepared.env = env
  return prepared
}

const prepareMCPServers = (servers: Record<string, APIMCPServerConfig>): Record<string, APIMCPServerConfig> | undefined => {
  const prepared = Object.entries(servers).reduce<Record<string, APIMCPServerConfig>>((result, [name, server]) => {
    if (name.trim() && server.type) result[name.trim()] = prepareMCPServer(server)
    return result
  }, {})
  return Object.keys(prepared).length > 0 ? prepared : undefined
}

const prepareMarketplaces = (marketplaces: Record<string, MarketplaceConfig>): Record<string, MarketplaceConfig> =>
  Object.entries(marketplaces).reduce<Record<string, MarketplaceConfig>>((result, [name, marketplace]) => {
    if (name.trim() && marketplace.url?.trim()) result[name.trim()] = { url: marketplace.url.trim() }
    return result
  }, {})

const copyConnectionSettings = (source: SettingsData, target: SettingsData): void => {
  for (const field of ['codex_connection', 'claude_connection'] as const) {
    const value = source[field]
    if (!value) continue
    const { has_api_key: ignored, ...connection } = value
    void ignored
    target[field] = connection
  }
}

const copyOptionalSessionSettings = (source: SettingsData, target: SettingsData): void => {
  if (source.preferred_team_id !== undefined) target.preferred_team_id = source.preferred_team_id
  if (source.github_app_installation_id !== undefined) target.github_app_installation_id = source.github_app_installation_id.trim()
  if (source.github_repositories !== undefined) target.github_repositories = source.github_repositories.map(value => value.trim()).filter(Boolean)
  if (source.slack_user_id !== undefined) target.slack_user_id = source.slack_user_id
  if (source.notification_channels !== undefined) target.notification_channels = source.notification_channels
  if (source.default_session_profile_id !== undefined) target.default_session_profile_id = source.default_session_profile_id
  if (source.enabled_team_session_profile_ids !== undefined) target.enabled_team_session_profile_ids = source.enabled_team_session_profile_ids
  if (source.auto_suspend !== undefined) target.auto_suspend = source.auto_suspend
}

const copyExternalManagers = (source: SettingsData, target: SettingsData): void => {
  if (source.external_session_managers === undefined) return
  target.external_session_managers = source.external_session_managers.map(({ id, instance_id, name, labels }) => ({
    id,
    instance_id,
    name,
    labels,
  }))
}

// 保存前に空の値を除外した設定データを作成
export const prepareSettingsForSave = (data: SettingsData): SettingsData => {
  const prepared: SettingsData = {}
  if (data.bedrock) prepared.bedrock = prepareBedrock(data.bedrock)
  if (data.mcp_servers) {
    const servers = prepareMCPServers(data.mcp_servers)
    if (servers) prepared.mcp_servers = servers
  }
  if (data.marketplaces !== undefined) prepared.marketplaces = prepareMarketplaces(data.marketplaces)
  if (data.enabled_plugins !== undefined) prepared.enabled_plugins = data.enabled_plugins.map(value => value.trim()).filter(Boolean)
  if (data.skills !== undefined) prepared.skills = [...new Set(data.skills.map(value => value.trim()).filter(Boolean))]
  if (data.claude_code_oauth_token !== undefined) prepared.claude_code_oauth_token = data.claude_code_oauth_token
  if (data.auth_mode) prepared.auth_mode = data.auth_mode
  if (data.env_vars !== undefined) {
    const envVars = trimRecordKeys(data.env_vars)
    if (envVars) prepared.env_vars = envVars
  }
  copyConnectionSettings(data, prepared)
  copyOptionalSessionSettings(data, prepared)
  copyExternalManagers(data, prepared)
  return prepared
}

// Default settings
export const getDefaultSettings = (): SettingsFormData => ({
  mcpServers: []
})

// Get proxy settings
export const getDefaultProxySettingsAsync = async (): Promise<AgentApiProxySettings> => {
  const endpoint = await getCurrentHostProxyUrlAsync()

  return {
    endpoint,
    enabled: true,
    timeout: 30000,
    apiKey: ''
  }
}

// Get the current hostname for proxy URL from request headers
export function getCurrentHostProxyUrlFromHeaders(requestHeaders?: HeadersInit): string {
  if (requestHeaders) {
    const headersObj = requestHeaders instanceof Headers ? requestHeaders : new Headers(requestHeaders)
    const host = headersObj.get('host')
    const protocol = headersObj.get('x-forwarded-proto') || 
                    headersObj.get('x-forwarded-scheme') ||
                    (headersObj.get('x-forwarded-ssl') === 'on' ? 'https' : 'http')
    
    if (host) {
      return `${protocol}://${host}/api/v1`
    }
  }
  
  // Fallback to localhost for development
  return 'http://localhost:3000/api/v1'
}

// Get the current hostname for proxy URL
function getCurrentHostProxyUrl(): string {
  if (typeof window === 'undefined') {
    // Server-side: fallback to localhost for development
    // Note: headers() is async in Next.js 15, so we can't use it here
    return 'http://localhost:3000/api/v1'
  }
  
  // Client-side: construct URL from current hostname
  const protocol = window.location.protocol
  const hostname = window.location.hostname
  const port = window.location.port
  
  // Construct the base URL
  let baseUrl = `${protocol}//${hostname}`
  if (port && port !== '80' && port !== '443') {
    baseUrl += `:${port}`
  }
  
  return `${baseUrl}/api/v1`
}

// Async version for server-side use with Next.js 15
export async function getCurrentHostProxyUrlAsync(): Promise<string> {
  if (typeof window === 'undefined') {
    // Server-side: try to get hostname from request headers
    try {
      const { headers } = await import('next/headers')
      const headersList = await headers()
      const host = headersList.get('host')
      const protocol = headersList.get('x-forwarded-proto') || 
                      headersList.get('x-forwarded-scheme') ||
                      (headersList.get('x-forwarded-ssl') === 'on' ? 'https' : 'http')
      
      if (host) {
        return `${protocol}://${host}/api/v1`
      }
    } catch (error) {
      // headers() might not be available in all contexts
      console.warn('Failed to get request headers for proxy URL:', error)
    }
    
    // Fallback to localhost for development
    return 'http://localhost:3000/api/v1'
  }
  
  // Client-side: construct URL from current hostname
  const protocol = window.location.protocol
  const hostname = window.location.hostname
  const port = window.location.port
  
  // Construct the base URL
  let baseUrl = `${protocol}//${hostname}`
  if (port && port !== '80' && port !== '443') {
    baseUrl += `:${port}`
  }
  
  return `${baseUrl}/api/v1`
}

// Default proxy settings for profiles
export const getDefaultProxySettings = (): AgentApiProxySettings => {
  const endpoint = getCurrentHostProxyUrl()
  
  return {
    endpoint,
    enabled: true,
    timeout: 30000,
    apiKey: ''
  }
}



// Global settings utilities
export const loadGlobalSettings = (): SettingsFormData => {
  // Check if we're in a browser environment
  if (typeof window === 'undefined') {
    return getDefaultSettings()
  }
  
  try {
    const savedSettings = localStorage.getItem('agentapi-global-settings')
    if (savedSettings) {
      const parsedSettings = JSON.parse(savedSettings)
      // Ensure all required properties exist with proper defaults
      return mergeWithDefaults(parsedSettings, getDefaultSettings())
    }
  } catch (err) {
    console.error('Failed to load global settings:', err)
  }
  return getDefaultSettings()
}

export const saveGlobalSettings = (settings: SettingsFormData): void => {
  // Check if we're in a browser environment
  if (typeof window === 'undefined') {
    console.warn('Cannot save settings: localStorage not available (server-side)')
    return
  }
  
  try {
    localStorage.setItem('agentapi-global-settings', JSON.stringify(settings))
  } catch (err) {
    console.error('Failed to save global settings:', err)
    throw err
  }
}

// Repository settings utilities with hierarchy support
export const loadRepositorySettings = (repoFullname: string): SettingsFormData => {
  const globalSettings = loadGlobalSettings()
  
  // Check if we're in a browser environment
  if (typeof window === 'undefined') {
    return globalSettings
  }
  
  try {
    const savedSettings = localStorage.getItem(`agentapi-settings-${repoFullname}`)
    if (savedSettings) {
      const repoSettings = JSON.parse(savedSettings)
      // Ensure repository settings have proper structure before merging
      const safeRepoSettings = mergeWithDefaults(repoSettings, getDefaultSettings())
      // Merge global settings with repository-specific overrides
      return mergeSettings(globalSettings, safeRepoSettings)
    }
  } catch (err) {
    console.error('Failed to load repository settings:', err)
  }
  
  // Return global settings as fallback
  return globalSettings
}

export const saveRepositorySettings = (repoFullname: string, settings: SettingsFormData): void => {
  // Check if we're in a browser environment
  if (typeof window === 'undefined') {
    console.warn('Cannot save repository settings: localStorage not available (server-side)')
    return
  }
  
  try {
    localStorage.setItem(`agentapi-settings-${repoFullname}`, JSON.stringify(settings))
  } catch (err) {
    console.error('Failed to save repository settings:', err)
    throw err
  }
}

// Safely merge partial settings with defaults to ensure all properties exist
const mergeWithDefaults = (partialSettings: Partial<SettingsFormData> | null | undefined, defaultSettings: SettingsFormData): SettingsFormData => {
  return {
    mcpServers: Array.isArray(partialSettings?.mcpServers)
      ? partialSettings.mcpServers
      : defaultSettings.mcpServers
  }
}

// Merge settings with hierarchy: global settings as base, repo settings as overrides
const mergeSettings = (globalSettings: SettingsFormData, repoSettings: SettingsFormData): SettingsFormData => {
  return {
    mcpServers: [
      ...(globalSettings.mcpServers || []),
      ...(repoSettings.mcpServers || []).filter(repoServer =>
        !(globalSettings.mcpServers || []).some(globalServer => globalServer.id === repoServer.id)
      )
    ]
  }
}

// Get effective settings for display (what would actually be used)
export const getEffectiveSettings = (repoFullname: string): SettingsFormData => {
  return loadRepositorySettings(repoFullname)
}

// Full global settings (with all fields)
const FULL_GLOBAL_SETTINGS_KEY = 'agentapi-full-global-settings'

export const normalizeAgentApiProxyEndpoint = (endpoint: string): string =>
  endpoint.replace(/\/api\/proxy\/?$/, '/api/v1')

export const getDefaultFullGlobalSettings = (): GlobalSettings => {
  const now = new Date().toISOString()
  return {
    agentApiProxy: getDefaultProxySettings(),
    mcpServers: [],
    repositoryHistory: [],
    messageTemplates: [],
    created_at: now,
    updated_at: now
  }
}

export const loadFullGlobalSettings = (): GlobalSettings => {
  if (typeof window === 'undefined') {
    return getDefaultFullGlobalSettings()
  }

  try {
    const savedSettings = localStorage.getItem(FULL_GLOBAL_SETTINGS_KEY)
    if (savedSettings) {
      const parsedSettings = JSON.parse(savedSettings)
      // Convert repositoryHistory lastUsed to Date objects
      if (parsedSettings.repositoryHistory) {
        parsedSettings.repositoryHistory = parsedSettings.repositoryHistory.map(
          (item: { repository: string; lastUsed: string | Date }) => ({
            ...item,
            lastUsed: new Date(item.lastUsed)
          })
        )
      }
      const defaults = getDefaultFullGlobalSettings()
      const agentApiProxy = {
        ...defaults.agentApiProxy,
        ...parsedSettings.agentApiProxy,
      }
      agentApiProxy.endpoint = normalizeAgentApiProxyEndpoint(agentApiProxy.endpoint)
      return { ...defaults, ...parsedSettings, agentApiProxy }
    }
  } catch (err) {
    console.error('Failed to load full global settings:', err)
  }
  return getDefaultFullGlobalSettings()
}

export const saveFullGlobalSettings = (settings: GlobalSettings): void => {
  if (typeof window === 'undefined') {
    console.warn('Cannot save full global settings: localStorage not available (server-side)')
    return
  }

  try {
    // Convert repositoryHistory Date objects to ISO strings
    const settingsToSave = {
      ...settings,
      repositoryHistory: settings.repositoryHistory.map(item => ({
        ...item,
        lastUsed: item.lastUsed instanceof Date ? item.lastUsed.toISOString() : item.lastUsed
      })),
      updated_at: new Date().toISOString()
    }
    localStorage.setItem(FULL_GLOBAL_SETTINGS_KEY, JSON.stringify(settingsToSave))
  } catch (err) {
    console.error('Failed to save full global settings:', err)
    throw err
  }
}

// Repository history utilities
const MAX_REPOSITORY_HISTORY = 10

export const addRepositoryToHistory = (repository: string): void => {
  if (!repository.trim()) return

  const settings = loadFullGlobalSettings()
  const existingIndex = settings.repositoryHistory.findIndex(
    item => item.repository === repository
  )

  const now = new Date()

  if (existingIndex !== -1) {
    settings.repositoryHistory[existingIndex].lastUsed = now
  } else {
    settings.repositoryHistory.unshift({
      repository,
      lastUsed: now
    })
  }

  settings.repositoryHistory.sort((a, b) => b.lastUsed.getTime() - a.lastUsed.getTime())
  settings.repositoryHistory = settings.repositoryHistory.slice(0, MAX_REPOSITORY_HISTORY)

  saveFullGlobalSettings(settings)
}

export const getRepositoryHistory = (): RepositoryHistoryItem[] => {
  const settings = loadFullGlobalSettings()
  return settings.repositoryHistory
}

// Message template utilities
export const getMessageTemplates = (): MessageTemplate[] => {
  const settings = loadFullGlobalSettings()
  return settings.messageTemplates
}

export const saveMessageTemplates = (templates: MessageTemplate[]): void => {
  const settings = loadFullGlobalSettings()
  settings.messageTemplates = templates
  saveFullGlobalSettings(settings)
}

// Enter Key Behavior utilities
export const getEnterKeyBehavior = (): EnterKeyBehavior => {
  const settings = loadFullGlobalSettings()
  // 未設定の既存ユーザーにも新しいデフォルトを適用する
  return settings.enterKeyBehavior ?? 'newline'
}

export const setEnterKeyBehavior = (behavior: EnterKeyBehavior): void => {
  const settings = loadFullGlobalSettings()
  settings.enterKeyBehavior = behavior
  saveFullGlobalSettings(settings)
}

// Font Settings utilities
export const getDefaultFontSettings = (): FontSettings => ({
  fontSize: 14,  // Default 14px
  fontFamily: 'sans-serif'
})

export const getFontSettings = (): FontSettings => {
  const settings = loadFullGlobalSettings()
  return settings.fontSettings ?? getDefaultFontSettings()
}

export const setFontSettings = (fontSettings: FontSettings): void => {
  const settings = loadFullGlobalSettings()
  settings.fontSettings = fontSettings
  saveFullGlobalSettings(settings)

  // Dispatch custom event to notify other components
  if (typeof window !== 'undefined') {
    window.dispatchEvent(new CustomEvent('fontSettingsChanged', { detail: fontSettings }))
  }
}

// AgentAPI Type utilities
export const getAgentApiType = (): AgentApiType => {
  const settings = loadFullGlobalSettings()
  if (
    settings.agentApiType === 'auto' ||
    settings.agentApiType === 'claude-legacy' ||
    settings.agentApiType === 'claude-acp' ||
    settings.agentApiType === 'codex-acp' ||
    settings.agentApiType === 'pi' ||
    settings.agentApiType === 'cursor' ||
    settings.agentApiType === 'antigravity'
  ) {
    return settings.agentApiType
  }
  return 'auto'
}

export const setAgentApiType = (type: AgentApiType): void => {
  const settings = loadFullGlobalSettings()
  settings.agentApiType = type
  delete (settings as { useClaudeAgentAPI?: boolean }).useClaudeAgentAPI
  saveFullGlobalSettings(settings)
}

// ACP Server Mode utilities
export const getACPServerEnabled = (): boolean => {
  const settings = loadFullGlobalSettings()
  return settings.acpServerEnabled ?? false
}

export const setACPServerEnabled = (enabled: boolean): void => {
  const settings = loadFullGlobalSettings()
  settings.acpServerEnabled = enabled
  saveFullGlobalSettings(settings)
}
export interface SecretProjection {
  key: string
  type: 'env' | 'file'
  env_name?: string
  path?: string
  permissions?: '0400' | '0600'
}

export interface SettingsSecret {
  id: string
  name: string
  keys: string[]
  projections?: SecretProjection[]
  version: number
  created_at: string
  updated_at: string
}
