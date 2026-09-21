import { ResourceScope, SandboxConfig, DockerConfig } from './agentapi';
import type { APIMCPServerConfig, ModelConnection } from './settings';

// Session profile params
export interface SessionProfileParams {
  /** Legacy pool location; the editor saves this as SessionProfileConfig.pool. */
  pool?: string;
  initial_message?: string;
  github_token?: string;
  agent_type?: string;
  model?: string;
  /** Model switching candidates offered in the ACP chat info panel. */
  model_options?: string[];
  sandbox?: SandboxConfig;
  docker?: DockerConfig;
  auth_proxy?: boolean;
  session_ttl?: string;
  unsynced_file_paths?: string[];
  credential_source?: CredentialSource;
  codex_auth_mode?: 'auth_json' | 'openai_compatible';
  claude_auth_mode?: 'oauth' | 'bedrock' | 'anthropic_compatible';
}

export type CredentialSource = 'session_user' | 'team' | 'none';

export interface ProfileFile {
  name?: string;
  path: string;
  content?: string;
  permissions?: string;
}

// Session profile config
export interface SessionProfileConfig {
  settings_team_id?: string;
  codex_connection?: ModelConnection | null;
  claude_connection?: ModelConnection | null;
  environment?: Record<string, string>;
  tags?: Record<string, string>;
  pool?: string;
  initial_message_template?: string;
  reuse_message_template?: string;
  reuse_session?: boolean;
  memory_key?: Record<string, string>;
  params?: SessionProfileParams;
  sandbox_policy_id?: string;
  session_ttl?: string;
  unsynced_file_paths?: string[];
  source_session_profile_id?: string;
  files?: ProfileFile[];
  mcp_servers?: Record<string, APIMCPServerConfig>;
}

// SessionProfile entity
export interface SessionProfile {
  id: string;
  name: string;
  description?: string;
  user_id?: string;
  scope?: ResourceScope;
  team_id?: string;
  is_default?: boolean;
  selector_tags?: Record<string, string>;
  config?: SessionProfileConfig;
  created_at: string;
  updated_at: string;
}

// Create SessionProfile request
export interface CreateSessionProfileRequest {
  name: string;
  description?: string;
  scope?: ResourceScope;
  team_id?: string;
  is_default?: boolean;
  selector_tags?: Record<string, string>;
  config?: SessionProfileConfig;
}

// Update SessionProfile request
export interface UpdateSessionProfileRequest {
  name?: string;
  description?: string;
  is_default?: boolean;
  selector_tags?: Record<string, string>;
  config?: SessionProfileConfig;
}

// SessionProfile list parameters
export interface SessionProfileListParams {
  scope?: ResourceScope;
  team_id?: string;
}

// SessionProfile list response
export interface SessionProfileListResponse {
  session_profiles: SessionProfile[];
  total?: number;
  page?: number;
  limit?: number;
}
