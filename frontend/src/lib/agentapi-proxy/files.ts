import type { ProxyOperationContext } from './operation-context';

export const filesOperations = {
  /**
   * List all user-managed files
   * GET /files
   */
  async listFiles(): Promise<import('../../types/user_file').FileListResponse> {
    if (this.debug) {
      console.log('[AgentAPIProxy] Listing user files');
    }
    return this.makeRequest<import('../../types/user_file').FileListResponse>('/files');
  },

  /**
   * Get a single user-managed file
   * GET /files/{fileId}
   */
  async getFile(fileId: string): Promise<import('../../types/user_file').UserFile> {
    return this.makeRequest<import('../../types/user_file').UserFile>(`/files/${encodeURIComponent(fileId)}`);
  },

  /**
   * Create a user-managed file
   * POST /files
   */
  async createFile(
    data: import('../../types/user_file').CreateFileRequest
  ): Promise<import('../../types/user_file').UserFile> {
    if (this.debug) {
      console.log('[AgentAPIProxy] Creating user file:', data.path);
    }
    return this.makeRequest<import('../../types/user_file').UserFile>('/files', {
      method: 'POST',
      body: JSON.stringify(data),
    });
  },

  /**
   * Update a user-managed file
   * PUT /files/{fileId}
   */
  async updateFile(
    fileId: string,
    data: import('../../types/user_file').UpdateFileRequest
  ): Promise<import('../../types/user_file').UserFile> {
    if (this.debug) {
      console.log(`[AgentAPIProxy] Updating user file: ${fileId}`);
    }
    return this.makeRequest<import('../../types/user_file').UserFile>(`/files/${encodeURIComponent(fileId)}`, {
      method: 'PUT',
      body: JSON.stringify(data),
    });
  },

  /**
   * Delete a user-managed file
   * DELETE /files/{fileId}
   */
  async deleteFile(fileId: string): Promise<{ success: boolean }> {
    if (this.debug) {
      console.log(`[AgentAPIProxy] Deleting user file: ${fileId}`);
    }
    return this.makeRequest<{ success: boolean }>(`/files/${encodeURIComponent(fileId)}`, {
      method: 'DELETE',
    });
  },
} satisfies ThisType<ProxyOperationContext>;

export type FilesOperations = typeof filesOperations;
