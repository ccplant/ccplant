'use client'

import { useEffect, useState } from 'react'
import { useRouter } from 'next/navigation'

import { useConfig } from '@/hooks/useConfig'
import { getRedirectUri } from '@/lib/oauth-utils'
import type { GitHubConnection } from '@/types/github-connection'
import type { GoogleConnection } from '@/types/google-connection'

async function loadConnections<T>(url: string): Promise<T[]> {
  const response = await fetch(url, { cache: 'no-store' })
  if (!response.ok) throw new Error(`Failed to load ${url}`)
  const data = await response.json()
  return Array.isArray(data.connections) ? data.connections : []
}

export function useLoginPage() {
  const router = useRouter()
  const { config, isLoading: isConfigLoading } = useConfig()
  const [apiKey, setApiKey] = useState('')
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState('')
  const [isGitHubLoading, setIsGitHubLoading] = useState(false)
  const [loadingConnectionId, setLoadingConnectionId] = useState<string | null>(null)
  const [loginConnections, setLoginConnections] = useState<GitHubConnection[]>([])
  const [googleConnections, setGoogleConnections] = useState<GoogleConnection[]>([])
  const [checkingAuth, setCheckingAuth] = useState(true)

  useEffect(() => {
    const check = async () => {
      try {
        const response = await fetch('/api/auth/status')
        const data = await response.json()
        if (data.authenticated) router.push('/')
        else setCheckingAuth(false)
      } catch (statusError) {
        console.error('Failed to check auth status:', statusError)
        setCheckingAuth(false)
      }
    }
    void check()
  }, [router])

  useEffect(() => {
    loadConnections<GitHubConnection>('/api/auth/github/connections')
      .then(setLoginConnections).catch(() => setLoginConnections([]))
    loadConnections<GoogleConnection>('/api/auth/google/connections')
      .then(setGoogleConnections).catch(() => setGoogleConnections([]))
  }, [])

  const handleLogin = async (event: React.FormEvent) => {
    event.preventDefault()
    setLoading(true)
    setError('')
    try {
      const response = await fetch('/api/auth/login', {
        method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ apiKey }),
      })
      const data = await response.json()
      if (response.ok) router.push('/')
      else setError(data.error || 'Invalid API key')
    } catch {
      setError('An error occurred. Please try again.')
    } finally {
      setLoading(false)
    }
  }

  const handleGitHubLogin = async (connectionId?: string) => {
    if (connectionId) setLoadingConnectionId(connectionId)
    else setIsGitHubLoading(true)
    setError('')
    try {
      const response = await fetch('/api/auth/github/authorize', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ redirect_uri: getRedirectUri(), ...(connectionId ? { connection_id: connectionId } : {}) }),
      })
      const data = await response.json()
      if (!response.ok) throw new Error(data.error || 'GitHub認証の開始に失敗しました')
      window.location.href = data.auth_url
    } catch (loginError) {
      setError(loginError instanceof Error ? loginError.message : '予期しないエラーが発生しました')
      setIsGitHubLoading(false)
      setLoadingConnectionId(null)
    }
  }

  const handleGoogleLogin = async (connectionId: string) => {
    setLoadingConnectionId(connectionId)
    setError('')
    try {
      const response = await fetch('/api/auth/google/authorize', {
        method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ connection_id: connectionId }),
      })
      const data = await response.json()
      if (!response.ok) throw new Error(data.error || 'Google認証の開始に失敗しました')
      window.location.href = data.auth_url
    } catch (loginError) {
      setError(loginError instanceof Error ? loginError.message : '予期しないエラーが発生しました')
      setLoadingConnectionId(null)
    }
  }

  return {
    apiKey, checkingAuth, error, googleConnections, handleGitHubLogin, handleGoogleLogin,
    handleLogin, isConfigLoading, isGitHubLoading, loading, loadingConnectionId,
    loginConnections, loginDescription: config?.loginDescription ?? 'Enter an access token or sign in with GitHub to continue.',
    loginSubDescription: config?.loginSubDescription ?? '', loginTitle: config?.loginTitle ?? 'ccplant', setApiKey,
  }
}

export type LoginPageModel = ReturnType<typeof useLoginPage>
