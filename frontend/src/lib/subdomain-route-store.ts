export interface SubdomainRoute {
  subdomain: string
  apiUrl: string
  enabled: boolean
}

export interface SubdomainRouteStore {
  findBySubdomain(subdomain: string): Promise<SubdomainRoute | null>
}

interface D1Row {
  subdomain: string
  api_url: string
  enabled: number
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

export class D1SubdomainRouteStore implements SubdomainRouteStore {
  constructor(private readonly database: D1DatabaseLike) {}

  async findBySubdomain(subdomain: string): Promise<SubdomainRoute | null> {
    const row = await this.database.prepare(
      'SELECT subdomain, api_url, enabled FROM api_routes WHERE subdomain = ?1',
    ).bind(subdomain).first<D1Row>()
    return fromRow(row)
  }

}

export async function getOptionalSubdomainRouteStore(): Promise<SubdomainRouteStore | null> {
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
