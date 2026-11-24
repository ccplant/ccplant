import {
  ApiToken,
  ApiTokenListParams,
  ApiTokenListResponse,
  CreateApiTokenRequest,
  CreateApiTokenResponse,
} from '../../types/api_token';
import type { ProxyOperationContext } from './operation-context';

export const apiTokensOperations = {
  /**
   * List API tokens for a scope. Use scope="personal" for the user's tokens,
   * or scope="team" with team_id for a team's tokens.
   */
  async getApiTokens(params: ApiTokenListParams): Promise<ApiTokenListResponse> {
    const searchParams = new URLSearchParams();
    if (params?.scope) searchParams.set('scope', params.scope);
    if (params?.team_id) searchParams.set('team_id', params.team_id);

    const endpoint = `/api-tokens${searchParams.toString() ? `?${searchParams.toString()}` : ''}`;
    const result = await this.makeRequest<ApiToken[] | ApiTokenListResponse>(endpoint);

    // Tolerate both wrapped ({items:[]}) and bare-array responses.
    if (Array.isArray(result)) {
      return { items: result };
    }
    return { items: result.items || [] };
  },

  /**
   * Create a new API token. The plaintext token is returned exactly once in
   * `plaintext_token` and must never be persisted by the caller.
   */
  async createApiToken(data: CreateApiTokenRequest): Promise<CreateApiTokenResponse> {
    if (this.debug) {
      // Never log the token itself; only the non-sensitive request fields.
      console.log('[AgentAPIProxy] Creating API token:', {
        name: data.name,
        scope: data.scope,
        team_id: data.team_id,
        permissions: data.permissions,
        expires_at: data.expires_at,
      });
    }

    const result = await this.makeRequest<CreateApiTokenResponse>('/api-tokens', {
      method: 'POST',
      body: JSON.stringify(data),
    });

    if (this.debug) {
      console.log(`[AgentAPIProxy] Created API token: ${result.token?.id}`);
    }

    return result;
  },

  /**
   * Get metadata for a single API token. Never returns the plaintext token.
   */
  async getApiToken(tokenId: string): Promise<ApiToken> {
    return this.makeRequest<ApiToken>(`/api-tokens/${encodeURIComponent(tokenId)}`);
  },

  /**
   * Delete an API token. If the deleted token backs the current UI session,
   * subsequent requests will return 401 and the existing auth flow will
   * handle re-authentication; we do not compare plaintext tokens.
   */
  async deleteApiToken(tokenId: string): Promise<void> {
    if (this.debug) {
      console.log(`[AgentAPIProxy] Deleting API token: ${tokenId}`);
    }

    await this.makeRequest<void>(`/api-tokens/${encodeURIComponent(tokenId)}`, {
      method: 'DELETE',
    });

    if (this.debug) {
      console.log(`[AgentAPIProxy] Deleted API token: ${tokenId}`);
    }
  },
} satisfies ThisType<ProxyOperationContext>;

export type ApiTokensOperations = typeof apiTokensOperations;
