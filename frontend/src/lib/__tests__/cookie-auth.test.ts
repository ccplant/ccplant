import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import {
  AUTH_COOKIE_VERSION,
  decryptApiKey,
  decryptAuthCookie,
  encryptApiKey,
  encryptOAuthSession,
  renewApiKeyCookie,
} from '../cookie-auth'

describe('versioned authentication cookie', () => {
  const cookieGet = vi.fn()
  const cookieSet = vi.fn()
  const cookieStore = { get: cookieGet, set: cookieSet }

  beforeEach(() => {
    vi.stubEnv('COOKIE_ENCRYPTION_SECRET', '11'.repeat(32))
    cookieGet.mockReset()
    cookieSet.mockReset()
  })

  afterEach(() => {
    vi.unstubAllEnvs()
  })

  it('stores an API key in the versioned schema', () => {
    const encrypted = encryptApiKey('api-key-value')

    expect(encrypted).not.toContain('api-key-value')
    expect(decryptAuthCookie(encrypted)).toEqual({
      version: AUTH_COOKIE_VERSION,
      type: 'api_key',
      access_token: 'api-key-value',
    })
    expect(decryptApiKey(encrypted)).toBe('api-key-value')
  })

  it('keeps the OAuth session id and access token in one encrypted payload', () => {
    const encrypted = encryptOAuthSession('session-123', 'github-access-token')

    expect(encrypted).not.toContain('github-access-token')
    expect(decryptAuthCookie(encrypted)).toEqual({
      version: AUTH_COOKIE_VERSION,
      type: 'github_oauth',
      session_id: 'session-123',
      access_token: 'github-access-token',
    })
  })

  it('rejects tampered ciphertext', () => {
    const encrypted = encryptOAuthSession('session-123', 'github-access-token')
    const tampered = `${encrypted.slice(0, -2)}AA`

    expect(() => decryptAuthCookie(tampered)).toThrow()
  })

  it('rejects a non-hex encryption key', () => {
    vi.stubEnv('COOKIE_ENCRYPTION_SECRET', 'z'.repeat(64))

    expect(() => encryptApiKey('token')).toThrow('64 hex characters')
  })

  it('renews an existing authentication cookie when the renewal marker has expired', async () => {
    cookieGet.mockImplementation((name: string) => (
      name === 'agentapi_token' ? { value: 'encrypted-cookie' } : undefined
    ))

    await renewApiKeyCookie(cookieStore as never)

    expect(cookieSet).toHaveBeenCalledTimes(2)
    expect(cookieSet).toHaveBeenNthCalledWith(
      1,
      'agentapi_token',
      'encrypted-cookie',
      expect.objectContaining({ maxAge: 30 * 24 * 60 * 60 }),
    )
    expect(cookieSet).toHaveBeenNthCalledWith(
      2,
      'agentapi_token_renewed',
      '1',
      expect.objectContaining({ maxAge: 24 * 60 * 60 }),
    )
  })

  it('does not emit cookies again while the renewal marker is present', async () => {
    cookieGet.mockImplementation((name: string) => (
      name === 'agentapi_token' ? { value: 'encrypted-cookie' } : { value: '1' }
    ))

    await renewApiKeyCookie(cookieStore as never)

    expect(cookieSet).not.toHaveBeenCalled()
  })
})
