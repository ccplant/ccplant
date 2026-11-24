import { afterEach, describe, expect, it, vi } from 'vitest';

import { AgentAPIProxyClient } from '../agentapi-proxy-client';

describe('AgentAPIProxyClient ACP initialization subscription', () => {
  afterEach(() => {
    vi.unstubAllGlobals();
  });

  it('uses the successful handshake as the real subscription and buffers events until history loads', async () => {
    const close = vi.fn();
    class FakeEventSource {
      static readonly CONNECTING = 0;
      static readonly OPEN = 1;
      static readonly CLOSED = 2;
      static instance: FakeEventSource;
      readyState = FakeEventSource.OPEN;
      onopen: (() => void) | null = null;
      onmessage: ((event: MessageEvent) => void) | null = null;
      onerror: ((event: Event) => void) | null = null;
      constructor(readonly url: string) { FakeEventSource.instance = this; }
      close = close;
      addEventListener() {}
    }
    vi.stubGlobal('EventSource', FakeEventSource);

    const onMessage = vi.fn();
    const client = new AgentAPIProxyClient({ baseURL: '/api/v1' });
    const subscription = client.subscribeToACPSessionEventsForInitialization('session-1', {
      onMessage,
      onChunk: vi.fn(),
      onThoughtChunk: vi.fn(),
      onPermission: vi.fn(),
      onError: vi.fn(),
    });
    expect(FakeEventSource.instance.url).toBe('/api/v1/session-1/sse');
    FakeEventSource.instance.onopen?.();

    await expect(subscription.opened).resolves.toBe(true);
    expect(close).not.toHaveBeenCalled();

    const payload = JSON.stringify({
      jsonrpc: '2.0',
      method: 'session/update',
      params: { update: { sessionUpdate: 'agent_message_chunk', content: { type: 'text', text: 'live' } } },
    });
    FakeEventSource.instance.onmessage?.(new MessageEvent('message', { data: payload, lastEventId: '7' }));
    FakeEventSource.instance.onmessage?.(new MessageEvent('message', { data: payload, lastEventId: '8' }));
    expect(onMessage).not.toHaveBeenCalled();

    subscription.resumeAfterHistory(7);
    expect(onMessage).toHaveBeenCalledOnce();
    subscription.close();
  });

});

describe('AgentAPIProxyClient ACP initialization failure', () => {
  afterEach(() => {
    vi.unstubAllGlobals();
  });

  it('reports an unsuccessful initial handshake when the SSE endpoint errors', async () => {
    class FakeEventSource {
      static readonly CONNECTING = 0;
      static readonly OPEN = 1;
      static readonly CLOSED = 2;
      static instance: FakeEventSource;
      readyState = FakeEventSource.CONNECTING;
      onopen: (() => void) | null = null;
      onmessage: ((event: MessageEvent) => void) | null = null;
      onerror: ((event: Event) => void) | null = null;
      constructor(readonly url: string) { FakeEventSource.instance = this; }
      close() {}
      addEventListener() {}
    }
    vi.stubGlobal('EventSource', FakeEventSource);

    const client = new AgentAPIProxyClient({ baseURL: '/api/v1' });
    const subscription = client.subscribeToACPSessionEventsForInitialization('session-1', {
      onMessage: vi.fn(),
      onChunk: vi.fn(),
      onThoughtChunk: vi.fn(),
      onPermission: vi.fn(),
      onError: vi.fn(),
    });
    FakeEventSource.instance.onerror?.(new Event('error'));

    await expect(subscription.opened).resolves.toBe(false);
    subscription.close();
  });
});
