import { SessionEventsOptions } from '../../types/agentapi';
import { subscribeToEventStream } from './event-stream';
import { parseProxyResponse, proxyRequestError } from './http-response';
import { debugAllowed, inFlightGetRequests, requestUrlForLog } from './transport-support';
import { ACPInitializationSubscription, AgentAPIProxyClientConfig, HerdrRPCResponse } from './types';

export class AgentAPIProxyTransport {
  protected baseURL: string;

  protected apiKey?: string;

  protected timeout: number;

  protected maxSessions: number;

  protected sessionTimeout: number;

  protected debug: boolean;

  constructor(config: AgentAPIProxyClientConfig) {
    this.baseURL = config.baseURL.replace(/\/$/, ''); // Remove trailing slash
    this.apiKey = config.apiKey;
    this.timeout = config.timeout || 10000;
    this.maxSessions = config.maxSessions || 10;
    this.sessionTimeout = config.sessionTimeout || 300000; // 5 minutes
    this.debug = debugAllowed(config.debug === true);

    if (this.debug) {
      console.log('[AgentAPIProxy] Initialized with config:', {
        baseURL: requestUrlForLog(this.baseURL),
        maxSessions: this.maxSessions,
        sessionTimeout: this.sessionTimeout,
      });
    }
  }

  protected subscribeWithEventSource(
    url: string,
    handlers: {
      onOpen?: () => void;
      onMessage: (event: MessageEvent) => void;
      onTransientError?: (event: Event, state: number) => void;
      onReconnect?: (attempt: number, delayMs: number) => void;
      onClosed?: (event: Event, state: number) => void;
    },
    options?: SessionEventsOptions,
    initialLastEventId?: number,
    initialization?: {
      deferMessages: boolean;
      handshakeTimeoutMs: number;
    }
  ): ACPInitializationSubscription {
    return subscribeToEventStream(url, handlers, { options, initialLastEventId, initialization });
  }

  protected makeRequest<T>(endpoint: string, options: RequestInit = {}, attempt = 1): Promise<T> {
    const method = (options.method || 'GET').toUpperCase();
    if (method !== 'GET' || attempt !== 1) {
      return this.makeRequestUncached<T>(endpoint, options, attempt);
    }

    const key = `${this.baseURL}|${this.apiKey || 'cookie'}|${endpoint}`;
    const existing = inFlightGetRequests.get(key);
    if (existing) return existing as Promise<T>;

    const request = this.makeRequestUncached<T>(endpoint, options, attempt);
    inFlightGetRequests.set(key, request);
    void request
      .finally(() => {
        if (inFlightGetRequests.get(key) === request) {
          inFlightGetRequests.delete(key);
        }
      })
      .catch(() => undefined);
    return request;
  }

  protected async makeRequestUncached<T>(endpoint: string, options: RequestInit = {}, attempt = 1): Promise<T> {
    const url = `${this.baseURL}${endpoint}`;
    const send = (authentication: Record<string, string>, label: string) => {
      if (this.debug) {
        console.log(`[AgentAPIProxy] ${options.method || 'GET'} ${requestUrlForLog(url)} (${label})`, { attempt });
      }
      return fetch(url, {
        ...options,
        headers: {
          'Content-Type': 'application/json',
          Accept: 'application/json, application/problem+json',
          ...authentication,
          ...options.headers,
        },
      });
    };
    try {
      if (this.apiKey) {
        const response = await send({ Authorization: `Bearer ${this.apiKey}` }, 'authenticated');
        if (response.status === 401 || response.status === 403) {
          const fallback = await send({ 'X-API-Key': this.apiKey }, 'authentication fallback');
          return await parseProxyResponse<T>(fallback, true);
        }
        return await parseProxyResponse<T>(response, false);
      }
      return await parseProxyResponse<T>(await send({}, 'cookie/public auth'), true);
    } catch (error) {
      throw proxyRequestError(error);
    }
  }

  protected async herdrRPC<T>(sessionId: string, method: string, params: Record<string, unknown>): Promise<T> {
    const response = await this.makeRequest<HerdrRPCResponse<T>>(`/${sessionId}/herdr/rpc`, {
      method: 'POST',
      body: JSON.stringify({ jsonrpc: '2.0', id: Date.now(), method, params }),
    });
    if (response.error) {
      throw new Error(`herdr-core RPC ${response.error.code}: ${response.error.message}`);
    }
    if (response.result === undefined) {
      throw new Error('herdr-core returned no result');
    }
    return response.result;
  }
}
