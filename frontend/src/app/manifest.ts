import type { MetadataRoute } from 'next'
import { headers } from 'next/headers'
import { getRequestAppBranding } from '@/lib/server-app-branding'

/**
 * PWA マニフェストを動的に生成
 * 環境変数で以下の設定が可能:
 * - PWA_APP_NAME: アプリ名
 * - PWA_SHORT_NAME: 短縮名
 * - PWA_DESCRIPTION: 説明
 * - PWA_ICON_URL: カスタムアイコン URL (設定時はすべてのサイズでこの URL を使用)
 */
export default async function manifest(): Promise<MetadataRoute.Manifest> {
  const requestHeaders = await headers()
  const forwardedHost = requestHeaders.get('x-forwarded-host')?.split(',')[0]?.trim()
  const host = forwardedHost || requestHeaders.get('host') || ''
  const branding = await getRequestAppBranding(host.replace(/:\d+$/, ''))
  const appName = branding.appTitle

  const shortName = process.env.PWA_SHORT_NAME
    || process.env.NEXT_PUBLIC_PWA_SHORT_NAME
    || appName

  const description = process.env.PWA_DESCRIPTION
    || process.env.NEXT_PUBLIC_PWA_DESCRIPTION
    || 'Launch, connect, and manage AI agent sessions with ccplant.'

  // カスタムアイコン URL が設定されている場合はそれを使用
  const customIconUrl = branding.iconUrl

  // アイコン設定を生成
  const icons: MetadataRoute.Manifest['icons'] = customIconUrl
    ? [
        {
          src: customIconUrl,
          sizes: '192x192',
          type: 'image/png',
          purpose: 'maskable',
        },
        {
          src: customIconUrl,
          sizes: '192x192',
          type: 'image/png',
          purpose: 'any',
        },
        {
          src: customIconUrl,
          sizes: '512x512',
          type: 'image/png',
          purpose: 'maskable',
        },
        {
          src: customIconUrl,
          sizes: '512x512',
          type: 'image/png',
          purpose: 'any',
        },
      ]
    : [
        {
          src: '/icon-192x192.png',
          sizes: '192x192',
          type: 'image/png',
          purpose: 'maskable',
        },
        {
          src: '/icon-192x192.png',
          sizes: '192x192',
          type: 'image/png',
          purpose: 'any',
        },
        {
          src: '/icon-256x256.png',
          sizes: '256x256',
          type: 'image/png',
        },
        {
          src: '/icon-384x384.png',
          sizes: '384x384',
          type: 'image/png',
        },
        {
          src: '/icon-512x512.png',
          sizes: '512x512',
          type: 'image/png',
          purpose: 'maskable',
        },
        {
          src: '/icon-512x512.png',
          sizes: '512x512',
          type: 'image/png',
          purpose: 'any',
        },
      ]

  return {
    name: appName,
    short_name: shortName,
    description: description,
    theme_color: '#071A1D',
    background_color: '#071A1D',
    display: 'standalone',
    orientation: 'portrait',
    scope: '/',
    start_url: '/',
    icons,
  }
}
