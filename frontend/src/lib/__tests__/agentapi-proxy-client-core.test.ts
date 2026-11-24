import { afterEach, describe, expect, it, vi } from 'vitest';

import { AgentAPIProxyClient } from '../agentapi-proxy-client';

describe('AgentAPIProxyClient herdr-core transport', () => {
  afterEach(() => vi.restoreAllMocks());

  it('lists terminals through the session-scoped JSON-RPC bridge', async () => {
    const fetchMock = vi.spyOn(globalThis, 'fetch').mockResolvedValueOnce(
      new Response(JSON.stringify({
        jsonrpc: '2.0', id: 1,
        result: { terminals: [{ address: 't7', name: 'review', state: 'live', status: { state: 'working' } }] },
      }), { status: 200, headers: { 'Content-Type': 'application/json' } }),
    );
    const client = new AgentAPIProxyClient({ baseURL: 'http://proxy.example.test' });

    await expect(client.listHerdrTerminals('session-a')).resolves.toEqual([
      { address: 't7', name: 'review', state: 'live', status: { state: 'working' } },
    ]);
    expect(fetchMock).toHaveBeenCalledWith(
      'http://proxy.example.test/session-a/herdr/rpc',
      expect.objectContaining({ method: 'POST' }),
    );
    const request = fetchMock.mock.calls[0]?.[1] as RequestInit;
    expect(JSON.parse(request.body as string)).toMatchObject({ method: 'terminal.list', params: { all: false } });
  });

  it('sends prompts with terminal.send', async () => {
    const fetchMock = vi.spyOn(globalThis, 'fetch').mockResolvedValueOnce(
      new Response(JSON.stringify({ jsonrpc: '2.0', id: 1, result: { turn: 3 } }), {
        status: 200, headers: { 'Content-Type': 'application/json' },
      }),
    );
    const client = new AgentAPIProxyClient({ baseURL: 'http://proxy.example.test' });

    await client.sendHerdrTerminal('session-a', 't7', 'continue');
    const request = fetchMock.mock.calls[0]?.[1] as RequestInit;
    expect(JSON.parse(request.body as string)).toMatchObject({
      method: 'terminal.send', params: { terminal: 't7', text: 'continue', wait: false },
    });
  });
});

describe('AgentAPIProxyClient team settings', () => {
  afterEach(() => {
    vi.restoreAllMocks();
  });

  it.each([
    ['get', (client: AgentAPIProxyClient) => client.getSettings('acme/platform')],
    ['save', (client: AgentAPIProxyClient) => client.saveSettings('acme/platform', {})],
    ['delete', (client: AgentAPIProxyClient) => client.deleteSettings('acme/platform')],
  ])('preserves the organization/team identifier when settings are %s', async (_operation, request) => {
    const fetchMock = vi.spyOn(globalThis, 'fetch').mockResolvedValueOnce(
      new Response(JSON.stringify({}), {
        status: 200,
        headers: { 'Content-Type': 'application/json' },
      }),
    );
    const client = new AgentAPIProxyClient({ baseURL: 'http://proxy.example.test' });

    await request(client);

    expect(fetchMock).toHaveBeenCalledWith(
      'http://proxy.example.test/settings/acme%2Fplatform',
      expect.any(Object),
    );
  });

  it('synchronizes members through the encoded team endpoint', async () => {
    const fetchMock = vi.spyOn(globalThis, 'fetch').mockResolvedValueOnce(
      new Response(JSON.stringify({ operation_id: 'op-1', member_count: 2 }), {
        status: 200,
        headers: { 'Content-Type': 'application/json' },
      }),
    );
    const client = new AgentAPIProxyClient({ baseURL: 'http://proxy.example.test' });

    await expect(client.syncTeamMembers('acme/platform')).resolves.toMatchObject({ operation_id: 'op-1', member_count: 2 });
    expect(fetchMock).toHaveBeenCalledWith(
      'http://proxy.example.test/teams/acme%2Fplatform/members/sync',
      expect.objectContaining({ method: 'POST' }),
    );
  });
});

describe('AgentAPIProxyClient Session Runner Pools', () => {
  afterEach(() => {
    vi.restoreAllMocks();
  });

  it('lists the healthy pools available to the current user', async () => {
    const fetchMock = vi.spyOn(globalThis, 'fetch').mockResolvedValueOnce(
      new Response(JSON.stringify({
        session_pools: [{ name: 'native-linux', enabled: true, labels: { arch: 'amd64' } }],
      }), {
        status: 200,
        headers: { 'Content-Type': 'application/json' },
      }),
    );
    const client = new AgentAPIProxyClient({ baseURL: 'http://proxy.example.test' });

    await expect(client.getAvailableSessionPools()).resolves.toEqual([
      { name: 'native-linux', enabled: true, labels: { arch: 'amd64' } },
    ]);
    expect(fetchMock).toHaveBeenCalledWith(
      'http://proxy.example.test/available-session-pools',
      expect.any(Object),
    );
  });
});

describe('AgentAPIProxyClient concurrent reads', () => {
  afterEach(() => {
    vi.restoreAllMocks();
  });

  it('shares an identical in-flight GET across client instances', async () => {
    let resolveFetch!: (response: Response) => void;
    const fetchMock = vi.spyOn(globalThis, 'fetch').mockImplementation(() =>
      new Promise<Response>(resolve => { resolveFetch = resolve; })
    );
    const firstClient = new AgentAPIProxyClient({ baseURL: 'http://proxy.example.test' });
    const secondClient = new AgentAPIProxyClient({ baseURL: 'http://proxy.example.test' });

    const first = firstClient.search({ limit: 1000, scope: 'user' });
    const second = secondClient.search({ limit: 1000, scope: 'user' });

    expect(fetchMock).toHaveBeenCalledTimes(1);
    resolveFetch(new Response(JSON.stringify({ sessions: [] }), {
      status: 200,
      headers: { 'Content-Type': 'application/json' },
    }));
    await expect(Promise.all([first, second])).resolves.toEqual([
      { sessions: [] },
      { sessions: [] },
    ]);
  });
});

describe('AgentAPIProxyClient session scope isolation', () => {
  afterEach(() => {
    vi.restoreAllMocks();
  });

  it('keeps only sessions from the requested team', async () => {
    vi.spyOn(globalThis, 'fetch').mockResolvedValueOnce(
      new Response(JSON.stringify({
        sessions: [
          { session_id: 'team-a', scope: 'team', team_id: 'acme/a' },
          { session_id: 'team-b', scope: 'team', team_id: 'acme/b' },
          { session_id: 'personal', scope: 'user' },
        ],
      }), {
        status: 200,
        headers: { 'Content-Type': 'application/json' },
      }),
    );
    const client = new AgentAPIProxyClient({ baseURL: 'http://proxy.example.test' });

    await expect(client.search({ scope: 'team', team_id: 'acme/a' })).resolves.toEqual({
      sessions: [{ session_id: 'team-a', scope: 'team', team_id: 'acme/a' }],
    });
  });

  it('treats legacy sessions without a scope as personal', async () => {
    vi.spyOn(globalThis, 'fetch').mockResolvedValueOnce(
      new Response(JSON.stringify({
        sessions: [
          { session_id: 'legacy-personal' },
          { session_id: 'team', scope: 'team', team_id: 'acme/a' },
        ],
      }), {
        status: 200,
        headers: { 'Content-Type': 'application/json' },
      }),
    );
    const client = new AgentAPIProxyClient({ baseURL: 'http://proxy.example.test' });

    await expect(client.search({ scope: 'user' })).resolves.toEqual({
      sessions: [{ session_id: 'legacy-personal' }],
    });
  });
});

describe('session settings reload', () => {
  afterEach(() => vi.restoreAllMocks());
  it('requests a complete reload with an idempotency key', async () => {
    const fetchMock = vi.spyOn(globalThis, 'fetch').mockResolvedValueOnce(new Response(
      JSON.stringify({ session_id: 'one', request_id: 'operation' }),
      { status: 202, headers: { 'Content-Type': 'application/json' } },
    ));
    const client = new AgentAPIProxyClient({ baseURL: 'http://proxy.example.test' });
    await client.restartSession('one');
    const [url, options] = fetchMock.mock.calls[0];
    expect(url).toBe('http://proxy.example.test/sessions/one/restart');
    expect(JSON.parse(options!.body as string)).toEqual({ reload_settings: true, busy_policy: 'wait' });
    expect(new Headers(options!.headers).get('Idempotency-Key')).toBeTruthy();
  });
  it('reads operation status without waking the session', async () => {
    const fetchMock = vi.spyOn(globalThis, 'fetch').mockResolvedValueOnce(new Response(
      JSON.stringify({ phase: 'paused', revision: 2 }), { status: 200 },
    ));
    const client = new AgentAPIProxyClient({ baseURL: 'http://proxy.example.test' });
    await expect(client.restartStatus('one')).resolves.toEqual({ phase: 'paused', revision: 2 });
    expect(fetchMock).toHaveBeenCalledTimes(1);
    expect(fetchMock.mock.calls[0][0]).toBe('http://proxy.example.test/sessions/one/restart');
    expect(fetchMock.mock.calls[0][1]?.method ?? 'GET').toBe('GET');
  });
});
