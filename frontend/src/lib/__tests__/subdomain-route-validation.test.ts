import { describe, expect, it } from 'vitest'
import { normalizeSubdomain, normalizeUserApiUrl } from '../subdomain-route-validation'

describe('subdomain route validation', () => {
  it('normalizes valid route values', () => {
    expect(normalizeSubdomain(' Team-A ')).toBe('team-a')
    expect(normalizeUserApiUrl('https://api.example.com/')).toBe('https://api.example.com')
  })

  it.each([
    'http://api.example.com',
    'https://localhost:8080',
    'https://127.0.0.1',
    'https://192.168.1.1',
    'https://user:password@api.example.com',
    'https://api.example.com?token=secret',
  ])('rejects unsafe API URL %s', (url) => {
    expect(() => normalizeUserApiUrl(url)).toThrow()
  })
})
