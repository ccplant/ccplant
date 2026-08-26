import { getOptionalSubdomainRouteStore, SubdomainRouteStore } from './subdomain-route-store'

/**
 * Resolve the internal backend URL used by Next.js route handlers.
 *
 * Older deployments sometimes configured the frontend with a URL ending in
 * `/api/proxy`. Route handlers now talk to the backend directly, so strip that
 * legacy suffix instead of accidentally nesting requests below it.
 */
export function normalizeBackendBaseUrl(configuredUrl: string): string {
  const value = configuredUrl.trim()
  if (!value) {
    throw new Error('AGENTAPI_PROXY_URL is required')
  }

  const url = new URL(value)
  url.hash = ''
  url.search = ''
  url.pathname = url.pathname
    .replace(/\/api\/proxy\/?$/, '')
    .replace(/\/+$/, '')

  return url.toString().replace(/\/$/, '')
}

export function getSubdomain(hostname: string): string | undefined {
  const normalizedHostname = hostname.trim().toLowerCase().replace(/\.$/, '')
  if (!normalizedHostname || normalizedHostname === 'localhost') return undefined

  const [subdomain, ...rest] = normalizedHostname.split('.')
  return rest.length > 0 ? subdomain : undefined
}

export function getBackendBaseUrl(): string {
  const configuredUrl = process.env.AGENTAPI_PROXY_URL || process.env.AGENTAPI_PROXY_ENDPOINT
  if (!configuredUrl) {
    throw new Error('AGENTAPI_PROXY_URL is required')
  }
  return normalizeBackendBaseUrl(configuredUrl)
}

export async function getRequestBackendBaseUrl(
  hostname: string,
  store?: SubdomainRouteStore | null,
): Promise<string> {
  const subdomain = getSubdomain(hostname)
  const routeStore = store === undefined ? await getOptionalSubdomainRouteStore() : store

  if (subdomain && routeStore) {
    try {
      const route = await routeStore.findBySubdomain(subdomain)
      if (route?.enabled) return normalizeBackendBaseUrl(route.apiUrl)
    } catch (error) {
      console.error('Failed to resolve subdomain route from persistent storage:', error)
    }
  }

  return getBackendBaseUrl()
}

export function getBackendUrl(path: string): string {
  const normalizedPath = path.replace(/^\/+/, '')
  return new URL(normalizedPath, `${getBackendBaseUrl()}/`).toString()
}
