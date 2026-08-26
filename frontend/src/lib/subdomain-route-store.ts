export interface SubdomainRoute {
  subdomain: string
  ownerId: string
  apiUrl: string
  enabled: boolean
}

export interface SubdomainRouteStore {
  findBySubdomain(subdomain: string): Promise<SubdomainRoute | null>
  findByOwner(ownerId: string): Promise<SubdomainRoute | null>
  upsert(route: SubdomainRoute): Promise<void>
  deleteByOwner(ownerId: string): Promise<void>
}

interface D1Row {
  subdomain: string
  owner_id: string
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
    ownerId: row.owner_id,
    apiUrl: row.api_url,
    enabled: row.enabled === 1,
  }
}

export class D1SubdomainRouteStore implements SubdomainRouteStore {
  constructor(private readonly database: D1DatabaseLike) {}

  async findBySubdomain(subdomain: string): Promise<SubdomainRoute | null> {
    const row = await this.database.prepare(
      'SELECT subdomain, owner_id, api_url, enabled FROM api_routes WHERE subdomain = ?1',
    ).bind(subdomain).first<D1Row>()
    return fromRow(row)
  }

  async findByOwner(ownerId: string): Promise<SubdomainRoute | null> {
    const row = await this.database.prepare(
      'SELECT subdomain, owner_id, api_url, enabled FROM api_routes WHERE owner_id = ?1',
    ).bind(ownerId).first<D1Row>()
    return fromRow(row)
  }

  async upsert(route: SubdomainRoute): Promise<void> {
    await this.database.prepare(`
      INSERT INTO api_routes (subdomain, owner_id, api_url, enabled, created_at, updated_at)
      VALUES (?1, ?2, ?3, ?4, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP)
      ON CONFLICT(owner_id) DO UPDATE SET
        subdomain = excluded.subdomain,
        api_url = excluded.api_url,
        enabled = excluded.enabled,
        updated_at = CURRENT_TIMESTAMP
    `).bind(route.subdomain, route.ownerId, route.apiUrl, route.enabled ? 1 : 0).run()
  }

  async deleteByOwner(ownerId: string): Promise<void> {
    await this.database.prepare('DELETE FROM api_routes WHERE owner_id = ?1').bind(ownerId).run()
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
