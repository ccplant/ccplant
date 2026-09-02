import {
  getOptionalSubdomainRouteStore,
  SubdomainBranding,
  SubdomainBrandingStore,
} from './subdomain-route-store'
import { getSubdomain } from './server-backend-url'

export interface AppBranding {
  appTitle: string
  iconUrl: string | null
  icon: ArrayBuffer | null
  iconContentType: string | null
}

const DEFAULT_TITLE = 'ccplant'
const ALLOWED_ICON_TYPES = new Set(['image/png', 'image/jpeg', 'image/webp', 'image/svg+xml', 'image/x-icon'])

function defaults(): AppBranding {
  return {
    appTitle: process.env.PWA_APP_NAME || process.env.NEXT_PUBLIC_PWA_APP_NAME || DEFAULT_TITLE,
    iconUrl: process.env.PWA_ICON_URL || process.env.FAVICON_URL || null,
    icon: null,
    iconContentType: null,
  }
}

function validIcon(branding: SubdomainBranding | null): boolean {
  return Boolean(
    branding?.appIcon
    && branding.appIcon.byteLength > 0
    && branding.appIconContentType
    && ALLOWED_ICON_TYPES.has(branding.appIconContentType.toLowerCase()),
  )
}

export async function getRequestAppBranding(
  hostname: string,
  store?: SubdomainBrandingStore | null,
): Promise<AppBranding> {
  const fallback = defaults()
  const subdomain = getSubdomain(hostname)
  if (!subdomain) return fallback

  const brandingStore = store === undefined
    ? await getOptionalSubdomainRouteStore()
    : store

  if (!brandingStore) return fallback

  try {
    const branding = await brandingStore.findBrandingBySubdomain(subdomain)
    const title = branding?.appTitle?.trim()
    const hasIcon = validIcon(branding)
    return {
      appTitle: title || fallback.appTitle,
      iconUrl: hasIcon ? '/api/app-icon' : fallback.iconUrl,
      icon: hasIcon ? branding!.appIcon : null,
      iconContentType: hasIcon ? branding!.appIconContentType!.toLowerCase() : null,
    }
  } catch (error) {
    // Older databases without the branding columns keep working with env defaults.
    console.error('Failed to resolve app branding from persistent storage:', error)
    return fallback
  }
}
