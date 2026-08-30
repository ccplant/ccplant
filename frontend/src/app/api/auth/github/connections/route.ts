import { NextRequest, NextResponse } from 'next/server'
import { getRequestBackendBaseUrl } from '@/lib/server-backend-url'

export async function GET(request: NextRequest) {
  try {
    const backendBaseUrl = await getRequestBackendBaseUrl(request.nextUrl.hostname)
    const response = await fetch(`${backendBaseUrl}/github-connections/login-options`, {
      cache: 'no-store',
    })
    if (!response.ok) {
      return NextResponse.json({ connections: [] }, { status: response.status })
    }
    const data = await response.json()
    return NextResponse.json({
      connections: Array.isArray(data.connections) ? data.connections : [],
    })
  } catch {
    return NextResponse.json({ connections: [] }, { status: 502 })
  }
}
