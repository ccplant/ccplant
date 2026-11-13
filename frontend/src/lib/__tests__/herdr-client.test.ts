import { afterEach, describe, expect, it, vi } from 'vitest';
import { AgentAPIProxyClient } from '../agentapi-proxy-client';

describe('Herdr transport', () => {
  afterEach(() => vi.restoreAllMocks());
  const target = { server_id: 'local', pane_id: 'w1:p1', generation: 'fingerprint' };
  it('encodes session/target and bypasses browser caching for output', async () => {
    const fetch = vi.spyOn(globalThis, 'fetch').mockResolvedValueOnce(new Response(JSON.stringify({ text: 'screen' }), { status: 200 }));
    const client = new AgentAPIProxyClient({ baseURL: 'http://proxy.example.test' });
    await expect(client.readHerdrOutput('session/one', target)).resolves.toEqual({ text: 'screen' });
    expect(fetch).toHaveBeenCalledWith('http://proxy.example.test/session%2Fone/herdr/output?server_id=local&pane_id=w1%3Ap1&generation=fingerprint', expect.objectContaining({ cache: 'no-store' }));
  });
  it('does not retry an uncertain POST', async () => {
    const fetch = vi.spyOn(globalThis, 'fetch').mockRejectedValueOnce(new Error('disconnect'));
    const client = new AgentAPIProxyClient({ baseURL: 'http://proxy.example.test' });
    await expect(client.sendHerdrInput('session', target, 'hello')).rejects.toThrow('disconnect');
    expect(fetch).toHaveBeenCalledTimes(1);
    expect(fetch).toHaveBeenCalledWith('http://proxy.example.test/session/herdr/input', expect.objectContaining({ method: 'POST', body: JSON.stringify({ ...target, text: 'hello' }) }));
  });
});