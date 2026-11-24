import type { ProxyOperationContext } from './operation-context';

export const storagesOperations = {
  async getStorages(params: {
    scope: 'user' | 'team';
    team_id?: string;
  }): Promise<{ storages: import('../../types/storage').Storage[]; total: number }> {
    const query = new URLSearchParams({ scope: params.scope });
    if (params.team_id) query.set('team_id', params.team_id);
    return this.makeRequest(`/storages?${query}`);
  },

  async createStorage(
    data: import('../../types/storage').CreateStorageRequest
  ): Promise<import('../../types/storage').Storage> {
    return this.makeRequest('/storages', { method: 'POST', body: JSON.stringify(data) });
  },

  async updateStorage(
    id: string,
    data: { name: string; description?: string }
  ): Promise<import('../../types/storage').Storage> {
    return this.makeRequest(`/storages/${encodeURIComponent(id)}`, { method: 'PATCH', body: JSON.stringify(data) });
  },

  async deleteStorage(id: string): Promise<void> {
    await this.makeRequest(`/storages/${encodeURIComponent(id)}`, { method: 'DELETE' });
  },
} satisfies ThisType<ProxyOperationContext>;

export type StoragesOperations = typeof storagesOperations;
