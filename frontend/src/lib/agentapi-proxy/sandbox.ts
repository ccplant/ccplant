import {
  CreateSandboxPolicyRequest,
  SandboxPolicy,
  SandboxPolicyListParams,
  SandboxPolicyListResponse,
  UpdateSandboxPolicyRequest,
} from '../../types/sandbox_policy';
import type { ProxyOperationContext } from './operation-context';

export const sandboxOperations = {
  /**
   * Revoke share for a session
   * DELETE /sessions/{sessionId}/share
   */
  async getSandboxPolicyDomains(
    policyId: string
  ): Promise<{ allowed: string[]; denied: string[]; ignored: string[]; updated_at?: string }> {
    return this.makeRequest<{ allowed: string[]; denied: string[]; ignored: string[]; updated_at?: string }>(
      `/sandbox-policies/${policyId}/domains`
    );
  },

  async updateIgnoredSandboxPolicyDomains(
    policyId: string,
    ignored: string[]
  ): Promise<{ allowed: string[]; denied: string[]; ignored: string[]; updated_at?: string }> {
    return this.makeRequest<{ allowed: string[]; denied: string[]; ignored: string[]; updated_at?: string }>(
      `/sandbox-policies/${policyId}/domains/ignored`,
      {
        method: 'PUT',
        body: JSON.stringify({ ignored }),
      }
    );
  },

  async getSessionSandboxDomains(sessionId: string): Promise<{ allowed: string[]; denied: string[] }> {
    if (this.debug) {
      console.log(`[AgentAPIProxy] Fetching sandbox domains for session: ${sessionId}`);
    }
    return this.makeRequest<{ allowed: string[]; denied: string[] }>(`/sessions/${sessionId}/sandbox-domains`);
  },

  async createSandboxPolicy(data: CreateSandboxPolicyRequest): Promise<SandboxPolicy> {
    return this.makeRequest<SandboxPolicy>('/sandbox-policies', {
      method: 'POST',
      body: JSON.stringify(data),
    });
  },

  async getSandboxPolicies(params?: SandboxPolicyListParams): Promise<SandboxPolicyListResponse> {
    const searchParams = new URLSearchParams();
    if (params?.scope) searchParams.set('scope', params.scope);
    if (params?.team_id) searchParams.set('team_id', params.team_id);
    const endpoint = `/sandbox-policies${searchParams.toString() ? `?${searchParams.toString()}` : ''}`;
    const result = await this.makeRequest<SandboxPolicy[] | SandboxPolicyListResponse>(endpoint);
    if (Array.isArray(result)) {
      return { sandbox_policies: result };
    }
    return { ...result, sandbox_policies: result.sandbox_policies || [] };
  },

  async getSandboxPolicy(policyId: string): Promise<SandboxPolicy> {
    return this.makeRequest<SandboxPolicy>(`/sandbox-policies/${policyId}`);
  },

  async updateSandboxPolicy(policyId: string, data: UpdateSandboxPolicyRequest): Promise<SandboxPolicy> {
    return this.makeRequest<SandboxPolicy>(`/sandbox-policies/${policyId}`, {
      method: 'PUT',
      body: JSON.stringify(data),
    });
  },

  async deleteSandboxPolicy(policyId: string): Promise<void> {
    await this.makeRequest<void>(`/sandbox-policies/${policyId}`, {
      method: 'DELETE',
    });
  },
} satisfies ThisType<ProxyOperationContext>;

export type SandboxOperations = typeof sandboxOperations;
