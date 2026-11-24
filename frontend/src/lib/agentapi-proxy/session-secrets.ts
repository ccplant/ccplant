import type { ProxyOperationContext } from './operation-context';
import { CreateSessionSecretResponse, SessionSecretListResponse } from './types';

export const sessionSecretsOperations = {
  async createSessionSecret(
    sessionId: string,
    value: string,
    expiresInSeconds = 600
  ): Promise<CreateSessionSecretResponse> {
    return this.makeRequest<CreateSessionSecretResponse>(`/sessions/${encodeURIComponent(sessionId)}/secrets`, {
      method: 'POST',
      body: JSON.stringify({ value, expires_in_seconds: expiresInSeconds }),
    });
  },

  async listSessionSecrets(sessionId: string): Promise<SessionSecretListResponse> {
    return this.makeRequest<SessionSecretListResponse>(`/sessions/${encodeURIComponent(sessionId)}/secrets`);
  },

  async reauthorizeSessionSecret(
    sessionId: string,
    secretId: string,
    expiresInSeconds = 600
  ): Promise<CreateSessionSecretResponse> {
    return this.makeRequest<CreateSessionSecretResponse>(
      `/sessions/${encodeURIComponent(sessionId)}/secrets/${encodeURIComponent(secretId)}/reauthorize`,
      {
        method: 'POST',
        body: JSON.stringify({ expires_in_seconds: expiresInSeconds }),
      }
    );
  },
} satisfies ThisType<ProxyOperationContext>;

export type SessionSecretsOperations = typeof sessionSecretsOperations;
