import { NextRequest, NextResponse } from 'next/server'
import { AUTH_COOKIE_NAME, AUTH_COOKIE_OPTIONS, encryptOAuthSession } from '@/lib/cookie-auth'
import { getPublicBaseUrl, getPublicUrl } from '@/lib/public-url'
import { getRequestBackendBaseUrl } from '@/lib/server-backend-url'

function errorRedirect(request: NextRequest, error: string) {
  const url = getPublicUrl(request, '/login/github/error')
  url.searchParams.set('error', error)
  return NextResponse.redirect(url)
}

export async function GET(request: NextRequest) {
  const state = request.nextUrl.searchParams.get('state')
  const code = request.nextUrl.searchParams.get('code')
  if (!state || !code) return errorRedirect(request, 'missing_params')

  const connectionLogin = request.cookies.get('oauth_connection_login')?.value === '1'
  if (connectionLogin && request.cookies.get('oauth_state')?.value !== state) {
    return errorRedirect(request, 'invalid_state')
  }

  try {
    const backendBaseUrl = await getRequestBackendBaseUrl(request.nextUrl.hostname)
    const callback = new URL(`${backendBaseUrl}/auth/github-connections/callback`)
    callback.searchParams.set('state', state)
    callback.searchParams.set('code', code)
    const response = await fetch(callback, { redirect: 'manual', cache: 'no-store' })

    if (response.status >= 300 && response.status < 400) {
      const location = response.headers.get('location')
      if (!location) return errorRedirect(request, 'auth_failed')
      return NextResponse.redirect(new URL(location, getPublicBaseUrl(request)))
    }
    if (!response.ok) return errorRedirect(request, 'auth_failed')

    const data = await response.json()
    if (!connectionLogin || typeof data.session_id !== 'string' || typeof data.access_token !== 'string') {
      return errorRedirect(request, 'auth_failed')
    }
    const result = NextResponse.redirect(new URL('/chats', getPublicBaseUrl(request)))
    result.cookies.set(AUTH_COOKIE_NAME, encryptOAuthSession(data.session_id, data.access_token), AUTH_COOKIE_OPTIONS)
    for (const name of ['oauth_state', 'oauth_connection_login']) {
      result.cookies.set(name, '', { httpOnly: true, secure: true, sameSite: 'lax', maxAge: 0, path: '/' })
    }
    return result
  } catch {
    return errorRedirect(request, 'server_error')
  }
}
