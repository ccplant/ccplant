'use client'

import { Suspense, useEffect, useState } from 'react'
import { useParams, useRouter, useSearchParams } from 'next/navigation'
import Link from 'next/link'
import { useTeamScope } from '../../../contexts/TeamScopeContext'
import SessionProfileEditor from '../../components/SessionProfileEditor'
import { createAgentAPIProxyClientFromStorage } from '../../../lib/agentapi-proxy-client'
import type { SessionProfile } from '../../../types/session_profile'

function ProfilePage() {
  const { profileId } = useParams<{ profileId: string }>()
  const query = useSearchParams()
  const router = useRouter()
  const { setAvailableTeams } = useTeamScope()
  useEffect(() => {
    let cancelled = false
    createAgentAPIProxyClientFromStorage().getUserInfo()
      .then(info => { if (!cancelled) setAvailableTeams(info.teams ?? []) })
      .catch(() => { /* Existing team choices remain available when offline. */ })
    return () => { cancelled = true }
  }, [setAvailableTeams])
  const [profile, setProfile] = useState<SessionProfile | null>(null)
  const [error, setError] = useState('')
  const [loading, setLoading] = useState(true)
  useEffect(() => {
    let cancelled = false
    setError('')
    setProfile(null)
    setLoading(true)
    if (profileId === 'new') { setLoading(false); return }
    createAgentAPIProxyClientFromStorage().getSessionProfile(profileId)
      .then(value => { if (!cancelled) setProfile(value) })
      .catch(() => { if (!cancelled) setError('プロファイルを読み込めませんでした。アクセス権と接続を確認してください。') })
      .finally(() => { if (!cancelled) setLoading(false) })
    return () => { cancelled = true }
  }, [profileId])
  if (loading) return <p role="status" className="p-8">読み込み中...</p>
  if (error) return <div className="p-8"><p role="alert">{error}</p><Link href="/session-profiles" className="text-blue-600">プロファイル一覧に戻る</Link></div>
  return <SessionProfileEditor key={profileId} editingProfile={profile} section={query.get('section') || 'basic'}
    createScope={query.get('scope') === 'team' && query.get('team_id') ? { scope: 'team', team_id: query.get('team_id')! } : { scope: 'user' }}
    onClose={() => router.push('/session-profiles')} onSuccess={() => router.push('/session-profiles')} />
}

export default function Page() {
  return <Suspense fallback={<p className="p-8">読み込み中...</p>}><ProfilePage /></Suspense>
}
