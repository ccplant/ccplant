import { afterEach, describe, expect, it, vi } from 'vitest'
import { NextRequest } from 'next/server'

import { GET, POST } from './route'

describe('/api/v1 authentication', () => {
  const originalProxyUrl = process.env.AGENTAPI_PROXY_URL

  afterEach(() => {
    vi.restoreAllMocks()
    if (originalProxyUrl === undefined) {
      delete process.env.AGENTAPI_PROXY_URL
    } else {
      process.env.AGENTAPI_PROXY_URL = originalProxyUrl
    }
  })

  it('passes through Authorization without reading the cookie', async () => {
    process.env.AGENTAPI_PROXY_URL = 'http://backend:8080'
    const fetchMock = vi.spyOn(globalThis, 'fetch').mockResolvedValue(
      Response.json({ ok: true }),
    )
    const request = new NextRequest('https://ui.example.test/api/v1/sessions?limit=10', {
      headers: { Authorization: 'Bearer api-client-token' },
    })

    const response = await GET(request, {
      params: Promise.resolve({ path: ['sessions'] }),
    })

    const [url, options] = fetchMock.mock.calls[0]
    expect(url).toBe('http://backend:8080/sessions?limit=10')
    expect(new Headers(options?.headers).get('authorization')).toBe('Bearer api-client-token')
    expect(response.status).toBe(200)
  })

  it('forwards external session manager enrollment without a UI session', async () => {
    process.env.AGENTAPI_PROXY_URL = 'http://backend:8080'
    const fetchMock = vi.spyOn(globalThis, 'fetch').mockResolvedValue(
      Response.json({ id: 'manager-1', connection_token: 'connection-token' }),
    )
    const request = new NextRequest(
      'https://ui.example.test/api/v1/session-managers/enroll',
      {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ registration_token: 'one-time-token' }),
      },
    )

    const response = await POST(request, {
      params: Promise.resolve({ path: ['session-managers', 'enroll'] }),
    })

    const [url, options] = fetchMock.mock.calls[0]
    expect(url).toBe('http://backend:8080/session-managers/enroll')
    expect(new Headers(options?.headers).has('authorization')).toBe(false)
    expect(response.status).toBe(200)
  })
})
