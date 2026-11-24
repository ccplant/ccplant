import type { ProxyOperationContext } from './operation-context';
import {
  AgentAPIProxyError,
  AntigravityAuthAttempt,
  CodexDeviceAuthConfig,
  CodexDeviceAuthStart,
  CodexDeviceAuthStatus,
  CodexDeviceAuthTarget,
  CredentialsMetadata,
} from './types';

export const credentialsOperations = {
  /**
   * Get credential metadata (does not return raw data)
   * GET /credentials/{name}
   */
  async getCredentials(name: string): Promise<CredentialsMetadata | null> {
    try {
      return await this.makeRequest<CredentialsMetadata>(`/credentials/${encodeURIComponent(name)}`);
    } catch (error) {
      if (error instanceof AgentAPIProxyError && error.status === 404) {
        return null;
      }
      throw error;
    }
  },

  /**
   * Upload or replace credential JSON data
   * PUT /credentials/{name}
   */
  async uploadCredentials(name: string, data: Record<string, unknown>, type?: string): Promise<CredentialsMetadata> {
    if (this.debug) {
      console.log(`[AgentAPIProxy] Uploading credentials: ${name}`);
    }
    const query = type ? `?type=${encodeURIComponent(type)}` : '';
    return await this.makeRequest<CredentialsMetadata>(`/credentials/${encodeURIComponent(name)}${query}`, {
      method: 'PUT',
      body: JSON.stringify(data),
    });
  },

  /**
   * Delete credentials
   * DELETE /credentials/{name}
   */
  async deleteCredentials(name: string): Promise<void> {
    if (this.debug) {
      console.log(`[AgentAPIProxy] Deleting credentials: ${name}`);
    }
    await this.makeRequest<void>(`/credentials/${encodeURIComponent(name)}`, {
      method: 'DELETE',
    });
  },

  /**
   * Check if Codex device auth is configured on the proxy
   * GET /codex/device-auth/config
   */
  async getCodexDeviceAuthConfig(): Promise<CodexDeviceAuthConfig> {
    return await this.makeRequest<CodexDeviceAuthConfig>('/codex/device-auth/config');
  },

  /**
   * Start Codex OAuth device authorization flow
   * POST /codex/device-auth
   */
  async startCodexDeviceAuth(target?: CodexDeviceAuthTarget): Promise<CodexDeviceAuthStart> {
    if (this.debug) {
      console.log('[AgentAPIProxy] Starting Codex device auth');
    }
    return await this.makeRequest<CodexDeviceAuthStart>('/codex/device-auth', {
      method: 'POST',
      ...(target ? { body: JSON.stringify(target) } : {}),
    });
  },

  /**
   * Poll for Codex device auth status
   * POST /codex/device-auth/token
   */
  async pollCodexDeviceAuth(): Promise<CodexDeviceAuthStatus> {
    return await this.makeRequest<CodexDeviceAuthStatus>('/codex/device-auth/token', {
      method: 'POST',
    });
  },

  async getCodexDeviceAuthAttempt(attemptId: string): Promise<CodexDeviceAuthStart> {
    return await this.makeRequest<CodexDeviceAuthStart>(`/codex/device-auth/${encodeURIComponent(attemptId)}`);
  },

  async cancelCodexDeviceAuth(attemptId: string): Promise<void> {
    await this.makeRequest<void>(`/codex/device-auth/${encodeURIComponent(attemptId)}`, { method: 'DELETE' });
  },

  async getAntigravityAuthConfig(
    target?: Pick<CodexDeviceAuthTarget, 'scope' | 'team_id'>
  ): Promise<CodexDeviceAuthConfig> {
    const query = new URLSearchParams();
    if (target?.scope) query.set('scope', target.scope);
    if (target?.team_id) query.set('team_id', target.team_id);
    const suffix = query.size ? `?${query.toString()}` : '';
    return await this.makeRequest<CodexDeviceAuthConfig>(`/antigravity/auth/config${suffix}`);
  },

  async startAntigravitySSO(target: CodexDeviceAuthTarget): Promise<AntigravityAuthAttempt> {
    return await this.makeRequest<AntigravityAuthAttempt>('/antigravity/auth/sso', {
      method: 'POST',
      body: JSON.stringify(target),
    });
  },

  async getAntigravityAuthAttempt(attemptId: string): Promise<AntigravityAuthAttempt> {
    return await this.makeRequest<AntigravityAuthAttempt>(`/antigravity/auth/${encodeURIComponent(attemptId)}`);
  },

  async submitAntigravityAuthInput(attemptId: string, input: string): Promise<void> {
    await this.makeRequest<void>(`/antigravity/auth/${encodeURIComponent(attemptId)}/input`, {
      method: 'POST',
      body: JSON.stringify({ input }),
    });
  },

  async cancelAntigravityAuth(attemptId: string): Promise<void> {
    await this.makeRequest<void>(`/antigravity/auth/${encodeURIComponent(attemptId)}`, { method: 'DELETE' });
  },
} satisfies ThisType<ProxyOperationContext>;

export type CredentialsOperations = typeof credentialsOperations;
