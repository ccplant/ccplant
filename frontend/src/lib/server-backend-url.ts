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

const SUBDOMAIN_MAP_ENV = 'AGENTAPI_PROXY_SUBDOMAIN_MAP'

function getSubdomain(hostname: string): string | undefined {
  const normalizedHostname = hostname.trim().toLowerCase().replace(/\.$/, '')
  if (!normalizedHostname || normalizedHostname === 'localhost') return undefined

  const [subdomain, ...rest] = normalizedHostname.split('.')
  return rest.length > 0 ? subdomain : undefined
}

export function resolveMappedBackendUrl(
  hostname: string,
  configuredMap = process.env.AGENTAPI_PROXY_SUBDOMAIN_MAP,
): string | undefined {
  if (!configuredMap?.trim()) return undefined

  let mapping: unknown
  try {
    mapping = JSON.parse(configuredMap)
  } catch {
    throw new Error(`${SUBDOMAIN_MAP_ENV} must be a valid JSON object`)
  }

  if (!mapping || Array.isArray(mapping) || typeof mapping !== 'object') {
    throw new Error(`${SUBDOMAIN_MAP_ENV} must be a JSON object`)
  }

  const subdomain = getSubdomain(hostname)
  if (!subdomain) return undefined

  const configuredUrl = (mapping as Record<string, unknown>)[subdomain]
  if (configuredUrl === undefined) return undefined
  if (typeof configuredUrl !== 'string') {
    throw new Error(`${SUBDOMAIN_MAP_ENV}.${subdomain} must be a URL string`)
  }

  return normalizeBackendBaseUrl(configuredUrl)
}

export function getBackendBaseUrl(hostname?: string): string {
  const mappedUrl = hostname ? resolveMappedBackendUrl(hostname) : undefined
  if (mappedUrl) return mappedUrl

  const configuredUrl = process.env.AGENTAPI_PROXY_URL || process.env.AGENTAPI_PROXY_ENDPOINT
  if (!configuredUrl) {
    throw new Error('AGENTAPI_PROXY_URL is required')
  }
  return normalizeBackendBaseUrl(configuredUrl)
}

export function getBackendUrl(path: string, hostname?: string): string {
  const normalizedPath = path.replace(/^\/+/, '')
  return new URL(normalizedPath, `${getBackendBaseUrl(hostname)}/`).toString()
}
