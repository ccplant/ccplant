import { NextRequest, NextResponse } from 'next/server'
import { getAuthSessionFromCookie } from '@/lib/cookie-auth'

const blockedHosts = new Set(['localhost', 'metadata.google.internal', '169.254.169.254'])

function parseSourceURL(value: unknown): URL {
  if (typeof value !== 'string' || !value.trim()) throw new Error('API URL を入力してください')
  const url = new URL(value.trim())
  if (url.protocol !== 'https:' && !(process.env.NODE_ENV !== 'production' && url.protocol === 'http:')) {
    throw new Error('API URL には https:// を指定してください')
  }
  const host = url.hostname.toLowerCase()
  if (blockedHosts.has(host) || host.endsWith('.localhost') || /^127\./.test(host) || /^10\./.test(host) || /^192\.168\./.test(host) || /^172\.(1[6-9]|2\d|3[01])\./.test(host)) {
    throw new Error('ローカルまたはプライベートネットワークの URL は指定できません')
  }
  url.search = ''
  url.hash = ''
  url.pathname = url.pathname.replace(/\/$/, '')
  return url
}

async function sourceFetch(base: URL, path: string, token: string) {
  const url = new URL(`${base.pathname}/${path}`.replace(/\/{2,}/g, '/'), base.origin)
  const request = (header: 'Authorization' | 'X-API-Key') => fetch(url, {
    headers: header === 'Authorization' ? { Authorization: `Bearer ${token}` } : { 'X-API-Key': token },
    cache: 'no-store',
    signal: AbortSignal.timeout(15_000),
  })
  let response = await request('Authorization')
  if (response.status === 401 || response.status === 403) response = await request('X-API-Key')
  if (!response.ok) {
    const detail = await response.json().catch(() => null) as { message?: string } | null
    throw new Error(detail?.message || `移行元 API が ${response.status} を返しました`)
  }
  return response.json()
}

async function sourceCollection(base: URL, path: string, token: string, key: 'webhooks' | 'slackbots') {
  const items: unknown[] = []
  for (let page = 1; page <= 100; page += 1) {
    const data = await sourceFetch(base, `${path}&limit=100&page=${page}`, token) as Record<string, unknown> | unknown[]
    const batch = Array.isArray(data) ? data : Array.isArray(data[key]) ? data[key] as unknown[] : []
    items.push(...batch)
    const total = !Array.isArray(data) && typeof data.total === 'number' ? data.total : undefined
    if (batch.length < 100 || (total !== undefined && items.length >= total)) break
  }
  return { [key]: items, total: items.length }
}

export async function POST(request: NextRequest) {
  try {
    if (!await getAuthSessionFromCookie()) {
      return NextResponse.json({ message: '認証が必要です' }, { status: 401 })
    }
    const body = await request.json() as { api_url?: unknown; token?: unknown; team_id?: unknown }
    const base = parseSourceURL(body.api_url)
    const token = typeof body.token === 'string' ? body.token.trim() : ''
    const teamId = typeof body.team_id === 'string' ? body.team_id.trim() : ''
    if (!token) return NextResponse.json({ message: 'API token を入力してください' }, { status: 400 })
    if (!teamId) return NextResponse.json({ message: '移行元 Team ID を入力してください' }, { status: 400 })
    const query = `scope=team&team_id=${encodeURIComponent(teamId)}`
    const [memories, webhooks, slackbots, profiles, policies] = await Promise.all([
      sourceFetch(base, `memories?${query}`, token),
      sourceCollection(base, `webhooks?${query}`, token, 'webhooks'),
      sourceCollection(base, `slackbots?${query}`, token, 'slackbots'),
      sourceFetch(base, `session-profiles?${query}`, token),
      sourceFetch(base, `sandbox-policies?${query}`, token),
    ])
    return NextResponse.json({ memories, webhooks, slackbots, profiles, policies }, { headers: { 'Cache-Control': 'no-store' } })
  } catch (reason) {
    const message = reason instanceof Error ? reason.message : '移行元へ接続できませんでした'
    return NextResponse.json({ message }, { status: 400 })
  }
}
