import { SessionEventData, SessionEventsOptions, SessionMessage } from '../../types/agentapi';
import type { ProxyStreamContext } from './stream-context';
import { AgentStatus, EventSubscription, ProxySessionStatusEvent } from './types';

export const legacyEventsOperations = {
  subscribeToSessionEvents(
    sessionId: string,
    onMessage: (message: SessionMessage) => void,
    onStatus?: (status: AgentStatus) => void,
    onError?: (error: Error) => void,
    options?: SessionEventsOptions
  ): EventSubscription {
    // For SSE, we need to construct the full URL - check if we're using proxy
    const isUsingProxy = this.baseURL.includes('/api/v1');
    const eventSourceUrl = isUsingProxy ? `/api/v1/${sessionId}/events` : `${this.baseURL}/${sessionId}/events`;

    if (this.debug) {
      console.log(`[AgentAPIProxy] Creating EventSource for session ${sessionId}:`, eventSourceUrl);
    }

    return this.subscribeWithEventSource(
      eventSourceUrl,
      {
        onMessage: (event) => {
          try {
            const eventData: SessionEventData = JSON.parse(event.data);

            if (this.debug) {
              console.log(`[AgentAPIProxy] Session event received:`, eventData);
            }

            switch (eventData.type) {
              case 'message':
                onMessage(eventData.data as SessionMessage);
                break;
              case 'status':
                if (onStatus) {
                  onStatus(eventData.data as AgentStatus);
                }
                break;
              case 'error':
                if (onError) {
                  const errorData = eventData.data as { error: string };
                  onError(new Error(errorData.error));
                }
                break;
              default:
                if (this.debug) {
                  console.warn(`[AgentAPIProxy] Unknown event type: ${eventData.type}`);
                }
            }
          } catch (err) {
            console.error('[AgentAPIProxy] Failed to parse session event:', err);
            if (onError) {
              onError(err instanceof Error ? err : new Error('Failed to parse event data'));
            }
          }
        },
        onTransientError: (event, state) => {
          if (this.debug) {
            console.warn(`[AgentAPIProxy] Session EventSource transient error (readyState=${state})`, event);
          }
        },
        onReconnect: (attempt, delayMs) => {
          if (this.debug) {
            console.log(
              `[AgentAPIProxy] Session EventSource closed; reconnecting in ${delayMs}ms (attempt ${attempt})`
            );
          }
        },
        onClosed: (event, state) => {
          console.error(`[AgentAPIProxy] Session EventSource closed permanently (readyState=${state})`, event);
          onError?.(new Error('Session EventSource connection closed'));
        },
      },
      options
    );
  },

  /**
   * Subscribe to proxy-wide session status changes via SSE (GET /sessions/status/stream).
   * Each event carries the session_id, new status, and timestamp for any session
   * accessible by the current user.
   *
   * @returns The EventSource — call `.close()` to unsubscribe.
   */
  subscribeToSessionsStatusEvents(
    onEvent: (event: ProxySessionStatusEvent) => void,
    onError?: (error: Error) => void
  ): EventSource {
    const isUsingProxy = this.baseURL.includes('/api/v1');
    const eventSourceUrl = isUsingProxy ? `/api/v1/sessions/status/stream` : `${this.baseURL}/sessions/status/stream`;

    if (this.debug) {
      console.log(`[AgentAPIProxy] Creating EventSource for proxy-wide session status:`, eventSourceUrl);
    }

    const eventSource = new EventSource(eventSourceUrl);

    eventSource.addEventListener('agentapi_upstream_error', (event) => {
      eventSource.close();
      let status = 0;
      try {
        status = (JSON.parse((event as MessageEvent).data) as { status?: number }).status ?? 0;
      } catch (error) {
        console.warn('Failed to parse proxy status stream error:', error);
      }
      onError?.(new Error(`Proxy status stream upstream failed${status ? ` (${status})` : ''}`));
    });

    eventSource.onmessage = (event) => {
      // Skip heartbeat comments (EventSource ignores comment lines automatically,
      // but guard against empty data just in case)
      if (!event.data) return;
      try {
        const data: ProxySessionStatusEvent = JSON.parse(event.data);
        if (this.debug) {
          console.log(`[AgentAPIProxy] Proxy session status event:`, data);
        }
        onEvent(data);
      } catch (err) {
        console.error('[AgentAPIProxy] Failed to parse proxy session status event:', err);
      }
    };

    eventSource.onerror = () => {
      console.error('[AgentAPIProxy] Proxy status EventSource error');
      if (onError) {
        onError(new Error('Proxy session status EventSource connection error'));
      }
    };

    return eventSource;
  },
} satisfies ThisType<ProxyStreamContext>;
