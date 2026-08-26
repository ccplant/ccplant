const SUBDOMAIN_PATTERN = /^[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?$/
const BLOCKED_HOSTNAMES = new Set(['localhost', 'localhost.localdomain'])

export function normalizeSubdomain(value: string): string {
  const subdomain = value.trim().toLowerCase()
  if (!SUBDOMAIN_PATTERN.test(subdomain)) {
    throw new Error('Subdomain must contain only lowercase letters, numbers, and hyphens')
  }
  return subdomain
}

function isPrivateIpv4(hostname: string): boolean {
  const parts = hostname.split('.').map(Number)
  if (parts.length !== 4 || parts.some((part) => !Number.isInteger(part) || part < 0 || part > 255)) {
    return false
  }
  return parts[0] === 10
    || parts[0] === 127
    || (parts[0] === 169 && parts[1] === 254)
    || (parts[0] === 172 && parts[1] >= 16 && parts[1] <= 31)
    || (parts[0] === 192 && parts[1] === 168)
}

export function normalizeUserApiUrl(value: string): string {
  let url: URL
  try {
    url = new URL(value.trim())
  } catch {
    throw new Error('API URL must be a valid HTTPS URL')
  }

  const hostname = url.hostname.toLowerCase()
  if (
    url.protocol !== 'https:'
    || url.username
    || url.password
    || url.search
    || url.hash
    || BLOCKED_HOSTNAMES.has(hostname)
    || hostname.endsWith('.localhost')
    || hostname === '::1'
    || hostname === '[::1]'
    || hostname.startsWith('fc')
    || hostname.startsWith('fd')
    || hostname.startsWith('fe80:')
    || isPrivateIpv4(hostname)
  ) {
    throw new Error('API URL must be a public HTTPS URL without credentials, query, or fragment')
  }

  url.pathname = url.pathname.replace(/\/+$/, '')
  return url.toString().replace(/\/$/, '')
}
