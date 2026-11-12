'use client'

import { useState, useEffect, useCallback, useRef } from 'react'
import { SessionProfile } from '../../types/session_profile'
import { createAgentAPIProxyClientFromStorage } from '../../lib/agentapi-proxy-client'
import SessionProfileCard from './SessionProfileCard'
import { useTeamScope } from '../../contexts/TeamScopeContext'

interface SessionProfileListViewProps {
  onProfileEdit: (profile: SessionProfile) => void
  onProfilesUpdate?: () => void
}

export default function SessionProfileListView({
  onProfileEdit,
  onProfilesUpdate,
}: SessionProfileListViewProps) {
  const { getScopeParams, isTeamScope } = useTeamScope()
  const [profiles, setProfiles] = useState<SessionProfile[]>([])
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)
  const [deletingIds, setDeletingIds] = useState<Set<string>>(new Set())
  const [savingTeamProfileIds, setSavingTeamProfileIds] = useState<Set<string>>(new Set())
  const fetchIdRef = useRef(0)

  const fetchProfiles = useCallback(async () => {
    const fetchId = ++fetchIdRef.current
    try {
      setLoading(true)
      setError(null)

      const scopeParams = getScopeParams()

      const client = createAgentAPIProxyClientFromStorage()
      const response = await client.getSessionProfiles({
        ...scopeParams,
        include_available_team_profiles: !isTeamScope,
      })

      if (fetchId !== fetchIdRef.current) return
      setProfiles(response.session_profiles || [])
    } catch (err) {
      if (fetchId !== fetchIdRef.current) return
      console.error('Failed to fetch session profiles:', err)
      setError(err instanceof Error ? err.message : 'セッションプロファイルの取得に失敗しました')
    } finally {
      if (fetchId === fetchIdRef.current) {
        setLoading(false)
      }
    }
  }, [getScopeParams, isTeamScope])

  // Refetch profiles when team scope changes
  useEffect(() => {
    fetchProfiles()
  }, [fetchProfiles])

  const handleDelete = async (profileId: string) => {
    setDeletingIds((prev) => new Set(prev).add(profileId))
    try {
      const client = createAgentAPIProxyClientFromStorage()
      await client.deleteSessionProfile(profileId)
      setProfiles((prev) => prev.filter((p) => p.id !== profileId))
      onProfilesUpdate?.()
    } catch (err) {
      console.error('Failed to delete session profile:', err)
      alert(err instanceof Error ? err.message : 'セッションプロファイルの削除に失敗しました')
    } finally {
      setDeletingIds((prev) => {
        const next = new Set(prev)
        next.delete(profileId)
        return next
      })
    }
  }

  const handleTeamProfileToggle = async (profile: SessionProfile) => {
    setSavingTeamProfileIds((prev) => new Set(prev).add(profile.id))
    try {
      const client = createAgentAPIProxyClientFromStorage()
      const user = await client.getUserInfo()
      const settings = await client.getSettings(user.principal_id)
      const current = new Set(settings.enabled_team_session_profile_ids || [])
      if (profile.personal_enabled) current.delete(profile.id)
      else current.add(profile.id)
      await client.saveSettings(user.principal_id, {
        enabled_team_session_profile_ids: Array.from(current),
      })
      setProfiles((prev) => prev.map((item) => item.id === profile.id
        ? { ...item, personal_enabled: !profile.personal_enabled }
        : item))
      onProfilesUpdate?.()
    } catch (err) {
      console.error('Failed to update personal team profile selection:', err)
      alert(err instanceof Error ? err.message : 'チームプロファイルの個人利用設定に失敗しました')
    } finally {
      setSavingTeamProfileIds((prev) => {
        const next = new Set(prev)
        next.delete(profile.id)
        return next
      })
    }
  }

  if (loading) {
    return (
      <div className="flex items-center justify-center py-16">
        <div className="flex items-center gap-3 text-gray-500 dark:text-gray-400">
          <svg className="w-5 h-5 animate-spin" fill="none" stroke="currentColor" viewBox="0 0 24 24">
            <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M4 4v5h.582m15.356 2A8.001 8.001 0 004.582 9m0 0H9m11 11v-5h-.581m0 0a8.003 8.003 0 01-15.357-2m15.357 2H15" />
          </svg>
          <span className="text-sm">セッションプロファイルを読み込み中...</span>
        </div>
      </div>
    )
  }

  if (error) {
    return (
      <div className="flex items-center justify-center py-16">
        <div className="text-center">
          <div className="w-12 h-12 mx-auto mb-4 rounded-full bg-red-100 dark:bg-red-900/30 flex items-center justify-center">
            <svg className="w-6 h-6 text-red-600 dark:text-red-400" fill="none" stroke="currentColor" viewBox="0 0 24 24">
              <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M12 8v4m0 4h.01M21 12a9 9 0 11-18 0 9 9 0 0118 0z" />
            </svg>
          </div>
          <p className="text-sm text-red-600 dark:text-red-400 mb-3">{error}</p>
          <button
            onClick={fetchProfiles}
            className="text-sm text-blue-600 dark:text-blue-400 hover:underline"
          >
            再試行
          </button>
        </div>
      </div>
    )
  }

  if (profiles.length === 0) {
    return (
      <div className="flex items-center justify-center py-16">
        <div className="text-center">
          <div className="w-12 h-12 mx-auto mb-4 rounded-full bg-gray-100 dark:bg-gray-700 flex items-center justify-center">
            <svg className="w-6 h-6 text-gray-400 dark:text-gray-500" fill="none" stroke="currentColor" viewBox="0 0 24 24">
              <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M9 5H7a2 2 0 00-2 2v12a2 2 0 002 2h10a2 2 0 002-2V7a2 2 0 00-2-2h-2M9 5a2 2 0 002 2h2a2 2 0 002-2M9 5a2 2 0 012-2h2a2 2 0 012 2" />
            </svg>
          </div>
          <p className="text-sm font-medium text-gray-700 dark:text-gray-300 mb-1">
            セッションプロファイルがまだありません
          </p>
          <p className="text-xs text-gray-500 dark:text-gray-400">
            右上の「新しいプロファイル」ボタンから作成してください
          </p>
        </div>
      </div>
    )
  }

  const personalProfiles = isTeamScope ? profiles : profiles.filter((profile) => profile.scope !== 'team')
  const teamProfiles = isTeamScope ? [] : profiles.filter((profile) => profile.scope === 'team')

  return (
    <div className="space-y-8">
      {personalProfiles.length > 0 && (
        <div className="grid grid-cols-1 md:grid-cols-2 xl:grid-cols-3 gap-4">
          {personalProfiles.map((profile) => (
            <SessionProfileCard
              key={profile.id}
              profile={profile}
              onEdit={onProfileEdit}
              onDelete={handleDelete}
              isDeleting={deletingIds.has(profile.id)}
            />
          ))}
        </div>
      )}
      {teamProfiles.length > 0 && (
        <section>
          <div className="mb-3">
            <h2 className="text-base font-semibold text-gray-900 dark:text-white">個人スコープで使うチームプロファイル</h2>
            <p className="mt-1 text-sm text-gray-500 dark:text-gray-400">有効にすると、個人セッションの選択肢と自動選択の候補に追加されます。</p>
          </div>
          <div className="divide-y divide-gray-200 overflow-hidden rounded-lg border border-gray-200 bg-white dark:divide-gray-700 dark:border-gray-700 dark:bg-gray-800">
            {teamProfiles.map((profile) => (
              <label key={profile.id} className="flex cursor-pointer items-center justify-between gap-4 px-4 py-3">
                <span className="min-w-0">
                  <span className="block truncate text-sm font-medium text-gray-900 dark:text-white">{profile.name}</span>
                  <span className="block truncate text-xs text-gray-500 dark:text-gray-400">{profile.team_id}{profile.description ? ` · ${profile.description}` : ''}</span>
                </span>
                <input
                  type="checkbox"
                  checked={Boolean(profile.personal_enabled)}
                  disabled={savingTeamProfileIds.has(profile.id)}
                  onChange={() => handleTeamProfileToggle(profile)}
                  className="h-4 w-4 rounded border-gray-300 text-blue-600 focus:ring-blue-500 disabled:opacity-50"
                />
              </label>
            ))}
          </div>
        </section>
      )}
    </div>
  )
}
