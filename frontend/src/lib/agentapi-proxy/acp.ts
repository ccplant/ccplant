import { parseACPJSONRPCMessages } from './acp-history';
import type { ProxyOperationContext } from './operation-context';
import {
  ACPConfigOption,
  ACPJSONRPCMessage,
  ACPMessageHistoryResult,
  ACPPromptContentBlock,
  ACPSessionInfo,
  ACPUserPromptInfo,
} from './types';

export const aCPOperations = {
  /**
   * Returns ACP session info if the session uses ACP transport, null otherwise.
   * Use this to detect whether a session is ACP-based.
   */
  async getACPSessionInfo(sessionId: string): Promise<ACPSessionInfo | null> {
    try {
      if (this.debug) {
        console.log(`[ACP] getACPSessionInfo called (proxySessionId=${sessionId})`);
      }
      const info = await this.makeRequest<ACPSessionInfo>(`/${sessionId}/session`);
      if (this.debug) {
        console.log(`[ACP] getACPSessionInfo completed (proxySessionId=${sessionId})`);
      }
      if (info?.sessionId) return info;
      return null;
    } catch {
      if (this.debug) {
        console.warn(`[ACP] getACPSessionInfo failed (proxySessionId=${sessionId})`);
      }
      return null;
    }
  },

  /**
   * Fetch message history from the ACP bridge's in-memory store.
   * When userPromptIndex is omitted, returns the current turn (latest user prompt onward).
   */
  async getACPMessageHistory(
    sessionId: string,
    _acpSessionId: string,
    userPromptIndex?: number
  ): Promise<ACPMessageHistoryResult> {
    const query = userPromptIndex !== undefined ? `?userPromptIndex=${userPromptIndex}` : '';
    const resp = await this.makeRequest<{
      messages: ACPJSONRPCMessage[];
      lastEventId?: number;
      userPromptCount?: number;
      userPromptIndex?: number;
      userPrompts?: ACPUserPromptInfo[];
    }>(`/${sessionId}/messages${query}`);

    const rawMessages = resp?.messages ?? [];
    const messages = parseACPJSONRPCMessages(rawMessages);
    if (this.debug) {
      console.log(
        `[ACP] getACPMessageHistory: ${messages.length} messages restored (userPromptIndex=${userPromptIndex ?? 'latest'})`
      );
    }

    return {
      messages,
      lastEventId: resp?.lastEventId,
      userPromptCount: resp?.userPromptCount ?? 0,
      userPromptIndex: resp?.userPromptIndex,
      userPrompts: resp?.userPrompts ?? [],
    };
  },

  /**
   * Send a session/prompt request to the ACP agent.
   * The result (stopReason) arrives asynchronously via the SSE stream.
   * @param promptId - JSON-RPC id; correlates the SSE result message.
   */
  async sendACPPrompt(
    sessionId: string,
    acpSessionId: string,
    prompt: ACPPromptContentBlock[],
    promptId: number
  ): Promise<void> {
    await this.makeRequest<unknown>(`/${sessionId}/rpc`, {
      method: 'POST',
      body: JSON.stringify({
        jsonrpc: '2.0',
        id: promptId,
        method: 'session/prompt',
        params: {
          sessionId: acpSessionId,
          prompt,
        },
      }),
    });
  },

  /**
   * Reply to a session/request_permission RPC that arrived via SSE.
   * @param rpcId  - The JSON-RPC id from the SSE message.
   * @param optionId - The selected option id to send back.
   */
  async replyToACPPermission(sessionId: string, rpcId: number, optionId: string): Promise<void> {
    await this.makeRequest<unknown>(`/${sessionId}/rpc`, {
      method: 'POST',
      body: JSON.stringify({
        jsonrpc: '2.0',
        id: rpcId,
        result: {
          outcome: { outcome: 'selected', optionId },
        },
      }),
    });
  },

  /**
   * Cancel the current ACP session turn.
   */
  async cancelACPSession(sessionId: string, acpSessionId: string): Promise<void> {
    await this.makeRequest<unknown>(`/${sessionId}/rpc`, {
      method: 'POST',
      body: JSON.stringify({
        jsonrpc: '2.0',
        method: 'session/cancel',
        params: { sessionId: acpSessionId },
      }),
    });
  },

  /**
   * Set an ACP session configuration option, such as the active model.
   * The ACP agent returns the complete configuration state after applying it.
   */
  async setACPSessionConfigOption(
    sessionId: string,
    acpSessionId: string,
    configId: string,
    value: string
  ): Promise<{ configOptions: ACPConfigOption[] }> {
    const response = await this.makeRequest<{
      configOptions?: ACPConfigOption[];
      result?: { configOptions?: ACPConfigOption[] };
    }>(`/${sessionId}/rpc`, {
      method: 'POST',
      body: JSON.stringify({
        jsonrpc: '2.0',
        id: Date.now(),
        method: 'session/set_config_option',
        params: {
          sessionId: acpSessionId,
          configId,
          value,
        },
      }),
    });

    return { configOptions: response.result?.configOptions ?? response.configOptions ?? [] };
  },
} satisfies ThisType<ProxyOperationContext>;

export type ACPOperations = typeof aCPOperations;
