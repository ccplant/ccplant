import { describe, expect, it, vi } from 'vitest'
import { D1DatabaseLike, D1SubdomainRouteStore } from '../subdomain-route-store'

describe('D1 subdomain route store', () => {
  it('selects the newest event for a subdomain', async () => {
    const first = vi.fn().mockResolvedValue({
      subdomain: 'team-a',
      api_url: 'https://new-api.example.test',
      enabled: 1,
    })
    const bind = vi.fn().mockReturnValue({ first })
    const prepare = vi.fn().mockReturnValue({ bind })
    const store = new D1SubdomainRouteStore({ prepare } as unknown as D1DatabaseLike)

    await expect(store.findBySubdomain('team-a')).resolves.toEqual({
      subdomain: 'team-a',
      apiUrl: 'https://new-api.example.test',
      enabled: true,
    })

    expect(prepare.mock.calls[0][0]).toContain('ORDER BY id DESC')
    expect(prepare.mock.calls[0][0]).toContain('LIMIT 1')
    expect(bind).toHaveBeenCalledWith('team-a')
  })

  it('selects branding from the newest event for a subdomain', async () => {
    const icon = new Uint8Array([1, 2, 3]).buffer
    const first = vi.fn().mockResolvedValue({
      app_title: 'Team A',
      app_icon: icon,
      app_icon_content_type: 'image/png',
    })
    const bind = vi.fn().mockReturnValue({ first })
    const prepare = vi.fn().mockReturnValue({ bind })
    const store = new D1SubdomainRouteStore({ prepare } as unknown as D1DatabaseLike)

    await expect(store.findBrandingBySubdomain('team-a')).resolves.toEqual({
      appTitle: 'Team A',
      appIcon: icon,
      appIconContentType: 'image/png',
    })
    expect(prepare.mock.calls[0][0]).toContain('app_icon_content_type')
    expect(prepare.mock.calls[0][0]).toContain('ORDER BY id DESC')
    expect(bind).toHaveBeenCalledWith('team-a')
  })

  it('converts a D1 blob number array into an ArrayBuffer', async () => {
    const first = vi.fn().mockResolvedValue({
      app_title: 'Team A',
      app_icon: [137, 80, 78, 71],
      app_icon_content_type: 'image/png',
    })
    const bind = vi.fn().mockReturnValue({ first })
    const prepare = vi.fn().mockReturnValue({ bind })
    const store = new D1SubdomainRouteStore({ prepare } as unknown as D1DatabaseLike)

    const branding = await store.findBrandingBySubdomain('team-a')
    expect(Array.from(new Uint8Array(branding!.appIcon!))).toEqual([137, 80, 78, 71])
  })
})
