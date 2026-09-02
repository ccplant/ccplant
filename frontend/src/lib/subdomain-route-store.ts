export interface SubdomainRoute {
  subdomain: string
  apiUrl: string
  enabled: boolean
}

export interface SubdomainRouteStore {
  findBySubdomain(subdomain: string): Promise<SubdomainRoute | null>
}

export interface SubdomainBranding {
  appTitle: string | null
  appIcon: ArrayBuffer | null
  appIconContentType: string | null
}

export interface SubdomainBrandingStore {
  findBrandingBySubdomain(subdomain: string): Promise<SubdomainBranding | null>
}

interface D1Row {
  subdomain: string
  api_url: string
  enabled: number
}

interface D1BrandingRow {
  app_title: string | null
  app_icon: ArrayBuffer | number[] | null
  app_icon_content_type: string | null
}

interface D1Statement {
  bind(...values: unknown[]): D1Statement
  first<T>(): Promise<T | null>
  run(): Promise<unknown>
}

export interface D1DatabaseLike {
  prepare(query: string): D1Statement
}

function fromRow(row: D1Row | null): SubdomainRoute | null {
  return row && {
    subdomain: row.subdomain,
    apiUrl: row.api_url,
    enabled: row.enabled === 1,
  }
}

export class D1SubdomainRouteStore implements SubdomainRouteStore, SubdomainBrandingStore {
  constructor(private readonly database: D1DatabaseLike) {}

  async findBySubdomain(subdomain: string): Promise<SubdomainRoute | null> {
    const row = await this.database.prepare(
      `SELECT subdomain, api_url, enabled
       FROM api_route_events
       WHERE subdomain = ?1
       ORDER BY id DESC
       LIMIT 1`,
    ).bind(subdomain).first<D1Row>()
    return fromRow(row)
  }

  async findBrandingBySubdomain(subdomain: string): Promise<SubdomainBranding | null> {
    const row = await this.database.prepare(
      `SELECT app_title, app_icon, app_icon_content_type
       FROM api_route_events
       WHERE subdomain = ?1
       ORDER BY id DESC
       LIMIT 1`,
    ).bind(subdomain).first<D1BrandingRow>()

    return row && {
      appTitle: row.app_title,
      appIcon: Array.isArray(row.app_icon)
        ? Uint8Array.from(row.app_icon).buffer
        : row.app_icon,
      appIconContentType: row.app_icon_content_type,
    }
  }

}

export async function getOptionalSubdomainRouteStore(): Promise<(SubdomainRouteStore & SubdomainBrandingStore) | null> {
  try {
    const { getCloudflareContext } = await import('@opennextjs/cloudflare')
    // OpenNext installs the context globally for each production Worker request.
    // Sync lookup avoids starting Wrangler in ordinary Node.js deployments.
    const { env } = getCloudflareContext()
    const database = (env as CloudflareEnv & { SUBDOMAIN_ROUTES_DB?: D1DatabaseLike }).SUBDOMAIN_ROUTES_DB
    return database ? new D1SubdomainRouteStore(database) : null
  } catch {
    // Next.js and non-Cloudflare deployments intentionally operate without D1.
    return null
  }
}
