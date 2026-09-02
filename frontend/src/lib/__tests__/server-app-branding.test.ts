import { afterEach, describe, expect, it, vi } from 'vitest'
import { getRequestAppBranding } from '../server-app-branding'

describe('server app branding', () => {
  afterEach(() => vi.unstubAllEnvs())

  it('resolves a title and uploaded icon for a tenant subdomain', async () => {
    const icon = new Uint8Array([1, 2, 3]).buffer
    const store = {
      findBrandingBySubdomain: vi.fn().mockResolvedValue({
        appTitle: 'Acme Agents',
        appIcon: icon,
        appIconContentType: 'image/png',
      }),
    }

    await expect(getRequestAppBranding('acme.example.test', store)).resolves.toEqual({
      appTitle: 'Acme Agents',
      iconUrl: '/api/app-icon',
      icon,
      iconContentType: 'image/png',
    })
    expect(store.findBrandingBySubdomain).toHaveBeenCalledWith('acme')
  })

  it('falls back when D1 branding is unavailable', async () => {
    vi.stubEnv('PWA_APP_NAME', 'Default Agents')
    vi.stubEnv('PWA_ICON_URL', 'https://assets.example.test/icon.png')
    const store = {
      findBrandingBySubdomain: vi.fn().mockRejectedValue(new Error('missing columns')),
    }

    await expect(getRequestAppBranding('acme.example.test', store)).resolves.toMatchObject({
      appTitle: 'Default Agents',
      iconUrl: 'https://assets.example.test/icon.png',
      icon: null,
    })
  })

  it('rejects an unsupported uploaded icon content type', async () => {
    const store = {
      findBrandingBySubdomain: vi.fn().mockResolvedValue({
        appTitle: 'Acme',
        appIcon: new Uint8Array([1]).buffer,
        appIconContentType: 'text/html',
      }),
    }

    await expect(getRequestAppBranding('acme.example.test', store)).resolves.toMatchObject({
      appTitle: 'Acme',
      iconUrl: null,
      icon: null,
    })
  })
})
