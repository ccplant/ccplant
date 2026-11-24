import { GitHubConnection, GitHubConnectionInput, GitHubIdentitiesResponse } from '../../types/github-connection';
import { GoogleConnection, GoogleConnectionInput, GoogleIdentitiesResponse } from '../../types/google-connection';
import {
  GoogleOAuthStatus,
  SciaAuthorizationURLResponse,
  SciaIntegrationsResponse,
  SciaRevokeResponse,
} from '../../types/settings';
import type { ProxyOperationContext } from './operation-context';

export const integrationsOperations = {
  /**
   * Get GitHub OAuth URL for authentication
   */
  async getGitHubAuthUrl(): Promise<string> {
    try {
      const response = await this.makeRequest<{ authUrl: string }>('/auth/github/url', {
        method: 'POST',
        body: JSON.stringify({}),
      });
      return response.authUrl;
    } catch (error) {
      console.error('[AgentAPIProxy] Failed to get GitHub auth URL:', error);
      throw error;
    }
  },

  /**
   * Get scia Google OAuth integration status for the current user.
   */
  async getGoogleOAuthStatus(): Promise<GoogleOAuthStatus> {
    return await this.makeRequest<GoogleOAuthStatus>('/integrations/google-oauth/status');
  },

  /**
   * Get scia OAuth integrations configured for the current user.
   */
  async getIntegrations(): Promise<SciaIntegrationsResponse> {
    return await this.makeRequest<SciaIntegrationsResponse>('/integrations');
  },

  /**
   * Ask scia, through the proxy, to build an OAuth authorization URL.
   */
  async createIntegrationAuthorizationURL(
    integrationId: string,
    request: { scope_ids: string[]; redirect_uri: string }
  ): Promise<SciaAuthorizationURLResponse> {
    return await this.makeRequest<SciaAuthorizationURLResponse>(
      `/integrations/${encodeURIComponent(integrationId)}/authorization-url`,
      {
        method: 'POST',
        body: JSON.stringify(request),
      }
    );
  },

  /**
   * Revoke/disconnect a scia OAuth integration for the current user.
   */
  async revokeIntegration(integrationId: string): Promise<SciaRevokeResponse> {
    return await this.makeRequest<SciaRevokeResponse>(`/integrations/${encodeURIComponent(integrationId)}/revoke`, {
      method: 'POST',
      body: JSON.stringify({}),
    });
  },

  async listGitHubConnections(admin = false): Promise<GitHubConnection[]> {
    const response = await this.makeRequest<{ connections: GitHubConnection[] }>(
      admin ? '/admin/github-connections' : '/github-connections'
    );
    return response.connections;
  },

  async createGitHubConnection(request: GitHubConnectionInput): Promise<GitHubConnection> {
    return this.makeRequest<GitHubConnection>('/admin/github-connections', {
      method: 'POST',
      body: JSON.stringify(request),
    });
  },

  async updateGitHubConnection(id: string, request: Partial<GitHubConnectionInput>): Promise<GitHubConnection> {
    return this.makeRequest<GitHubConnection>(`/admin/github-connections/${encodeURIComponent(id)}`, {
      method: 'PATCH',
      body: JSON.stringify(request),
    });
  },

  async updateGitHubConnectionSecret(
    id: string,
    secret: NonNullable<GitHubConnectionInput['oauth_client_secret']>
  ): Promise<GitHubConnection> {
    return this.makeRequest<GitHubConnection>(`/admin/github-connections/${encodeURIComponent(id)}/secret`, {
      method: 'PUT',
      body: JSON.stringify(secret),
    });
  },

  async deleteGitHubConnection(id: string): Promise<void> {
    await this.makeRequest(`/admin/github-connections/${encodeURIComponent(id)}`, { method: 'DELETE' });
  },

  async testGitHubConnection(id: string): Promise<{ api_reachable: boolean; secret_resolvable: boolean }> {
    return this.makeRequest(`/admin/github-connections/${encodeURIComponent(id)}/test`, { method: 'POST' });
  },

  async updateGitHubAppPrivateKey(id: string, value: string): Promise<GitHubConnection> {
    return this.makeRequest<GitHubConnection>(
      `/admin/github-connections/${encodeURIComponent(id)}/github-app/private-key`,
      { method: 'PUT', body: JSON.stringify({ value }) }
    );
  },

  async testGitHubApp(id: string, repository: string): Promise<{ valid: boolean; expires_at: string }> {
    return this.makeRequest(`/admin/github-connections/${encodeURIComponent(id)}/github-app/test`, {
      method: 'POST',
      body: JSON.stringify({ repository }),
    });
  },

  async listGitHubIdentities(): Promise<GitHubIdentitiesResponse> {
    return this.makeRequest<GitHubIdentitiesResponse>('/users/me/github-identities');
  },

  async startGitHubIdentityLink(
    connectionId: string,
    returnTo: string,
    callbackUrl?: string
  ): Promise<{ authorization_url: string }> {
    return this.makeRequest<{ authorization_url: string }>('/users/me/github-identities/link', {
      method: 'POST',
      body: JSON.stringify({ connection_id: connectionId, return_to: returnTo, callback_url: callbackUrl }),
    });
  },

  async unlinkGitHubIdentity(identityId: string): Promise<void> {
    await this.makeRequest(`/users/me/github-identities/${encodeURIComponent(identityId)}`, { method: 'DELETE' });
  },

  async listGoogleConnections(admin = false): Promise<GoogleConnection[]> {
    const response = await this.makeRequest<{ connections: GoogleConnection[] }>(
      admin ? '/admin/google-connections' : '/google-connections'
    );
    return response.connections;
  },

  async createGoogleConnection(request: GoogleConnectionInput): Promise<GoogleConnection> {
    return this.makeRequest<GoogleConnection>('/admin/google-connections', {
      method: 'POST',
      body: JSON.stringify(request),
    });
  },

  async updateGoogleConnection(id: string, request: Partial<GoogleConnectionInput>): Promise<GoogleConnection> {
    return this.makeRequest<GoogleConnection>(`/admin/google-connections/${encodeURIComponent(id)}`, {
      method: 'PATCH',
      body: JSON.stringify(request),
    });
  },

  async updateGoogleConnectionSecret(
    id: string,
    secret: NonNullable<GoogleConnectionInput['oauth_client_secret']>
  ): Promise<GoogleConnection> {
    return this.makeRequest<GoogleConnection>(`/admin/google-connections/${encodeURIComponent(id)}/secret`, {
      method: 'PUT',
      body: JSON.stringify(secret),
    });
  },

  async deleteGoogleConnection(id: string): Promise<void> {
    await this.makeRequest(`/admin/google-connections/${encodeURIComponent(id)}`, { method: 'DELETE' });
  },

  async testGoogleConnection(id: string): Promise<{ discovery_reachable: boolean; secret_resolvable: boolean }> {
    return this.makeRequest(`/admin/google-connections/${encodeURIComponent(id)}/test`, { method: 'POST' });
  },

  async listGoogleIdentities(): Promise<GoogleIdentitiesResponse> {
    return this.makeRequest<GoogleIdentitiesResponse>('/users/me/google-identities');
  },

  async startGoogleIdentityLink(
    connectionId: string,
    returnTo: string,
    callbackUrl?: string
  ): Promise<{ authorization_url: string }> {
    return this.makeRequest<{ authorization_url: string }>('/users/me/google-identities/link', {
      method: 'POST',
      body: JSON.stringify({ connection_id: connectionId, return_to: returnTo, callback_url: callbackUrl }),
    });
  },

  async unlinkGoogleIdentity(identityId: string): Promise<void> {
    await this.makeRequest(`/users/me/google-identities/${encodeURIComponent(identityId)}`, { method: 'DELETE' });
  },
} satisfies ThisType<ProxyOperationContext>;

export type IntegrationsOperations = typeof integrationsOperations;
