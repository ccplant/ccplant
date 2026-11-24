import {
  AdminSessionRunner,
  ClusterSessionManager,
  LogicalSessionPool,
  SessionPoolBinding,
  SessionPoolLogs,
  SessionPoolStatusResponse,
  SessionPoolSupplier,
} from '../../types/session_pool';
import type { ProxyOperationContext } from './operation-context';

export const poolsOperations = {
  /**
   * Get the healthy Session Runner Pools available to the current user.
   */
  async getAvailableSessionPools(): Promise<LogicalSessionPool[]> {
    const result = await this.makeRequest<{ session_pools: LogicalSessionPool[] }>('/available-session-pools');
    return result.session_pools || [];
  },

  async listClusterSessionManagers(): Promise<ClusterSessionManager[]> {
    const result = await this.makeRequest<{ session_managers: ClusterSessionManager[] }>('/session-managers');
    return result.session_managers ?? [];
  },

  async createClusterSessionManager(
    name: string
  ): Promise<{ manager: ClusterSessionManager; registration_token: string; expires_at: string }> {
    return this.makeRequest('/session-managers/registration-tokens', {
      method: 'POST',
      body: JSON.stringify({ name, scope: 'system' }),
    });
  },

  async deleteClusterSessionManager(managerID: string): Promise<void> {
    await this.makeRequest(`/session-managers/${encodeURIComponent(managerID)}`, { method: 'DELETE' });
  },

  async listSessionPools(): Promise<LogicalSessionPool[]> {
    const result = await this.makeRequest<{ session_pools: LogicalSessionPool[] }>('/session-pools');
    return result.session_pools ?? [];
  },

  async createSessionPool(
    input: Pick<LogicalSessionPool, 'name'> & Partial<Pick<LogicalSessionPool, 'labels'>>
  ): Promise<LogicalSessionPool> {
    return this.makeRequest('/session-pools', { method: 'POST', body: JSON.stringify(input) });
  },

  async deleteSessionPool(pool: string): Promise<void> {
    await this.makeRequest(`/session-pools/${encodeURIComponent(pool)}`, { method: 'DELETE' });
  },

  async createSessionPoolSupplier(
    managerID: string,
    input: Pick<SessionPoolSupplier, 'pool' | 'min_idle' | 'max_runners'>
  ): Promise<SessionPoolSupplier> {
    return this.makeRequest(
      `/session-pools/${encodeURIComponent(input.pool)}/suppliers/${encodeURIComponent(managerID)}`,
      { method: 'POST', body: JSON.stringify(input) }
    );
  },

  async deleteSessionPoolSupplier(managerID: string, pool: string): Promise<void> {
    await this.makeRequest(`/session-pools/${encodeURIComponent(pool)}/suppliers/${encodeURIComponent(managerID)}`, {
      method: 'DELETE',
    });
  },

  async listSessionPoolBindings(pool: string): Promise<SessionPoolBinding[]> {
    const result = await this.makeRequest<{ pool_bindings: SessionPoolBinding[] }>(
      `/session-pools/${encodeURIComponent(pool)}/bindings`
    );
    return result.pool_bindings ?? [];
  },

  async createSessionPoolBinding(
    pool: string,
    subjectType: 'user' | 'team' | 'all',
    subjectID: string,
    roles: 'use' | 'manage' | 'manage_and_use' | Array<'use' | 'manage'> = 'use',
    priority = 0,
    maxConcurrent = 0
  ): Promise<SessionPoolBinding> {
    const roleInput = Array.isArray(roles) ? { roles } : { role: roles };
    return this.makeRequest(`/session-pools/${encodeURIComponent(pool)}/bindings`, {
      method: 'POST',
      body: JSON.stringify({
        subject_type: subjectType,
        subject_id: subjectID,
        ...roleInput,
        priority,
        max_concurrent: maxConcurrent,
      }),
    });
  },

  async listManagedSessionPools(): Promise<LogicalSessionPool[]> {
    const result = await this.makeRequest<{ session_pools: LogicalSessionPool[] }>('/session-pools');
    return result.session_pools ?? [];
  },

  async createManagedSessionPool(input: { name: string; team_id?: string }): Promise<LogicalSessionPool> {
    return this.makeRequest('/session-pools', { method: 'POST', body: JSON.stringify(input) });
  },

  async patchManagedSessionPool(
    pool: string,
    input: { enabled?: boolean; labels?: Record<string, string> }
  ): Promise<LogicalSessionPool> {
    return this.makeRequest(`/session-pools/${encodeURIComponent(pool)}`, {
      method: 'PATCH',
      body: JSON.stringify(input),
    });
  },

  async deleteManagedSessionPool(pool: string): Promise<void> {
    await this.makeRequest(`/session-pools/${encodeURIComponent(pool)}`, { method: 'DELETE' });
  },

  async listManagedSessionPoolBindings(pool: string): Promise<SessionPoolBinding[]> {
    const result = await this.makeRequest<{ pool_bindings: SessionPoolBinding[] }>(
      `/session-pools/${encodeURIComponent(pool)}/bindings`
    );
    return result.pool_bindings ?? [];
  },

  async listManagedSessionPoolSuppliers(pool: string): Promise<SessionPoolSupplier[]> {
    const result = await this.makeRequest<{ pool_suppliers: SessionPoolSupplier[] }>(
      `/session-pools/${encodeURIComponent(pool)}/suppliers`
    );
    return result.pool_suppliers ?? [];
  },

  async createManagedSessionPoolSupplier(
    pool: string,
    managerID: string,
    input: { min_idle?: number; max_runners?: number; enabled?: boolean }
  ): Promise<SessionPoolSupplier> {
    return this.makeRequest(`/session-pools/${encodeURIComponent(pool)}/suppliers/${encodeURIComponent(managerID)}`, {
      method: 'POST',
      body: JSON.stringify(input),
    });
  },

  async patchManagedSessionPoolSupplier(
    pool: string,
    managerID: string,
    input: { enabled?: boolean; draining?: boolean; min_idle?: number; max_runners?: number }
  ): Promise<SessionPoolSupplier> {
    return this.makeRequest(`/session-pools/${encodeURIComponent(pool)}/suppliers/${encodeURIComponent(managerID)}`, {
      method: 'PATCH',
      body: JSON.stringify(input),
    });
  },

  async deleteManagedSessionPoolSupplier(pool: string, managerID: string): Promise<void> {
    await this.makeRequest(`/session-pools/${encodeURIComponent(pool)}/suppliers/${encodeURIComponent(managerID)}`, {
      method: 'DELETE',
    });
  },

  async createManagedSessionPoolBinding(
    pool: string,
    subjectType: 'user' | 'team' | 'all',
    subjectID: string,
    role: 'use' | 'manage' | 'manage_and_use',
    priority = 0,
    maxConcurrent = 0,
    enabled = true
  ): Promise<SessionPoolBinding> {
    return this.makeRequest(`/session-pools/${encodeURIComponent(pool)}/bindings`, {
      method: 'POST',
      body: JSON.stringify({
        subject_type: subjectType,
        subject_id: subjectID,
        role,
        priority,
        max_concurrent: maxConcurrent,
        enabled,
      }),
    });
  },

  async patchManagedSessionPoolBinding(
    pool: string,
    bindingID: string,
    input: {
      role?: 'use' | 'manage' | 'manage_and_use';
      roles?: Array<'use' | 'manage'>;
      enabled?: boolean;
      priority?: number;
      max_concurrent?: number;
    }
  ): Promise<SessionPoolBinding> {
    return this.makeRequest(`/session-pools/${encodeURIComponent(pool)}/bindings/${encodeURIComponent(bindingID)}`, {
      method: 'PATCH',
      body: JSON.stringify(input),
    });
  },

  async deleteManagedSessionPoolBinding(pool: string, bindingID: string): Promise<void> {
    await this.makeRequest(`/session-pools/${encodeURIComponent(pool)}/bindings/${encodeURIComponent(bindingID)}`, {
      method: 'DELETE',
    });
  },

  async deleteSessionPoolBinding(pool: string, bindingID: string): Promise<void> {
    await this.makeRequest(`/session-pools/${encodeURIComponent(pool)}/bindings/${encodeURIComponent(bindingID)}`, {
      method: 'DELETE',
    });
  },

  async getSessionPoolStatus(): Promise<SessionPoolStatusResponse> {
    return this.makeRequest<SessionPoolStatusResponse>('/session-pools/status');
  },

  async getSessionPoolManagerLogs(managerID: string, tail = 200): Promise<SessionPoolLogs> {
    return this.makeRequest<SessionPoolLogs>(
      `/session-managers/${encodeURIComponent(managerID)}/logs?tail=${Math.min(5000, Math.max(1, tail))}`
    );
  },

  async getSessionPoolRunnerLogs(runnerID: string, tail = 200): Promise<SessionPoolLogs> {
    return this.makeRequest<SessionPoolLogs>(
      `/session-runners/${encodeURIComponent(runnerID)}/logs?tail=${Math.min(5000, Math.max(1, tail))}`
    );
  },

  async listAdminSessionRunners(): Promise<AdminSessionRunner[]> {
    const result = await this.makeRequest<{ session_runners: AdminSessionRunner[] }>('/admin/session-runners');
    return result.session_runners ?? [];
  },

  async getAdminSessionRunnerLogs(runnerID: string, managerID: string, tail = 200): Promise<SessionPoolLogs> {
    const params = new URLSearchParams({ manager_id: managerID, tail: String(Math.min(5000, Math.max(1, tail))) });
    return this.makeRequest<SessionPoolLogs>(`/admin/session-runners/${encodeURIComponent(runnerID)}/logs?${params}`);
  },

  async deleteAdminSessionRunner(runnerID: string, managerID: string, force = false): Promise<void> {
    const params = new URLSearchParams({ manager_id: managerID });
    if (force) params.set('force', 'true');
    await this.makeRequest(`/admin/session-runners/${encodeURIComponent(runnerID)}?${params}`, { method: 'DELETE' });
  },
} satisfies ThisType<ProxyOperationContext>;

export type PoolsOperations = typeof poolsOperations;
