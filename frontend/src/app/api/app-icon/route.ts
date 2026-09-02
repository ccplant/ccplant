import { NextRequest, NextResponse } from 'next/server'
import { getRequestAppBranding } from '@/lib/server-app-branding'

export async function GET(request: NextRequest) {
  const branding = await getRequestAppBranding(request.nextUrl.hostname)
  if (!branding.icon || !branding.iconContentType) {
    return new NextResponse(null, { status: 404 })
  }

  return new NextResponse(branding.icon, {
    headers: {
      'Content-Type': branding.iconContentType,
      'Cache-Control': 'public, max-age=300, must-revalidate',
      'X-Content-Type-Options': 'nosniff',
    },
  })
}
