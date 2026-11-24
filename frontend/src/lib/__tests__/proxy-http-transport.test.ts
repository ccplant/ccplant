import { afterEach, describe, expect, it, vi } from 'vitest';
import { AgentAPIProxyClient, AgentAPIProxyError } from '../agentapi-proxy-client';

afterEach(() => vi.restoreAllMocks());

describe('proxy HTTP authentication and error responses', () => {
  it('retries a rejected Bearer credential with the API-key header', async () => {
    const fetchMock = vi
      .spyOn(globalThis, 'fetch')
      .mockResolvedValueOnce(new Response('{}', { status: 401 }))
      .mockResolvedValueOnce(new Response('{"name":"settings"}', { status: 200 }));
    const client = new AgentAPIProxyClient({ baseURL: 'http://proxy.example.test', apiKey: 'test-api-key' });
    await expect(client.getSettings('owner')).resolves.toEqual({ name: 'settings' });
    expect(fetchMock.mock.calls[0][1]?.headers).toMatchObject({ Authorization: 'Bearer test-api-key' });
    expect(fetchMock.mock.calls[1][1]?.headers).toMatchObject({ 'X-API-Key': 'test-api-key' });
    expect(fetchMock.mock.calls[1][1]?.headers).not.toHaveProperty('Authorization');
  });

  it('preserves structured errors without retrying non-authentication failures', async () => {
    const fetchMock = vi.spyOn(globalThis, 'fetch').mockResolvedValueOnce(
      new Response(
        JSON.stringify({
          error: { code: 'invalid_settings', message: 'Invalid settings', details: { field: 'name' } },
        }),
        { status: 400 }
      )
    );
    const client = new AgentAPIProxyClient({ baseURL: 'http://proxy.example.test', apiKey: 'test-api-key' });
    await expect(client.getSettings('owner')).rejects.toMatchObject({
      status: 400,
      code: 'invalid_settings',
      message: 'Invalid settings',
      details: { field: 'name' },
    });
    expect(fetchMock).toHaveBeenCalledTimes(1);
  });

  it('accepts empty successful responses', async () => {
    vi.spyOn(globalThis, 'fetch').mockResolvedValueOnce(new Response(null, { status: 204 }));
    const client = new AgentAPIProxyClient({ baseURL: 'http://proxy.example.test' });
    await expect(client.deleteWebhook('webhook')).resolves.toBeUndefined();
  });

  it('keeps network failures distinguishable from HTTP errors', async () => {
    vi.spyOn(globalThis, 'fetch').mockRejectedValueOnce(new Error('Connection lost'));
    const client = new AgentAPIProxyClient({ baseURL: 'http://proxy.example.test' });
    const failure = client.getSettings('owner');
    await expect(failure).rejects.toBeInstanceOf(AgentAPIProxyError);
    await expect(failure).rejects.toMatchObject({ status: 0, code: 'NETWORK_ERROR', message: 'Connection lost' });
  });
});
