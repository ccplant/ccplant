import {
  AdminSettingsDocument,
  AdminSettingsVersionsResponse,
  UpdateAdminSettingsRequest,
} from '../../types/admin-settings';
import { SettingsData } from '../../types/settings';
import type { ProxyOperationContext } from './operation-context';
import { AgentAPIProxyError } from './types';

export const settingsOperations = {
  /**
   * Get settings for a user or team
   * @param name - User name or team name
   */
  async getSettings(name: string): Promise<SettingsData> {
    try {
      return await this.makeRequest<SettingsData>(`/settings/${encodeURIComponent(name)}`);
    } catch (error) {
      // If settings don't exist (404), return empty settings
      if (error instanceof AgentAPIProxyError && error.status === 404) {
        if (this.debug) {
          console.log(`[AgentAPIProxy] Settings not found for ${name}, returning empty settings`);
        }
        return {};
      }
      throw error;
    }
  },

  async listSettingsSecrets(name: string): Promise<import('../../types/settings').SettingsSecret[]> {
    const result = await this.makeRequest<{ secrets: import('../../types/settings').SettingsSecret[] }>(
      `/settings/${encodeURIComponent(name)}/secrets`
    );
    return result.secrets || [];
  },

  async createSettingsSecret(
    name: string,
    data: {
      name: string;
      values: Record<string, string>;
      projections?: import('../../types/settings').SecretProjection[];
    }
  ): Promise<import('../../types/settings').SettingsSecret> {
    return this.makeRequest(`/settings/${encodeURIComponent(name)}/secrets`, {
      method: 'POST',
      body: JSON.stringify(data),
    });
  },

  async updateSettingsSecretValues(
    name: string,
    secretId: string,
    values: Record<string, string>,
    baseVersion: number
  ) {
    return this.makeRequest<import('../../types/settings').SettingsSecret>(
      `/settings/${encodeURIComponent(name)}/secrets/${encodeURIComponent(secretId)}/values`,
      { method: 'PUT', body: JSON.stringify({ values, base_version: baseVersion }) }
    );
  },

  async deleteSettingsSecret(name: string, secretId: string): Promise<void> {
    await this.makeRequest(`/settings/${encodeURIComponent(name)}/secrets/${encodeURIComponent(secretId)}`, {
      method: 'DELETE',
    });
  },

  /**
   * Save settings for a user or team
   * @param name - User name or team name
   * @param data - Settings data to save
   */
  async saveSettings(name: string, data: SettingsData): Promise<SettingsData> {
    if (this.debug) {
      console.log(`[AgentAPIProxy] Saving settings for: ${name}`);
    }
    const result = await this.makeRequest<SettingsData>(`/settings/${encodeURIComponent(name)}`, {
      method: 'PUT',
      body: JSON.stringify(data),
    });
    if (this.debug) {
      console.log(`[AgentAPIProxy] Successfully saved settings for: ${name}`);
    }
    return result;
  },

  /**
   * Delete settings for a user or team
   * @param name - User name or team name
   */
  async deleteSettings(name: string): Promise<void> {
    if (this.debug) {
      console.log(`[AgentAPIProxy] Deleting settings for: ${name}`);
    }
    await this.makeRequest<void>(`/settings/${encodeURIComponent(name)}`, {
      method: 'DELETE',
    });
    if (this.debug) {
      console.log(`[AgentAPIProxy] Successfully deleted settings for: ${name}`);
    }
  },

  async getAdminSettings(version?: number): Promise<AdminSettingsDocument> {
    const query = version ? `?version=${version}` : '';
    return this.makeRequest<AdminSettingsDocument>(`/admin/system-settings${query}`);
  },

  async updateAdminSettings(request: UpdateAdminSettingsRequest): Promise<AdminSettingsDocument> {
    return this.makeRequest<AdminSettingsDocument>('/admin/system-settings', {
      method: 'PUT',
      body: JSON.stringify(request),
    });
  },

  async listAdminSettingsVersions(): Promise<AdminSettingsVersionsResponse> {
    return this.makeRequest<AdminSettingsVersionsResponse>('/admin/system-settings/versions');
  },
} satisfies ThisType<ProxyOperationContext>;

export type SettingsOperations = typeof settingsOperations;
