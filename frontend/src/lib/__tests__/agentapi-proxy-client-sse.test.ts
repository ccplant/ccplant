import { afterEach, describe, expect, it, vi } from 'vitest';

import { AgentAPIProxyClient } from '../agentapi-proxy-client';

describe('AgentAPIProxyClient ACP SSE cursor', () => {
  afterEach(() => {
    vi.unstubAllGlobals();
  });

  it('resumes after the history cursor and ignores replayed event IDs', () => {
    class FakeEventSource {
      static readonly CONNECTING = 0;
      static readonly OPEN = 1;
      static readonly CLOSED = 2;
      static instances: FakeEventSource[] = [];
      readonly url: string;
      readyState = FakeEventSource.OPEN;
      onopen: (() => void) | null = null;
      onmessage: ((event: MessageEvent) => void) | null = null;
      onerror: ((event: Event) => void) | null = null;

      constructor(url: string | URL) {
        this.url = String(url);
        FakeEventSource.instances.push(this);
      }

      close() {}
      addEventListener() {}
    }
    vi.stubGlobal('EventSource', FakeEventSource);

    const onMessage = vi.fn();
    const client = new AgentAPIProxyClient({ baseURL: 'http://proxy.example.test' });
    const subscription = client.subscribeToACPSessionEvents('session-1', 'acp-1', {
      onMessage,
      onChunk: vi.fn(),
      onThoughtChunk: vi.fn(),
      onPermission: vi.fn(),
      onError: vi.fn(),
    }, 7);

    const source = FakeEventSource.instances[0];
    expect(source.url).toBe('http://proxy.example.test/session-1/sse?lastEventId=7');
    const payload = JSON.stringify({
      jsonrpc: '2.0',
      method: 'session/update',
      params: { update: { sessionUpdate: 'agent_message_chunk', content: { type: 'text', text: 'hello' } } },
    });
    source.onmessage?.(new MessageEvent('message', { data: payload, lastEventId: '7' }));
    source.onmessage?.(new MessageEvent('message', { data: payload, lastEventId: '8' }));

    expect(onMessage).toHaveBeenCalledTimes(1);
    subscription.close();
  });

});

describe('AgentAPIProxyClient ACP SSE reconnect policy', () => {
  afterEach(() => {
    vi.unstubAllGlobals();
  });

  it('stops reconnecting after a permanent upstream SSE error', async () => {
    vi.useFakeTimers();
    class FakeEventSource {
      static readonly CONNECTING = 0;
      static readonly OPEN = 1;
      static readonly CLOSED = 2;
      static instances: FakeEventSource[] = [];
      readyState = FakeEventSource.OPEN;
      onopen: (() => void) | null = null;
      onmessage: ((event: MessageEvent) => void) | null = null;
      onerror: ((event: Event) => void) | null = null;
      private listeners = new Map<string, (event: MessageEvent) => void>();
      constructor(readonly url: string) { FakeEventSource.instances.push(this); }
      close() {}
      addEventListener(type: string, listener: EventListenerOrEventListenerObject) {
        this.listeners.set(type, listener as (event: MessageEvent) => void);
      }
      emit(type: string, data: string) {
        this.listeners.get(type)?.(new MessageEvent(type, { data }));
      }
    }
    vi.stubGlobal('EventSource', FakeEventSource);

    const client = new AgentAPIProxyClient({ baseURL: '/api/v1' });
    const subscription = client.subscribeToACPSessionEvents('missing', 'acp-1', {
      onMessage: vi.fn(),
      onChunk: vi.fn(),
      onThoughtChunk: vi.fn(),
      onPermission: vi.fn(),
      onError: vi.fn(),
    });
    FakeEventSource.instances[0].onopen?.();
    FakeEventSource.instances[0].emit(
      'agentapi_upstream_error',
      JSON.stringify({ status: 404, retryable: false }),
    );
    await vi.runAllTimersAsync();

    expect(FakeEventSource.instances).toHaveLength(1);
    subscription.close();
    vi.useRealTimers();
  });

});

describe('AgentAPIProxyClient ACP status stream', () => {
  afterEach(() => {
    vi.unstubAllGlobals();
  });

  it('uses the bridge status stream instead of inferring status from ACP messages', () => {
    class FakeEventSource {
      static readonly CONNECTING = 0;
      static readonly OPEN = 1;
      static readonly CLOSED = 2;
      static instance: FakeEventSource;
      readonly url: string;
      readyState = FakeEventSource.OPEN;
      onopen: (() => void) | null = null;
      onmessage: ((event: MessageEvent) => void) | null = null;
      onerror: ((event: Event) => void) | null = null;

      private listeners = new Map<string, (event: MessageEvent) => void>();

      constructor(url: string | URL) {
        this.url = String(url);
        FakeEventSource.instance = this;
      }

      addEventListener(type: string, listener: EventListenerOrEventListenerObject) {
        this.listeners.set(type, listener as (event: MessageEvent) => void);
      }

      emit(type: string, data: string) { this.listeners.get(type)?.(new MessageEvent(type, { data })); }

      close() {}
    }
    vi.stubGlobal('EventSource', FakeEventSource);

    const onStatus = vi.fn();
    const client = new AgentAPIProxyClient({ baseURL: 'http://proxy.example.test' });
    const subscription = client.subscribeToACPStatus('session-1', onStatus);
    const source = FakeEventSource.instance;

    expect(source.url).toBe('http://proxy.example.test/session-1/events');
    source.emit('status_change', JSON.stringify({ status: 'running' }));
    source.emit('status_change', JSON.stringify({ status: 'stable' }));

    expect(onStatus.mock.calls).toEqual([
      [{ status: 'running' }],
      [{ status: 'stable' }],
    ]);
    subscription.close();
  });
});
