import { CreateShareResponse, RevokeShareResponse, ShareStatus } from '../../types/share';
import type { ProxyOperationContext } from './operation-context';
import { AgentAPIProxyError } from './types';

export const sharingOperations = {
  /**
   * Create a share URL for a session
   * POST /sessions/{sessionId}/share
   */
  async createShare(sessionId: string): Promise<CreateShareResponse> {
    if (this.debug) {
      console.log(`[AgentAPIProxy] Creating share for session: ${sessionId}`);
    }

    const result = await this.makeRequest<CreateShareResponse>(`/sessions/${sessionId}/share`, {
      method: 'POST',
    });

    if (this.debug) {
      console.log(`[AgentAPIProxy] Created share for session ${sessionId}`);
    }

    return result;
  },

  /**
   * Get share status for a session
   * GET /sessions/{sessionId}/share
   */
  async getShareStatus(sessionId: string): Promise<ShareStatus | null> {
    if (this.debug) {
      console.log(`[AgentAPIProxy] Getting share status for session: ${sessionId}`);
    }

    try {
      const result = await this.makeRequest<ShareStatus>(`/sessions/${sessionId}/share`);

      if (this.debug) {
        console.log(`[AgentAPIProxy] Share status loaded for session ${sessionId}`);
      }

      return result;
    } catch (error) {
      // If share doesn't exist (404), return null
      if (error instanceof AgentAPIProxyError && error.status === 404) {
        if (this.debug) {
          console.log(`[AgentAPIProxy] No share found for session ${sessionId}`);
        }
        return null;
      }
      throw error;
    }
  },

  async revokeShare(sessionId: string): Promise<RevokeShareResponse> {
    if (this.debug) {
      console.log(`[AgentAPIProxy] Revoking share for session: ${sessionId}`);
    }

    const result = await this.makeRequest<RevokeShareResponse>(`/sessions/${sessionId}/share`, {
      method: 'DELETE',
    });

    if (this.debug) {
      console.log(`[AgentAPIProxy] Revoked share for session ${sessionId}`);
    }

    return result;
  },
} satisfies ThisType<ProxyOperationContext>;

export type SharingOperations = typeof sharingOperations;
