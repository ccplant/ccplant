import { NextRequest, NextResponse } from 'next/server'
import { getAuthenticatedRouteOwner } from '@/lib/subdomain-route-auth'
import { getOptionalSubdomainRouteStore } from '@/lib/subdomain-route-store'
import { normalizeSubdomain, normalizeUserApiUrl } from '@/lib/subdomain-route-validation'

async function resolveContext() {
  const ownerId = await getAuthenticatedRouteOwner()
  if (!ownerId) return { response: NextResponse.json({ error: 'Not authenticated' }, { status: 401 }) }

  const store = await getOptionalSubdomainRouteStore()
  if (!store) {
    return {
      response: NextResponse.json(
        { error: 'Persistent subdomain route storage is not configured' },
        { status: 503 },
      ),
    }
  }
  return { ownerId, store }
}

export async function GET() {
  const context = await resolveContext()
  if ('response' in context) return context.response
  try {
    return NextResponse.json({ route: await context.store.findByOwner(context.ownerId) })
  } catch {
    return NextResponse.json({ error: 'Route storage is unavailable' }, { status: 503 })
  }
}

export async function PUT(request: NextRequest) {
  const context = await resolveContext()
  if ('response' in context) return context.response

  let body: { subdomain?: unknown; apiUrl?: unknown; enabled?: unknown }
  let subdomain: string
  let apiUrl: string
  try {
    body = await request.json() as typeof body
    if (typeof body.subdomain !== 'string' || typeof body.apiUrl !== 'string') {
      return NextResponse.json({ error: 'subdomain and apiUrl are required' }, { status: 400 })
    }

    subdomain = normalizeSubdomain(body.subdomain)
    apiUrl = normalizeUserApiUrl(body.apiUrl)
  } catch (error) {
    const message = error instanceof Error ? error.message : 'Invalid request'
    return NextResponse.json({ error: message }, { status: 400 })
  }

  try {
    const existing = await context.store.findBySubdomain(subdomain)
    if (existing && existing.ownerId !== context.ownerId) {
      return NextResponse.json({ error: 'Subdomain is already in use' }, { status: 409 })
    }

    const route = { subdomain, ownerId: context.ownerId, apiUrl, enabled: body.enabled !== false }
    await context.store.upsert(route)
    return NextResponse.json({ route })
  } catch {
    return NextResponse.json({ error: 'Route storage is unavailable' }, { status: 503 })
  }
}

export async function DELETE() {
  const context = await resolveContext()
  if ('response' in context) return context.response
  try {
    await context.store.deleteByOwner(context.ownerId)
    return new NextResponse(null, { status: 204 })
  } catch {
    return NextResponse.json({ error: 'Route storage is unavailable' }, { status: 503 })
  }
}
