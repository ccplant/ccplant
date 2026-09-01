import { afterEach, describe, expect, it, vi } from 'vitest'

import {
  getBackendUrl,
  getRequestBackendBaseUrl,
  normalizeBackendBaseUrl,
} from '../server-backend-url'

describe('server backend URL resolution', () => {
  afterEach(() => {
    vi.unstubAllEnvs()
  })

  it('removes a legacy /api/proxy suffix', () => {
    expect(normalizeBackendBaseUrl('http://backend:8080/api/proxy/')).toBe('http://backend:8080')
  })

  it('builds a direct backend endpoint without nesting below /api/proxy', () => {
    vi.stubEnv('AGENTAPI_PROXY_URL', 'http://backend:8080/api/proxy')

    expect(getBackendUrl('/user/info')).toBe('http://backend:8080/user/info')
  })

  it('rejects an empty backend URL', () => {
    expect(() => normalizeBackendBaseUrl('  ')).toThrow('AGENTAPI_PROXY_URL is required')
  })

  it('prefers a persisted route over the default backend', async () => {
    vi.stubEnv('AGENTAPI_PROXY_URL', 'http://default-backend:8080')
    const store = {
      findBySubdomain: vi.fn().mockResolvedValue({
        subdomain: 'alpha', apiUrl: 'https://stored-api.example.test', enabled: true,
      }),
    }

    await expect(getRequestBackendBaseUrl('alpha.ui.example.test', store))
      .resolves.toBe('https://stored-api.example.test')
  })

  it('bypasses persistent routing for the canonical public hostname', async () => {
    vi.stubEnv('AGENTAPI_PROXY_URL', 'https://default-backend.example.test')
    vi.stubEnv('NEXT_PUBLIC_BASE_URL', 'https://app.example.test/')
    const store = {
      findBySubdomain: vi.fn().mockResolvedValue({
        subdomain: 'app', apiUrl: 'https://stored-api.example.test', enabled: true,
      }),
    }

    await expect(getRequestBackendBaseUrl('APP.EXAMPLE.TEST', store))
      .resolves.toBe('https://default-backend.example.test')
    expect(store.findBySubdomain).not.toHaveBeenCalled()
  })

  it('falls back to the default backend when persistent storage fails', async () => {
    vi.stubEnv('AGENTAPI_PROXY_URL', 'http://default-backend:8080')
    const store = {
      findBySubdomain: vi.fn().mockRejectedValue(new Error('D1 unavailable')),
    }

    await expect(getRequestBackendBaseUrl('alpha.ui.example.test', store))
      .resolves.toBe('http://default-backend:8080')
  })
})
