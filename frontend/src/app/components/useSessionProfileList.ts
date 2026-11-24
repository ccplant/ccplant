import { useCallback, useEffect, useRef, useState } from 'react'
import { createAgentAPIProxyClientFromStorage } from '../../lib/agentapi-proxy-client'
import { useTeamScope } from '../../contexts/TeamScopeContext'
import { SessionProfile } from '../../types/session_profile'
import type { ProxyUserInfo } from '../../types/user'

export function useSessionProfileList(onProfilesUpdate?: () => void) {
  const { getScopeParams, isTeamScope } = useTeamScope()
  const [profiles, setProfiles] = useState<SessionProfile[]>([])
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)
  const [deletingIds, setDeletingIds] = useState<Set<string>>(new Set())
  const [savingTeamProfileIds, setSavingTeamProfileIds] = useState<Set<string>>(new Set())
  const [transferProfile, setTransferProfile] = useState<SessionProfile | null>(null)
  const [transferTeamId, setTransferTeamId] = useState('')
  const [transferringId, setTransferringId] = useState<string | null>(null)
  const [currentUser, setCurrentUser] = useState<ProxyUserInfo | null>(null)
  const fetchIdRef = useRef(0)

  const fetchProfiles = useCallback(async () => {
    const fetchId = ++fetchIdRef.current
    try {
      setLoading(true); setError(null)
      const client = createAgentAPIProxyClientFromStorage()
      const [response, user] = await Promise.all([
        client.getSessionProfiles({ ...getScopeParams(), include_available_team_profiles: !isTeamScope }),
        client.getUserInfo(),
      ])
      if (fetchId !== fetchIdRef.current) return
      setProfiles(response.session_profiles || []); setCurrentUser(user)
    } catch (err) {
      if (fetchId !== fetchIdRef.current) return
      console.error('Failed to fetch session profiles:', err)
      setError(err instanceof Error ? err.message : 'セッションプロファイルの取得に失敗しました')
    } finally {
      if (fetchId === fetchIdRef.current) setLoading(false)
    }
  }, [getScopeParams, isTeamScope])
  useEffect(() => { void fetchProfiles() }, [fetchProfiles])

  const deleteProfile = async (profileId: string) => {
    setDeletingIds((previous) => new Set(previous).add(profileId))
    try {
      await createAgentAPIProxyClientFromStorage().deleteSessionProfile(profileId)
      setProfiles((previous) => previous.filter((profile) => profile.id !== profileId)); onProfilesUpdate?.()
    } catch (err) {
      console.error('Failed to delete session profile:', err)
      alert(err instanceof Error ? err.message : 'セッションプロファイルの削除に失敗しました')
    } finally {
      setDeletingIds((previous) => { const next = new Set(previous); next.delete(profileId); return next })
    }
  }
  const openTransfer = (profile: SessionProfile) => { setTransferProfile(profile); setTransferTeamId('') }
  const transfer = async () => {
    if (!transferProfile) return
    const targetScope: 'user' | 'team' = transferProfile.scope === 'team' ? 'user' : 'team'
    if (targetScope === 'team' && !transferTeamId) return
    const request = { resource_type: 'session_profile' as const, resource_id: transferProfile.id, target_scope: targetScope, ...(targetScope === 'team' ? { target_team_id: transferTeamId } : {}) }
    setTransferringId(transferProfile.id)
    try {
      const client = createAgentAPIProxyClientFromStorage()
      await client.transferResource({ ...request, dry_run: true }); await client.transferResource(request)
      setTransferProfile(null); await fetchProfiles(); onProfilesUpdate?.()
    } catch (err) {
      console.error('Failed to transfer session profile:', err)
      alert(err instanceof Error ? err.message : 'セッションプロファイルの移管に失敗しました')
    } finally { setTransferringId(null) }
  }
  const toggleTeamProfile = async (profile: SessionProfile) => {
    setSavingTeamProfileIds((previous) => new Set(previous).add(profile.id))
    try {
      const client = createAgentAPIProxyClientFromStorage(); const user = await client.getUserInfo()
      const settings = await client.getSettings(user.principal_id)
      const enabled = new Set(settings.enabled_team_session_profile_ids || [])
      if (profile.personal_enabled) enabled.delete(profile.id); else enabled.add(profile.id)
      await client.saveSettings(user.principal_id, { enabled_team_session_profile_ids: Array.from(enabled) })
      setProfiles((previous) => previous.map((item) => item.id === profile.id ? { ...item, personal_enabled: !profile.personal_enabled } : item)); onProfilesUpdate?.()
    } catch (err) {
      console.error('Failed to update personal team profile selection:', err)
      alert(err instanceof Error ? err.message : 'チームプロファイルの個人利用設定に失敗しました')
    } finally {
      setSavingTeamProfileIds((previous) => { const next = new Set(previous); next.delete(profile.id); return next })
    }
  }
  return { profiles, loading, error, isTeamScope, deletingIds, savingTeamProfileIds, transferProfile, transferTeamId, transferringId, currentUser, fetchProfiles, deleteProfile, openTransfer, setTransferProfile, setTransferTeamId, transfer, toggleTeamProfile }
}
