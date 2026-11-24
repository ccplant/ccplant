import { ACPEventsDecoder } from './acp-events-decoder';
import type { ProxyStreamContext } from './stream-context';
import { ACPSessionCallbacks, AgentStatus, EventSubscription } from './types';

export const acpEventsOperations = {
  /**
   * Subscribe to the ACP bridge's authoritative turn status.
   * The bridge emits the current value immediately and every running/stable
   * transition as a named `status_change` SSE event.
   */
  subscribeToACPStatus(
    sessionId: string,
    onStatus: (status: AgentStatus) => void,
    onError?: (error: Error) => void
  ): EventSubscription {
    const isUsingProxy = this.baseURL.includes('/api/v1');
    const url = isUsingProxy ? `/api/v1/${sessionId}/events` : `${this.baseURL}/${sessionId}/events`;
    let source: EventSource | null = null;
    let reconnectTimer: ReturnType<typeof setTimeout> | null = null;
    let stableTimer: ReturnType<typeof setTimeout> | null = null;
    let reconnectAttempts = 0;
    let closed = false;

    const scheduleReconnect = (minimumDelayMs = 0) => {
      if (closed) return;
      reconnectAttempts += 1;
      const delay = Math.max(minimumDelayMs, Math.min(1000 * 2 ** (reconnectAttempts - 1), 30000));
      reconnectTimer = setTimeout(connect, delay);
    };
    const connect = () => {
      if (closed) return;
      const current = new EventSource(url);
      source = current;
      current.onopen = () => {
        if (source !== current || closed) return;
        if (stableTimer) clearTimeout(stableTimer);
        stableTimer = setTimeout(() => {
          reconnectAttempts = 0;
          stableTimer = null;
        }, 30000);
      };
      current.addEventListener('status_change', (event) => {
        try {
          const data = JSON.parse((event as MessageEvent).data) as { status?: unknown };
          if (data.status === 'running' || data.status === 'stable' || data.status === 'error') {
            onStatus({ status: data.status });
          }
        } catch (error) {
          onError?.(error instanceof Error ? error : new Error('Failed to parse ACP status event'));
        }
      });
      current.addEventListener('agentapi_upstream_error', (event) => {
        if (source !== current || closed) return;
        let retryable = true;
        let retryAfterMs = 0;
        try {
          const payload = JSON.parse((event as MessageEvent).data) as { retryable?: boolean; retryAfterMs?: number };
          retryable = payload.retryable !== false;
          retryAfterMs = payload.retryAfterMs ?? 0;
        } catch (error) {
          onError?.(error instanceof Error ? error : new Error('Failed to parse upstream error event'));
        }
        current.close();
        source = null;
        if (stableTimer) clearTimeout(stableTimer);
        if (retryable) scheduleReconnect(retryAfterMs);
        else onError?.(new Error('ACP status stream is no longer available'));
      });
      current.onerror = () => {
        if (source !== current || closed) return;
        current.close();
        source = null;
        if (stableTimer) clearTimeout(stableTimer);
        onError?.(new Error('ACP status event stream disconnected'));
        scheduleReconnect();
      };
    };
    connect();

    return {
      close: () => {
        closed = true;
        source?.close();
        if (reconnectTimer) clearTimeout(reconnectTimer);
        if (stableTimer) clearTimeout(stableTimer);
      },
    };
  },

  /**
   * Subscribe to ACP session events via SSE at /{sessionId}/sse.
   *
   * The SSE stream emits raw JSON-RPC 2.0 messages from the ACP agent:
   *   - session/update notifications  → converted to SessionMessage via callbacks.onMessage
   *   - session/request_permission    → converted to PendingAction via callbacks.onPermission
   * Turn status is intentionally consumed from the bridge's /events endpoint,
   * not inferred from these raw protocol messages.
   *
   * Returns a subscription handle; call .close() to unsubscribe.
   */
  subscribeToACPSessionEvents(
    sessionId: string,
    acpSessionId: string,
    callbacks: ACPSessionCallbacks,
    initialLastEventId?: number,
    deferUntilHistory = false
  ): EventSubscription {
    const isUsingProxy = this.baseURL.includes('/api/v1');
    const sseUrl = isUsingProxy ? `/api/v1/${sessionId}/sse` : `${this.baseURL}/${sessionId}/sse`;

    if (this.debug) {
      console.log(`[AgentAPIProxy] ACP SSE subscribe: ${sseUrl}`);
    }

    const decoder = new ACPEventsDecoder(callbacks, () => this.debug);

    if (this.debug) {
      console.log(`[ACP] EventSource subscription created (url=${sseUrl}, acpSessionId=${acpSessionId})`);
    }

    return this.subscribeWithEventSource(
      sseUrl,
      {
        onOpen: () => {
          if (this.debug) {
            console.log(`[ACP] SSE connection opened (url=${sseUrl}, acpSessionId=${acpSessionId})`);
          }
          callbacks.onConnectionOpen?.();
        },
        onMessage: (event: MessageEvent) => decoder.consume(event),
        onTransientError: (event, state) => {
          if (this.debug) {
            console.warn(
              `[ACP] SSE transient error; browser is reconnecting (url=${sseUrl}, acpSessionId=${acpSessionId}, readyState=${state})`
            );
          }
          callbacks.onConnectionConnecting?.();
        },
        onReconnect: (attempt, delayMs) => {
          if (this.debug) {
            console.warn(
              `[ACP] SSE closed; recreating EventSource in ${delayMs}ms (url=${sseUrl}, acpSessionId=${acpSessionId}, attempt=${attempt})`
            );
          }
          callbacks.onConnectionConnecting?.(attempt, delayMs);
        },
        onClosed: (event, state) => {
          if (this.debug) {
            console.error(
              `[ACP] SSE connection permanently closed (url=${sseUrl}, acpSessionId=${acpSessionId}, readyState=${state})`
            );
          }
          callbacks.onConnectionClosed?.();
          callbacks.onError(new Error(`ACP SSE connection closed (readyState=${state})`));
        },
      },
      { reconnect: true, reconnectInterval: 1000 },
      initialLastEventId,
      deferUntilHistory ? { deferMessages: true, handshakeTimeoutMs: 1000 } : undefined
    );
  },
} satisfies ThisType<ProxyStreamContext>;
