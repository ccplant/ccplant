import type { Metadata, Viewport } from 'next'
import { Inter } from 'next/font/google'
import './globals.css'
import { ThemeProvider } from '../contexts/ThemeContext'
import { TeamScopeProvider } from '../contexts/TeamScopeContext'
import { ToastProvider } from '../contexts/ToastContext'
import { ToastContainer } from '../components/Toast'
import { Analytics } from '@vercel/analytics/react'
import { PushNotificationAutoInit } from './components/PushNotificationAutoInit'
import { DynamicFavicon } from './components/DynamicFavicon'

const inter = Inter({ subsets: ['latin'] })

export const metadata: Metadata = {
  title: 'ccplant',
  description: 'Launch, connect, and manage AI agent sessions with ccplant.',
  appleWebApp: {
    capable: true,
    statusBarStyle: 'default',
    title: 'ccplant',
  },
  formatDetection: {
    telephone: false,
  },
  openGraph: {
    type: 'website',
    siteName: 'ccplant',
    title: 'ccplant',
    description: 'Launch, connect, and manage AI agent sessions with ccplant.',
  },
  twitter: {
    card: 'summary',
    title: 'ccplant',
    description: 'Launch, connect, and manage AI agent sessions with ccplant.',
  },
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
