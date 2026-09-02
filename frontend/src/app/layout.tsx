import type { Metadata, Viewport } from 'next'
import { headers } from 'next/headers'
import { Inter } from 'next/font/google'
import './globals.css'
import { ThemeProvider } from '../contexts/ThemeContext'
import { TeamScopeProvider } from '../contexts/TeamScopeContext'
import { ToastProvider } from '../contexts/ToastContext'
import { ToastContainer } from '../components/Toast'
import { Analytics } from '@vercel/analytics/react'
import { PushNotificationAutoInit } from './components/PushNotificationAutoInit'
import { DynamicFavicon } from './components/DynamicFavicon'
import { getRequestAppBranding } from '@/lib/server-app-branding'

const inter = Inter({ subsets: ['latin'] })

export async function generateMetadata(): Promise<Metadata> {
  const requestHeaders = await headers()
  const forwardedHost = requestHeaders.get('x-forwarded-host')?.split(',')[0]?.trim()
  const host = forwardedHost || requestHeaders.get('host') || ''
  const hostname = host.replace(/:\d+$/, '')
  const branding = await getRequestAppBranding(hostname)
  const description = process.env.PWA_DESCRIPTION
    || process.env.NEXT_PUBLIC_PWA_DESCRIPTION
    || 'Launch, connect, and manage AI agent sessions with ccplant.'

  return {
    title: branding.appTitle,
    description,
    appleWebApp: {
      capable: true,
      statusBarStyle: 'default',
      title: branding.appTitle,
    },
    formatDetection: { telephone: false },
    openGraph: {
      type: 'website',
      siteName: branding.appTitle,
      title: branding.appTitle,
      description,
    },
    twitter: {
      card: 'summary',
      title: branding.appTitle,
      description,
    },
  }
}

export const viewport: Viewport = {
  width: 'device-width',
  initialScale: 1,
  themeColor: '#071A1D',
}

export default function RootLayout({
  children,
}: {
  children: React.ReactNode
}) {
  return (
    <html lang="en">
      <head>
        <link rel="icon" href="/favicon.ico" />
        <link rel="apple-touch-icon" href="/icon-192x192.png" />
        <link rel="manifest" href="/api/manifest" />
      </head>
      <body className={inter.className}>
        <ThemeProvider>
          <TeamScopeProvider>
            <ToastProvider>
              {children}
              <ToastContainer />
              <PushNotificationAutoInit />
              <DynamicFavicon />
              <Analytics />
            </ToastProvider>
          </TeamScopeProvider>
        </ThemeProvider>
      </body>
    </html>
  )
}
