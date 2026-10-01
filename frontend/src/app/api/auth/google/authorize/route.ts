import { NextRequest, NextResponse } from 'next/server'
import { getPublicBaseUrl } from '@/lib/public-url'
import { getRequestBackendBaseUrl } from '@/lib/server-backend-url'

export async function POST(request: NextRequest) {
  try {
    const { connection_id } = await request.json()
    if (typeof connection_id !== 'string' || !connection_id) return NextResponse.json({ error: 'connection_id is required' }, { status: 400 })
    const backendBaseUrl = await getRequestBackendBaseUrl(request.nextUrl.hostname)
    const callbackUrl = new URL('/api/v1/auth/google-connections/callback', getPublicBaseUrl(request)).toString()
    const response = await fetch(`${backendBaseUrl}/google-connections/login`, {
      method: 'POST', headers: { 'Content-Type': 'application/json', Origin: request.nextUrl.origin },
      body: JSON.stringify({ connection_id, callback_url: callbackUrl }), cache: 'no-store',
    })
    const data = await response.json()
    if (!response.ok) return NextResponse.json({ error: data.message || 'Google認証の開始に失敗しました' }, { status: response.status })
    const result = NextResponse.json({ auth_url: data.auth_url, state: data.state })
    result.cookies.set('google_oauth_state', data.state, { httpOnly: true, secure: process.env.NODE_ENV === 'production', sameSite: 'lax', maxAge: 600, path: '/' })
    return result
  } catch {
    return NextResponse.json({ error: 'Internal server error' }, { status: 500 })
  }
}
