import { afterEach, describe, expect, it, vi } from 'vitest';

import { AgentAPIProxyClient, AgentAPIProxyError } from '../agentapi-proxy-client';

describe('AgentAPIProxyClient ACP message history', () => {
  afterEach(() => {
    vi.restoreAllMocks();
    vi.unstubAllEnvs();
  });

  it('resumes a session only through the explicit resume endpoint', async () => {
    const fetchMock = vi.spyOn(globalThis, 'fetch').mockResolvedValueOnce(
      new Response(JSON.stringify({ session_id: 'session-1', status: 'restoring' }), {
        status: 202,
        headers: { 'Content-Type': 'application/json' },
      })
    );
    const client = new AgentAPIProxyClient({ baseURL: 'http://proxy.example.test' });

    await expect(client.resumeSession('session-1')).resolves.toEqual({
      session_id: 'session-1',
      status: 'restoring',
    });
    expect(fetchMock).toHaveBeenCalledWith(
      'http://proxy.example.test/sessions/session-1/resume',
      expect.objectContaining({ method: 'POST' }),
    );
  });

  it('suspends a session through the explicit suspend endpoint', async () => {
    const fetchMock = vi.spyOn(globalThis, 'fetch').mockResolvedValueOnce(
      new Response(JSON.stringify({ session_id: 'session-1', status: 'suspending' }), {
        status: 200,
        headers: { 'Content-Type': 'application/json' },
      })
    );
    const client = new AgentAPIProxyClient({ baseURL: 'http://proxy.example.test' });

    await expect(client.suspendSession('session-1')).resolves.toEqual({
      session_id: 'session-1',
      status: 'suspending',
    });
    expect(fetchMock).toHaveBeenCalledWith(
      'http://proxy.example.test/sessions/session-1/suspend',
      expect.objectContaining({ method: 'POST' }),
    );
  });

});

describe('AgentAPIProxyClient rejects when history cannot be', () => {
  afterEach(() => {
    vi.restoreAllMocks();
    vi.unstubAllEnvs();
  });

  it('rejects when history cannot be fetched instead of returning an empty history', async () => {
    vi.spyOn(globalThis, 'fetch').mockRejectedValueOnce(new Error('connection lost'));
    const client = new AgentAPIProxyClient({ baseURL: 'http://proxy.example.test' });

    await expect(client.getACPMessageHistory('session-1', 'acp-session-1')).rejects.toMatchObject({
      status: 0,
      code: 'NETWORK_ERROR',
      message: 'connection lost',
    } satisfies Partial<AgentAPIProxyError>);
  });

  it('sends text and image content blocks unchanged to an ACP session', async () => {
    const fetchMock = vi.spyOn(globalThis, 'fetch').mockResolvedValueOnce(
      new Response(JSON.stringify({ ok: true }), {
        status: 202,
        headers: { 'Content-Type': 'application/json' },
      })
    );
    const client = new AgentAPIProxyClient({ baseURL: 'http://proxy.example.test' });

    await client.sendACPPrompt(
      'session-1',
      'acp-session-1',
      [
        { type: 'text', text: 'What is in this image?' },
        { type: 'image', mimeType: 'image/png', data: 'iVBORw0KGgo=' },
      ],
      42
    );

    const request = fetchMock.mock.calls[0][1] as RequestInit;
    expect(JSON.parse(String(request.body))).toMatchObject({
      id: 42,
      method: 'session/prompt',
      params: {
        sessionId: 'acp-session-1',
        prompt: [
          { type: 'text', text: 'What is in this image?' },
          { type: 'image', mimeType: 'image/png', data: 'iVBORw0KGgo=' },
        ],
      },
    });
  });

});

describe('AgentAPIProxyClient restores ACP image output from', () => {
  afterEach(() => {
    vi.restoreAllMocks();
    vi.unstubAllEnvs();
  });

  it('restores ACP image output from message history', async () => {
    vi.spyOn(globalThis, 'fetch').mockResolvedValueOnce(
      new Response(JSON.stringify({
        messages: [{
          jsonrpc: '2.0',
          method: 'session/update',
          params: {
            sessionId: 'acp-session-1',
            time: '2026-07-25T00:00:00Z',
            update: {
              sessionUpdate: 'agent_message_chunk',
              content: { type: 'image', mimeType: 'image/png', data: 'iVBORw0KGgo=' },
            },
          },
        }],
        userPromptCount: 0,
        lastEventId: 7,
        userPrompts: [],
      }), {
        status: 200,
        headers: { 'Content-Type': 'application/json' },
      })
    );
    const client = new AgentAPIProxyClient({ baseURL: 'http://proxy.example.test' });

    const history = await client.getACPMessageHistory('session-1', 'acp-session-1');

    expect(history.messages).toHaveLength(1);
    expect(history.messages[0].images).toEqual([
      { mimeType: 'image/png', data: 'iVBORw0KGgo=' },
    ]);
    expect(history.lastEventId).toBe(7);
  });

});

describe('AgentAPIProxyClient restores images nested in completed', () => {
  afterEach(() => {
    vi.restoreAllMocks();
    vi.unstubAllEnvs();
  });

  it('restores images nested in completed ACP tool results', async () => {
    vi.spyOn(globalThis, 'fetch').mockResolvedValueOnce(
      new Response(JSON.stringify({
        messages: [
          {
            jsonrpc: '2.0',
            method: 'session/update',
            params: {
              sessionId: 'acp-session-1',
              update: {
                sessionUpdate: 'tool_call',
                toolCallId: 'image-tool-1',
                kind: 'other',
                title: 'Generate image',
              },
            },
          },
          {
            jsonrpc: '2.0',
            method: 'session/update',
            params: {
              sessionId: 'acp-session-1',
              update: {
                sessionUpdate: 'tool_call_update',
                toolCallId: 'image-tool-1',
                status: 'completed',
                content: [{
                  type: 'content',
                  content: {
                    type: 'image',
                    mimeType: 'image/png',
                    data: 'generated-image-data',
                    uri: '/tmp/generated.png',
                  },
                }],
              },
            },
          },
        ],
        userPromptCount: 0,
        userPrompts: [],
      }), {
        status: 200,
        headers: { 'Content-Type': 'application/json' },
      }),
    );
    const client = new AgentAPIProxyClient({ baseURL: 'http://proxy.example.test' });

    const history = await client.getACPMessageHistory('session-1', 'acp-session-1');
    const toolResult = history.messages.find(message => message.role === 'tool_result');

    expect(toolResult?.images).toEqual([
      { mimeType: 'image/png', data: 'generated-image-data' },
    ]);
  });

});

describe('AgentAPIProxyClient does not ask the BFF', () => {
  afterEach(() => {
    vi.restoreAllMocks();
    vi.unstubAllEnvs();
  });

  it('does not ask the BFF to inject the login token into a session body', async () => {
    const fetchMock = vi.spyOn(globalThis, 'fetch').mockResolvedValueOnce(
      new Response(JSON.stringify({ session_id: 'session-1' }), {
        status: 200,
        headers: { 'Content-Type': 'application/json' },
      }),
    );
    const client = new AgentAPIProxyClient({
      baseURL: 'http://proxy.example.test',
      apiKey: 'login-api-key',
    });

    await client.start({ params: { message: 'hello' } });

    const request = fetchMock.mock.calls[0][1] as RequestInit;
    expect(new Headers(request.headers).get('x-inject-github-token')).toBeNull();
    expect(String(request.body)).not.toContain('login-api-key');
  });

});

describe('AgentAPIProxyClient registers a one-time session secret', () => {
  afterEach(() => {
    vi.restoreAllMocks();
    vi.unstubAllEnvs();
  });

  it('registers a one-time session secret without logging its value', async () => {
    vi.stubEnv('NODE_ENV', 'development');
    const logSpy = vi.spyOn(console, 'log').mockImplementation(() => undefined);
    const fetchMock = vi.spyOn(globalThis, 'fetch').mockResolvedValueOnce(
      new Response(JSON.stringify({
        secret_id: 'secret-1',
        expires_at: '2026-09-24T05:00:00Z',
        local_url: 'http+unix://%2Ftmp%2Fccplant%2Fprovisioner.sock/one-time-secrets/secret-1',
      }), {
        status: 201,
        headers: { 'Content-Type': 'application/json' },
      }),
    );
    const client = new AgentAPIProxyClient({
      baseURL: 'http://proxy.example.test',
      apiKey: 'login-api-key',
      debug: true,
    });

    await client.createSessionSecret('session/one', 'one-time-value', 1800);

    expect(fetchMock).toHaveBeenCalledWith(
      'http://proxy.example.test/sessions/session%2Fone/secrets',
      expect.objectContaining({
        method: 'POST',
        body: JSON.stringify({ value: 'one-time-value', expires_in_seconds: 1800 }),
      }),
    );
    expect(JSON.stringify(logSpy.mock.calls)).not.toContain('one-time-value');
  });

  it('reauthorizes a consumed session secret without resending its value', async () => {
    const fetchMock = vi.spyOn(globalThis, 'fetch').mockResolvedValueOnce(
      new Response(JSON.stringify({
        secret_id: 'secret/one',
        expires_at: '2026-09-25T05:00:00Z',
        local_url: 'http+unix://%2Ftmp%2Fccplant%2Fprovisioner.sock/one-time-secrets/secret%2Fone',
      }), {
        status: 200,
        headers: { 'Content-Type': 'application/json' },
      }),
    );
    const client = new AgentAPIProxyClient({
      baseURL: 'http://proxy.example.test',
      apiKey: 'login-api-key',
    });

    await client.reauthorizeSessionSecret('session/one', 'secret/one', 1800);

    expect(fetchMock).toHaveBeenCalledWith(
      'http://proxy.example.test/sessions/session%2Fone/secrets/secret%2Fone/reauthorize',
      expect.objectContaining({
        method: 'POST',
        body: JSON.stringify({ expires_in_seconds: 1800 }),
      }),
    );
    expect(JSON.stringify(fetchMock.mock.calls)).not.toContain('one-time-value');
  });

});

describe('AgentAPIProxyClient lists session secret metadata without', () => {
  afterEach(() => {
    vi.restoreAllMocks();
    vi.unstubAllEnvs();
  });

  it('lists session secret metadata without secret values', async () => {
    const fetchMock = vi.spyOn(globalThis, 'fetch').mockResolvedValueOnce(
      new Response(JSON.stringify({
        secrets: [{
          secret_id: 'secret-1',
          status: 'consumed',
          created_at: '2026-09-25T05:00:00Z',
          expires_at: '2026-09-25T05:10:00Z',
          consumed_at: '2026-09-25T05:01:00Z',
        }],
      }), {
        status: 200,
        headers: { 'Content-Type': 'application/json' },
      }),
    );
    const client = new AgentAPIProxyClient({
      baseURL: 'http://proxy.example.test',
      apiKey: 'login-api-key',
    });

    const response = await client.listSessionSecrets('session/one');

    expect(fetchMock).toHaveBeenCalledWith(
      'http://proxy.example.test/sessions/session%2Fone/secrets',
      expect.any(Object),
    );
    expect(response.secrets[0]).toMatchObject({ secret_id: 'secret-1', status: 'consumed' });
    expect(JSON.stringify(response)).not.toContain('one-time-value');
  });

});

describe('AgentAPIProxyClient never enables debug logging in', () => {
  afterEach(() => {
    vi.restoreAllMocks();
    vi.unstubAllEnvs();
  });

  it('never enables debug logging in production even when requested', async () => {
    vi.stubEnv('NODE_ENV', 'production');
    const logSpy = vi.spyOn(console, 'log').mockImplementation(() => undefined);
    vi.spyOn(globalThis, 'fetch').mockResolvedValueOnce(
      new Response(JSON.stringify({ name: 'alice', has_data: true }), {
        status: 200,
        headers: { 'Content-Type': 'application/json' },
      }),
    );
    const client = new AgentAPIProxyClient({
      baseURL: 'http://proxy.example.test',
      apiKey: 'login-api-key',
      debug: true,
    });

    await client.uploadCredentials('alice', { token: 'credential-secret' });

    expect(logSpy).not.toHaveBeenCalled();
  });

});

describe('AgentAPIProxyClient omits request and response secrets', () => {
  afterEach(() => {
    vi.restoreAllMocks();
    vi.unstubAllEnvs();
  });

  it('omits request and response secrets from opt-in development logs', async () => {
    vi.stubEnv('NODE_ENV', 'development');
    const logSpy = vi.spyOn(console, 'log').mockImplementation(() => undefined);
    vi.spyOn(globalThis, 'fetch').mockResolvedValueOnce(
      new Response(JSON.stringify({ id: 'slackbot-1' }), {
        status: 200,
        headers: { 'Content-Type': 'application/json' },
      }),
    );
    const client = new AgentAPIProxyClient({
      baseURL: 'http://proxy.example.test?token=url-secret',
      apiKey: 'login-api-key',
      debug: true,
    });

    await client.createSlackBot({
      name: 'bot',
      bot_token: 'xoxb-super-secret',
      app_token: 'xapp-super-secret',
    });

    const serializedLogs = JSON.stringify(logSpy.mock.calls);
    expect(serializedLogs).not.toContain('xoxb-super-secret');
    expect(serializedLogs).not.toContain('xapp-super-secret');
    expect(serializedLogs).not.toContain('login-api-key');
    expect(serializedLogs).not.toContain('url-secret');
  });
});



