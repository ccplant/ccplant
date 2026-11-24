import {
  Agent,
  AgentListParams,
  AgentListResponse,
  CreateSessionRequest,
  ResourceScope,
  Session,
  SessionContextTemplate,
  SessionListParams,
  SessionListResponse,
  SessionMessageListParams,
  SessionMessageListResponse,
  ToolStatusResponseBody,
} from '../../types/agentapi';
import type { ProxyOperationContext } from './operation-context';
import {
  AgentAPIProxyError,
  AgentStatus,
  GitHubUser,
  TransferableResourceType,
  TransferResourceResult,
  WaitSessionMessagesResponse,
} from './types';

export const sessionsOperations = {
  /**
   * Start a new session
   */
  async start(sessionData?: Partial<CreateSessionRequest> | Record<string, unknown>): Promise<Session> {
    // Handle backward compatibility and new format
    let data: Partial<CreateSessionRequest>;

    if (
      sessionData &&
      (sessionData.environment ||
        sessionData.tags ||
        sessionData.metadata ||
        sessionData.params ||
        sessionData.scope ||
        sessionData.team_id ||
        sessionData.session_profile_id ||
        sessionData.workspace_id ||
        sessionData.context_template_id)
    ) {
      // New format: sessionData contains environment, metadata, tags, params, scope, and/or team_id
      data = {
        environment: sessionData.environment as Record<string, string> | undefined,
        metadata: (sessionData.metadata as Record<string, unknown> | undefined) || { source: 'agentapi-ui' },
        tags: sessionData.tags as Record<string, string> | undefined,
        params: sessionData.params as { message?: string; github_token?: string; [key: string]: unknown } | undefined,
        scope: sessionData.scope as CreateSessionRequest['scope'],
        team_id: sessionData.team_id as string | undefined,
        session_profile_id: sessionData.session_profile_id as string | undefined,
        workspace_id: sessionData.workspace_id as string | undefined,
        context_template_id: sessionData.context_template_id as string | undefined,
      };
    } else {
      // Backward compatibility: sessionData is just metadata
      data = {
        metadata: (sessionData as Record<string, unknown>) || { source: 'agentapi-ui' },
      };
    }

    const session = await this.makeRequest<Session>('/start', {
      method: 'POST',
      body: JSON.stringify(data),
    });

    if (this.debug) {
      console.log(`[AgentAPIProxy] Started session: ${session.session_id}`);
    }

    return session;
  },

  /**
   * Search for sessions
   */
  async search(params?: SessionListParams): Promise<SessionListResponse> {
    const searchParams = new URLSearchParams();

    if (params?.page) searchParams.set('page', params.page.toString());
    if (params?.limit) searchParams.set('limit', params.limit.toString());
    if (params?.status) searchParams.set('status', params.status);
    if (params?.user_id) searchParams.set('user_id', params.user_id);
    if (params?.scope) searchParams.set('scope', params.scope);
    if (params?.team_id) searchParams.set('team_id', params.team_id);

    const endpoint = `/search${searchParams.toString() ? `?${searchParams.toString()}` : ''}`;
    const result = await this.makeRequest<SessionListResponse>(endpoint);

    // Keep the UI scope isolated even when an older proxy or an external
    // session manager returns a broader result set than requested.
    const sessions = (result.sessions || []).filter((session) => {
      if (params?.scope && (session.scope || 'user') !== params.scope) return false;
      if (params?.team_id && session.team_id !== params.team_id) return false;
      return true;
    });

    return {
      ...result,
      sessions,
    };
  },

  /**
   * Delete a session
   */
  async delete(sessionId: string): Promise<void> {
    if (this.debug) {
      console.log(`[AgentAPIProxy] Attempting to delete session: ${sessionId}`);
    }

    // Use the correct agentapi-proxy endpoint: DELETE /sessions/{sessionId}
    await this.makeRequest<void>(`/sessions/${sessionId}`, {
      method: 'DELETE',
    });

    if (this.debug) {
      console.log(`[AgentAPIProxy] Successfully deleted session: ${sessionId}`);
    }
  },

  async resumeSession(sessionId: string): Promise<{ session_id: string; status: string }> {
    return this.makeRequest<{ session_id: string; status: string }>(`/sessions/${sessionId}/resume`, {
      method: 'POST',
    });
  },

  async restartSession(sessionId: string, reloadSettings = true): Promise<{ session_id: string; request_id: string }> {
    return this.makeRequest(`/sessions/${encodeURIComponent(sessionId)}/restart`, {
      method: 'POST',
      headers: { 'Idempotency-Key': crypto.randomUUID() },
      body: JSON.stringify({ reload_settings: reloadSettings, busy_policy: 'wait' }),
    });
  },

  async pauseSession(sessionId: string): Promise<{ session_id: string; request_id: string }> {
    return this.makeRequest(`/sessions/${encodeURIComponent(sessionId)}/pause`, {
      method: 'POST',
      headers: { 'Idempotency-Key': crypto.randomUUID() },
      body: JSON.stringify({ busy_policy: 'wait' }),
    });
  },

  async restartStatus(sessionId: string): Promise<{ phase: string; revision: number; error?: string }> {
    return this.makeRequest(`/sessions/${encodeURIComponent(sessionId)}/restart`);
  },

  async suspendSession(sessionId: string): Promise<{ session_id: string; status: string }> {
    return this.makeRequest<{ session_id: string; status: string }>(`/sessions/${sessionId}/suspend`, {
      method: 'POST',
    });
  },

  async templateizeSession(sessionId: string, name: string, description = ''): Promise<SessionContextTemplate> {
    return this.makeRequest<SessionContextTemplate>(`/sessions/${encodeURIComponent(sessionId)}/templateize`, {
      method: 'POST',
      headers: { 'Idempotency-Key': crypto.randomUUID() },
      body: JSON.stringify({ name, description, wait_for_idle: true }),
    });
  },

  async getSessionMessages(sessionId: string, params?: SessionMessageListParams): Promise<SessionMessageListResponse> {
    const searchParams = new URLSearchParams();

    // New API parameters (cursor-based pagination)
    if (params?.before !== undefined) searchParams.set('before', params.before.toString());
    if (params?.after !== undefined) searchParams.set('after', params.after.toString());

    // Limit and direction parameters
    if (params?.limit) searchParams.set('limit', params.limit.toString());
    if (params?.direction) searchParams.set('direction', params.direction);

    // Context-based pagination
    if (params?.around !== undefined) searchParams.set('around', params.around.toString());
    if (params?.context !== undefined) searchParams.set('context', params.context.toString());

    // Deprecated parameters (kept for backwards compatibility)
    if (params?.page) searchParams.set('page', params.page.toString());
    if (params?.from) searchParams.set('from', params.from);
    if (params?.to) searchParams.set('to', params.to);

    const endpoint = `/${sessionId}/messages${searchParams.toString() ? `?${searchParams.toString()}` : ''}`;

    if (this.debug) {
      console.log(
        `[AgentAPIProxy] Fetching messages for session ${sessionId} with params:`,
        Object.fromEntries(searchParams)
      );
    }

    const result = await this.makeRequest<SessionMessageListResponse>(endpoint);

    return {
      ...result,
      messages: result.messages || [],
    };
  },

  async getSessionStatus(sessionId: string): Promise<AgentStatus> {
    try {
      return await this.makeRequest<AgentStatus>(`/${sessionId}/status`);
    } catch (err) {
      if (err instanceof AgentAPIProxyError && err.status === 500) {
        // Provisioner has permanently failed – return the error status so polling
        // continues for other data without throwing and surface the message.
        return { status: 'error', message: err.message };
      }
      throw err;
    }
  },

  /**
   * Get active tool execution status for a session
   * Returns null if the endpoint is not available (404)
   */
  async getToolStatus(sessionId: string): Promise<ToolStatusResponseBody | null> {
    try {
      return await this.makeRequest<ToolStatusResponseBody>(`/${sessionId}/tool_status`);
    } catch (error) {
      // If the endpoint doesn't exist (404), return null
      if (error instanceof AgentAPIProxyError && error.status === 404) {
        if (this.debug) {
          console.log(`[AgentAPIProxy] Tool status endpoint not available for session ${sessionId}`);
        }
        return null;
      }
      throw error;
    }
  },

  /**
   * Long-poll for message updates in a specific session (GET /sessions/:sessionId/messages/wait).
   *
   * Blocks on the server until a message_update event is received from the agentapi backend
   * for the given session, or until the timeout expires.
   *
   * Returns { updated: true, session_id, timestamp } on update, or { updated: false } on timeout.
   * Callers should immediately re-issue the request to maintain continuous notification.
   *
   * @param sessionId - The session to watch for message updates.
   * @param options.timeout - Max wait seconds (default: 30, max: 60).
   * @param options.signal - AbortSignal to cancel the request (e.g. on component unmount).
   * @param options.since - Unix timestamp in milliseconds of the last known message update.
   *   If the session received a message_update after this time the server returns immediately
   *   with updated=true, allowing the client to catch up after a period of inactivity.
   */
  async waitSessionMessages(
    sessionId: string,
    options: { timeout?: number; signal?: AbortSignal; since?: number } = {}
  ): Promise<WaitSessionMessagesResponse> {
    const { timeout = 30, signal, since } = options;
    let endpoint = `/sessions/${sessionId}/messages/wait?timeout=${timeout}`;
    if (since !== undefined) {
      endpoint += `&since=${since}`;
    }

    if (this.debug) {
      console.log(
        `[AgentAPIProxy] Long-polling for message updates in session ${sessionId} (timeout=${timeout}s, since=${since})`
      );
    }

    return this.makeRequest<WaitSessionMessagesResponse>(endpoint, { signal });
  },

  /**
   * Get list of agents
   */
  async getAgents(params?: AgentListParams): Promise<AgentListResponse> {
    const searchParams = new URLSearchParams();

    if (params?.page) searchParams.set('page', params.page.toString());
    if (params?.limit) searchParams.set('limit', params.limit.toString());
    if (params?.status) searchParams.set('status', params.status);
    if (params?.name) searchParams.set('name', params.name);

    const endpoint = `/agents${searchParams.toString() ? `?${searchParams.toString()}` : ''}`;
    const result = await this.makeRequest<AgentListResponse>(endpoint);

    return {
      ...result,
      agents: result.agents || [],
    };
  },

  /**
   * Get a specific agent by ID
   */
  async getAgent(agentId: string): Promise<Agent> {
    return this.makeRequest<Agent>(`/agents/${agentId}`);
  },

  /**
   * Health check for the proxy
   */
  async healthCheck(): Promise<boolean> {
    try {
      await this.makeRequest('/health');
      return true;
    } catch {
      return false;
    }
  },

  /**
   * Get authentication info for the current session
   */
  async getAuthInfo(): Promise<{ type: 'github' | 'none'; authenticated: boolean; user?: GitHubUser }> {
    try {
      return await this.makeRequest<{ type: 'github' | 'none'; authenticated: boolean; user?: GitHubUser }>(
        '/auth/info'
      );
    } catch (error) {
      // If the endpoint doesn't exist (404) or returns an error, assume no auth (older agentapi-proxy)
      if (this.debug) {
        console.log('[AgentAPIProxy] Auth info not available (likely older proxy version):', error);
      }
      return { type: 'none', authenticated: false };
    }
  },

  /** Validate or perform an ownership transfer between user/team scopes. */
  async transferResource(data: {
    resource_type: TransferableResourceType;
    resource_id: string;
    target_scope: ResourceScope;
    target_user_id?: string;
    target_team_id?: string;
    dry_run?: boolean;
  }): Promise<TransferResourceResult> {
    return this.makeRequest<TransferResourceResult>('/resources/transfer', {
      method: 'POST',
      body: JSON.stringify(data),
    });
  },
} satisfies ThisType<ProxyOperationContext>;

export type SessionsOperations = typeof sessionsOperations;
