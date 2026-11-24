import {
  CreateWebhookRequest,
  RegenerateSecretResponse,
  TriggerWebhookRequest,
  TriggerWebhookResponse,
  UpdateWebhookRequest,
  Webhook,
  WebhookListParams,
  WebhookListResponse,
} from '../../types/webhook';
import type { ProxyOperationContext } from './operation-context';

export const webhooksOperations = {
  /**
   * Create a new webhook
   */
  async createWebhook(data: CreateWebhookRequest): Promise<Webhook> {
    if (this.debug) {
      console.log('[AgentAPIProxy] Creating webhook');
    }

    const webhook = await this.makeRequest<Webhook>('/webhooks', {
      method: 'POST',
      body: JSON.stringify(data),
    });

    if (this.debug) {
      console.log(`[AgentAPIProxy] Created webhook: ${webhook.id}`);
    }

    return webhook;
  },

  /**
   * Get list of webhooks
   */
  async getWebhooks(params?: WebhookListParams): Promise<WebhookListResponse> {
    const searchParams = new URLSearchParams();

    if (params?.type) searchParams.set('type', params.type);
    if (params?.status) searchParams.set('status', params.status);
    if (params?.page) searchParams.set('page', params.page.toString());
    if (params?.limit) searchParams.set('limit', params.limit.toString());
    if (params?.scope) searchParams.set('scope', params.scope);
    if (params?.team_id) searchParams.set('team_id', params.team_id);

    const endpoint = `/webhooks${searchParams.toString() ? `?${searchParams.toString()}` : ''}`;
    const result = await this.makeRequest<WebhookListResponse>(endpoint);

    return {
      ...result,
      webhooks: result.webhooks || [],
    };
  },

  /**
   * Get a specific webhook by ID
   */
  async getWebhook(webhookId: string): Promise<Webhook> {
    return this.makeRequest<Webhook>(`/webhooks/${webhookId}`);
  },

  /**
   * Update a webhook
   */
  async updateWebhook(webhookId: string, data: UpdateWebhookRequest): Promise<Webhook> {
    if (this.debug) {
      console.log(`[AgentAPIProxy] Updating webhook ${webhookId}`);
    }

    const webhook = await this.makeRequest<Webhook>(`/webhooks/${webhookId}`, {
      method: 'PUT',
      body: JSON.stringify(data),
    });

    if (this.debug) {
      console.log(`[AgentAPIProxy] Updated webhook: ${webhook.id}`);
    }

    return webhook;
  },

  /**
   * Delete a webhook
   */
  async deleteWebhook(webhookId: string): Promise<void> {
    if (this.debug) {
      console.log(`[AgentAPIProxy] Deleting webhook: ${webhookId}`);
    }

    await this.makeRequest<void>(`/webhooks/${webhookId}`, {
      method: 'DELETE',
    });

    if (this.debug) {
      console.log(`[AgentAPIProxy] Deleted webhook: ${webhookId}`);
    }
  },

  /**
   * Regenerate webhook secret
   */
  async regenerateWebhookSecret(webhookId: string): Promise<RegenerateSecretResponse> {
    if (this.debug) {
      console.log(`[AgentAPIProxy] Regenerating secret for webhook: ${webhookId}`);
    }

    const result = await this.makeRequest<RegenerateSecretResponse>(`/webhooks/${webhookId}/regenerate-secret`, {
      method: 'POST',
    });

    if (this.debug) {
      console.log(`[AgentAPIProxy] Regenerated secret for webhook ${webhookId}`);
    }

    return result;
  },

  /**
   * Trigger a webhook with a test payload
   */
  async triggerWebhook(webhookId: string, data: TriggerWebhookRequest): Promise<TriggerWebhookResponse> {
    if (this.debug) {
      console.log(`[AgentAPIProxy] Triggering webhook ${webhookId}`);
    }

    const result = await this.makeRequest<TriggerWebhookResponse>(`/webhooks/${webhookId}/trigger`, {
      method: 'POST',
      body: JSON.stringify(data),
    });

    if (this.debug) {
      console.log(`[AgentAPIProxy] Webhook ${webhookId} trigger completed`);
    }

    return result;
  },
} satisfies ThisType<ProxyOperationContext>;

export type WebhooksOperations = typeof webhooksOperations;
