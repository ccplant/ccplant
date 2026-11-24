import {
  AvailableManager,
  ExternalSessionManagerConfig,
  ExternalSessionManagerLogs,
  ExternalSessionManagerOperationalStatus,
  ExternalSessionManagerRegistrationToken,
} from '../../types/settings';
import type { ProxyOperationContext } from './operation-context';
import { AgentAPIProxyError } from './types';

export const externalManagersOperations = {
  /**
   * Get all external session managers available to the authenticated user
   * (from user's own settings + all team settings they belong to)
   * GET /settings/managers
   */
  async getAvailableManagers(): Promise<AvailableManager[]> {
    try {
      const result = await this.makeRequest<{ managers: AvailableManager[] }>('/settings/managers');
      return result.managers || [];
    } catch (error) {
      if (error instanceof AgentAPIProxyError && error.status === 404) {
        if (this.debug) {
          console.log('[AgentAPIProxy] /settings/managers not available, returning empty list');
        }
        return [];
      }
      throw error;
    }
  },

  async issueExternalSessionManagerRegistrationToken(
    scope: 'user' | 'team' = 'user',
    teamId?: string
  ): Promise<ExternalSessionManagerRegistrationToken> {
    const result = await this.makeRequest<{ manager: { id: string }; registration_token: string; expires_at: string }>(
      '/session-managers/registration-tokens',
      {
        method: 'POST',
        body: JSON.stringify({ name: `session-manager-${Date.now()}`, scope, ...(teamId ? { team_id: teamId } : {}) }),
      }
    );
    return {
      manager_id: result.manager.id,
      registration_token: result.registration_token,
      expires_at: result.expires_at,
    };
  },

  async rotateExternalSessionManagerToken(
    managerId: string,
    scope: 'user' | 'team' = 'user',
    teamId?: string
  ): Promise<ExternalSessionManagerConfig> {
    void scope;
    void teamId;
    const result = await this.makeRequest<{ registration_token: string }>(
      `/session-managers/${encodeURIComponent(managerId)}/registration-token`,
      {
        method: 'POST',
      }
    );
    return {
      id: managerId,
      name: managerId,
      connection_token: result.registration_token,
    } as ExternalSessionManagerConfig;
  },

  async getExternalSessionManagerOperationalStatus(
    managerId: string,
    scope: 'user' | 'team' = 'user',
    teamId?: string
  ): Promise<ExternalSessionManagerOperationalStatus> {
    void managerId;
    void scope;
    void teamId;
    throw new Error('Session Manager operational controls are not available through the unified manager API');
  },

  async getExternalSessionManagerLogs(
    managerId: string,
    scope: 'user' | 'team' = 'user',
    teamId?: string,
    tail = 200
  ): Promise<ExternalSessionManagerLogs> {
    void managerId;
    void scope;
    void teamId;
    void tail;
    throw new Error('Session Manager logs are not available through the unified manager API');
  },

  async restartExternalSessionManager(
    managerId: string,
    scope: 'user' | 'team' = 'user',
    teamId?: string
  ): Promise<void> {
    void managerId;
    void scope;
    void teamId;
    throw new Error('Session Manager restart is not available through the unified manager API');
  },

  async upgradeExternalSessionManager(
    managerId: string,
    version: string,
    scope: 'user' | 'team' = 'user',
    teamId?: string
  ): Promise<void> {
    void managerId;
    void version;
    void scope;
    void teamId;
    throw new Error('Session Manager upgrade is managed by ccplant session-manager install');
  },
} satisfies ThisType<ProxyOperationContext>;

export type ExternalManagersOperations = typeof externalManagersOperations;
