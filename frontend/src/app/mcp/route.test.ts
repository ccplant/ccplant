import { afterEach, describe, expect, it, vi } from 'vitest'
import { NextRequest } from 'next/server'

import { POST } from './route'

describe('/mcp proxy', () => {
  afterEach(() => {
    vi.unstubAllEnvs()
    vi.restoreAllMocks()
  })

  it('forwards MCP requests to the backend MCP endpoint', async () => {
    vi.stubEnv('AGENTAPI_PROXY_URL', 'http://backend:8080')
    const fetchMock = vi.spyOn(globalThis, 'fetch').mockResolvedValue(
      Response.json({ jsonrpc: '2.0', result: { protocolVersion: '2025-06-18' }, id: 1 }),
    )
    const request = new NextRequest('https://app.example.test/mcp?client=codex', {
      method: 'POST',
      headers: {
        Authorization: 'Bearer api-client-token',
        'Content-Type': 'application/json',
        Accept: 'application/json, text/event-stream',
        'Mcp-Protocol-Version': '2025-06-18',
      },
      body: JSON.stringify({ jsonrpc: '2.0', method: 'initialize', id: 1 }),
    })

    const response = await POST(request, { params: Promise.resolve({}) })

    const [url, options] = fetchMock.mock.calls[0]
    const headers = new Headers(options?.headers)
    expect(url).toBe('http://backend:8080/mcp?client=codex')
    expect(headers.get('authorization')).toBe('Bearer api-client-token')
    expect(headers.get('mcp-protocol-version')).toBe('2025-06-18')
    expect(response.status).toBe(200)
    await expect(response.json()).resolves.toMatchObject({ jsonrpc: '2.0', id: 1 })
  })
})
