import { afterEach, describe, expect, it, vi } from 'vitest'

import {
  getBackendBaseUrl,
  getBackendUrl,
  getRequestBackendBaseUrl,
  normalizeBackendBaseUrl,
  resolveMappedBackendUrl,
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

  it('selects a backend by the request subdomain', () => {
    vi.stubEnv('AGENTAPI_PROXY_URL', 'http://default-backend:8080')
    vi.stubEnv('AGENTAPI_PROXY_SUBDOMAIN_MAP', JSON.stringify({
      alpha: 'https://alpha-api.example.test/api/proxy/',
      beta: 'https://beta-api.example.test',
    }))

    expect(getBackendBaseUrl('alpha.ui.example.test')).toBe('https://alpha-api.example.test')
    expect(getBackendUrl('/sessions', 'beta.ui.example.test')).toBe('https://beta-api.example.test/sessions')
  })

  it('falls back to the default backend when the subdomain is not mapped', () => {
    vi.stubEnv('AGENTAPI_PROXY_URL', 'http://default-backend:8080')
    vi.stubEnv('AGENTAPI_PROXY_SUBDOMAIN_MAP', '{"alpha":"https://alpha-api.example.test"}')

    expect(getBackendBaseUrl('unknown.ui.example.test')).toBe('http://default-backend:8080')
  })

  it('rejects an invalid subdomain map', () => {
    expect(() => resolveMappedBackendUrl('alpha.ui.example.test', '[]'))
      .toThrow('AGENTAPI_PROXY_SUBDOMAIN_MAP must be a JSON object')
    expect(() => resolveMappedBackendUrl('alpha.ui.example.test', '{invalid'))
      .toThrow('AGENTAPI_PROXY_SUBDOMAIN_MAP must be a valid JSON object')
  })

  it('prefers a persisted route over environment configuration', async () => {
    vi.stubEnv('AGENTAPI_PROXY_URL', 'http://default-backend:8080')
    vi.stubEnv('AGENTAPI_PROXY_SUBDOMAIN_MAP', '{"alpha":"https://env-api.example.test"}')
    const store = {
      findBySubdomain: vi.fn().mockResolvedValue({
        subdomain: 'alpha', ownerId: 'alice', apiUrl: 'https://stored-api.example.test', enabled: true,
      }),
      findByOwner: vi.fn(),
      upsert: vi.fn(),
      deleteByOwner: vi.fn(),
    }

    await expect(getRequestBackendBaseUrl('alpha.ui.example.test', store))
      .resolves.toBe('https://stored-api.example.test')
  })

  it('falls back to environment configuration when persistent storage fails', async () => {
    vi.stubEnv('AGENTAPI_PROXY_URL', 'http://default-backend:8080')
    vi.stubEnv('AGENTAPI_PROXY_SUBDOMAIN_MAP', '{"alpha":"https://env-api.example.test"}')
    const store = {
      findBySubdomain: vi.fn().mockRejectedValue(new Error('D1 unavailable')),
      findByOwner: vi.fn(),
      upsert: vi.fn(),
      deleteByOwner: vi.fn(),
    }

    await expect(getRequestBackendBaseUrl('alpha.ui.example.test', store))
      .resolves.toBe('https://env-api.example.test')
  })
})
