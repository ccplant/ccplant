import { NextRequest, NextResponse } from 'next/server'
import { getRequestBackendBaseUrl } from '@/lib/server-backend-url'
import { getPublicBaseUrl } from '@/lib/public-url'

export async function POST(request: NextRequest) {
  try {
    const body = await request.json()
    let { redirect_uri } = body
    const { connection_id } = body

    if (!redirect_uri) {
      return NextResponse.json(
        { error: 'redirect_uri is required' },
        { status: 400 }
      )
    }

    let redirectOrigin: string
    try {
      redirectOrigin = new URL(redirect_uri).origin
    } catch {
      return NextResponse.json(
        { error: 'redirect_uri must be an absolute URL' },
        { status: 400 }
      )
    }
    if (connection_id) {
      const publicBaseUrl = getPublicBaseUrl(request)
      redirect_uri = new URL('/api/proxy/auth/github-connections/callback', publicBaseUrl).toString()
      redirectOrigin = new URL(publicBaseUrl).origin
    }

    const backendBaseUrl = await getRequestBackendBaseUrl(request.nextUrl.hostname)
    const response = await fetch(`${backendBaseUrl}${connection_id ? '/github-connections/login' : '/oauth/authorize'}`, {
      method: 'POST',
      headers: {
        'Content-Type': 'application/json',
        ...(connection_id ? { Origin: redirectOrigin } : {}),
      },
      body: JSON.stringify({
        ...(connection_id ? { connection_id, callback_url: redirect_uri } : { redirect_uri }),
      }),
    })

    if (!response.ok) {
      console.error(`OAuth authorize request failed with status ${response.status}`)
      return NextResponse.json(
        { error: 'Failed to start OAuth flow' },
        { status: response.status }
      )
    }

    const data = await response.json()
    
    const result = NextResponse.json({
      auth_url: data.auth_url,
      state: data.state
    })
    result.cookies.set('oauth_state', data.state, {
      httpOnly: true,
      secure: process.env.NODE_ENV === 'production',
      sameSite: 'lax',
      maxAge: 900,
      path: '/',
    })
    result.cookies.set('oauth_connection_login', connection_id ? '1' : '', {
      httpOnly: true,
      secure: process.env.NODE_ENV === 'production',
      sameSite: 'lax',
      maxAge: connection_id ? 900 : 0,
      path: '/',
    })
    return result
  } catch {
    console.error('OAuth authorize request failed')
    return NextResponse.json(
      { error: 'Internal server error' },
      { status: 500 }
    )
  }
}
