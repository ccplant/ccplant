import {
  CreateSlackBotRequest,
  SlackBot,
  SlackBotListParams,
  SlackBotListResponse,
  UpdateSlackBotRequest,
} from '../../types/slackbot';
import type { ProxyOperationContext } from './operation-context';

export const slackbotsOperations = {
  /**
   * Create a new SlackBot
   */
  async createSlackBot(data: CreateSlackBotRequest): Promise<SlackBot> {
    if (this.debug) {
      console.log('[AgentAPIProxy] Creating slackbot');
    }

    const slackbot = await this.makeRequest<SlackBot>('/slackbots', {
      method: 'POST',
      body: JSON.stringify(data),
    });

    if (this.debug) {
      console.log(`[AgentAPIProxy] Created slackbot: ${slackbot.id}`);
    }

    return slackbot;
  },

  /**
   * Get list of SlackBots
   */
  async getSlackBots(params?: SlackBotListParams): Promise<SlackBotListResponse> {
    const searchParams = new URLSearchParams();

    if (params?.status) searchParams.set('status', params.status);
    if (params?.scope) searchParams.set('scope', params.scope);
    if (params?.team_id) searchParams.set('team_id', params.team_id);
    if (params?.page) searchParams.set('page', params.page.toString());
    if (params?.limit) searchParams.set('limit', params.limit.toString());

    const endpoint = `/slackbots${searchParams.toString() ? `?${searchParams.toString()}` : ''}`;
    const result = await this.makeRequest<SlackBot[] | SlackBotListResponse>(endpoint);

    // API returns array directly (not wrapped in object)
    if (Array.isArray(result)) {
      return { slackbots: result };
    }

    return {
      ...result,
      slackbots: result.slackbots || [],
    };
  },

  /**
   * Get a specific SlackBot by ID
   */
  async getSlackBot(slackbotId: string): Promise<SlackBot> {
    return this.makeRequest<SlackBot>(`/slackbots/${slackbotId}`);
  },

  /**
   * Update a SlackBot
   */
  async updateSlackBot(slackbotId: string, data: UpdateSlackBotRequest): Promise<SlackBot> {
    if (this.debug) {
      console.log(`[AgentAPIProxy] Updating slackbot ${slackbotId}`);
    }

    const slackbot = await this.makeRequest<SlackBot>(`/slackbots/${slackbotId}`, {
      method: 'PUT',
      body: JSON.stringify(data),
    });

    if (this.debug) {
      console.log(`[AgentAPIProxy] Updated slackbot: ${slackbot.id}`);
    }

    return slackbot;
  },

  /**
   * Delete a SlackBot
   */
  async deleteSlackBot(slackbotId: string): Promise<void> {
    if (this.debug) {
      console.log(`[AgentAPIProxy] Deleting slackbot: ${slackbotId}`);
    }

    await this.makeRequest<void>(`/slackbots/${slackbotId}`, {
      method: 'DELETE',
    });

    if (this.debug) {
      console.log(`[AgentAPIProxy] Deleted slackbot: ${slackbotId}`);
    }
  },
} satisfies ThisType<ProxyOperationContext>;

export type SlackbotsOperations = typeof slackbotsOperations;
