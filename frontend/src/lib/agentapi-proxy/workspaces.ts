import { SessionContextTemplate, Workspace } from '../../types/agentapi';
import type { ProxyOperationContext } from './operation-context';

export const workspacesOperations = {
  async saveSessionAsWorkspace(sessionId: string, name: string, description = ''): Promise<Workspace> {
    return this.makeRequest<Workspace>(`/sessions/${encodeURIComponent(sessionId)}/workspace`, {
      method: 'POST',
      headers: { 'Idempotency-Key': crypto.randomUUID() },
      body: JSON.stringify({ name, description, wait_for_idle: true }),
    });
  },

  async listWorkspaces(): Promise<{ workspaces: Workspace[] }> {
    return this.makeRequest('/workspaces');
  },

  async getWorkspace(workspaceId: string): Promise<Workspace> {
    return this.makeRequest(`/workspaces/${encodeURIComponent(workspaceId)}`);
  },

  async updateWorkspace(workspaceId: string, input: { name?: string; description?: string }): Promise<Workspace> {
    return this.makeRequest(`/workspaces/${encodeURIComponent(workspaceId)}`, {
      method: 'PATCH',
      body: JSON.stringify(input),
    });
  },

  async deleteWorkspace(workspaceId: string): Promise<void> {
    await this.makeRequest(`/workspaces/${encodeURIComponent(workspaceId)}`, { method: 'DELETE' });
  },

  async listSessionContextTemplates(): Promise<{ templates: SessionContextTemplate[] }> {
    return this.makeRequest('/session-context-templates');
  },

  async deleteSessionContextTemplate(templateId: string): Promise<void> {
    await this.makeRequest(`/session-context-templates/${encodeURIComponent(templateId)}`, { method: 'DELETE' });
  },
} satisfies ThisType<ProxyOperationContext>;

export type WorkspacesOperations = typeof workspacesOperations;
