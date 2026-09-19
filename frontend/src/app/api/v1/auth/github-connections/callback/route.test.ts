import { afterEach, describe, expect, it, vi } from 'vitest'
import { NextRequest } from 'next/server'

import { GET } from './route'

describe('/api/v1/auth/github-connections/callback', () => {
  const originalProxyUrl = process.env.AGENTAPI_PROXY_URL

  afterEach(() => {
    vi.restoreAllMocks()
    if (originalProxyUrl === undefined) {
      delete process.env.AGENTAPI_PROXY_URL
    } else {
      process.env.AGENTAPI_PROXY_URL = originalProxyUrl
    }
  })

  it('completes account linking without an existing UI session', async () => {
    process.env.AGENTAPI_PROXY_URL = 'http://backend:8080'
    const fetchMock = vi.spyOn(globalThis, 'fetch').mockResolvedValue(
      new Response(null, {
        status: 302,
        headers: { Location: '/settings/personal/account-connections?github_link=success' },
      }),
    )
    const request = new NextRequest(
      'https://ui.example.test/api/v1/auth/github-connections/callback?code=oauth-code&state=oauth-state',
    )

    const response = await GET(request)

    const [url, options] = fetchMock.mock.calls[0]
    expect(url.toString()).toBe(
      'http://backend:8080/auth/github-connections/callback?state=oauth-state&code=oauth-code',
    )
    expect(new Headers(options?.headers).has('authorization')).toBe(false)
    expect(response.status).toBe(307)
    expect(response.headers.get('location')).toBe(
      'https://ui.example.test/settings/personal/account-connections?github_link=success',
    )
  })
})
