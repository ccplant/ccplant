import { ActionRequest, ActionResponse, PendingAction } from '../../types/agentapi';
import type { ProxyOperationContext } from './operation-context';
import { AgentAPIProxyError } from './types';

export const sessionActionsOperations = {
  /**
   * Get pending actions for a session (e.g., AskUserQuestion)
   */
  async getPendingActions(sessionId: string): Promise<PendingAction[]> {
    try {
      const response = await this.makeRequest<ActionResponse>(`/${sessionId}/action`);
      return response.pending_actions || [];
    } catch (error) {
      // If endpoint doesn't exist (404), return empty array
      if (error instanceof AgentAPIProxyError && error.status === 404) {
        if (this.debug) {
          console.log(`[AgentAPIProxy] Action endpoint not available for session ${sessionId}`);
        }
        return [];
      }
      throw error;
    }
  },

  /**
   * Send action response (e.g., answer to AskUserQuestion)
   */
  async sendAction(sessionId: string, action: ActionRequest): Promise<void> {
    if (this.debug) {
      console.log(`[AgentAPIProxy] Sending action to session ${sessionId}`);
    }

    await this.makeRequest<void>(`/${sessionId}/action`, {
      method: 'POST',
      body: JSON.stringify(action),
    });

    if (this.debug) {
      console.log(`[AgentAPIProxy] Successfully sent action to session ${sessionId}`);
    }
  },
} satisfies ThisType<ProxyOperationContext>;

export type SessionActionsOperations = typeof sessionActionsOperations;
