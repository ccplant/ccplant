'use client'

import { useEffect, useState } from 'react'
import Link from 'next/link'
import { useRouter } from 'next/navigation'
import { ArrowLeft, Plus, Users } from 'lucide-react'
import { AgentAPIProxyError, createAgentAPIProxyClientFromStorage } from '@/lib/agentapi-proxy-client'
import { ItemList, ItemListEmpty, ItemListRow, SettingsPageHeader } from '@/components/settings'
import { DEFAULT_SETTINGS_SLUG, settingsHref } from '../navConfig'

export default function TeamSettingsIndexPage() {
  const router = useRouter()
  const [teams, setTeams] = useState<string[]>([])
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)
  const [isAdmin, setIsAdmin] = useState(false)
  const [showCreate, setShowCreate] = useState(false)
  const [newTeamId, setNewTeamId] = useState('')
  const [creating, setCreating] = useState(false)

  useEffect(() => {
    let cancelled = false
    const loadTeams = async () => {
      try {
        const client = createAgentAPIProxyClientFromStorage()
        const info = await client.getUserInfo()
        if (cancelled) return
        const admin = info?.is_admin === true
        setIsAdmin(admin)
        let list = info?.teams ?? []
        if (admin) {
          const configs = await client.listTeamConfigs()
          if (cancelled) return
          list = configs.map((team) => team.team_id)
        }
        setTeams(list)
        setLoading(false)
      } catch (err) {
        if (cancelled) return
        console.error('Failed to load teams:', err)
        setError('チーム情報の取得に失敗しました')
        setLoading(false)
      }
    }
    loadTeams()
    return () => {
      cancelled = true
    }
  }, [router])

  const createTeam = async (event: React.FormEvent) => {
    event.preventDefault()
    const teamId = newTeamId.trim().toLowerCase()
    if (!teamId) return
    setCreating(true)
    setError(null)
    try {
      const created = await createAgentAPIProxyClientFromStorage().createTeam(teamId)
      router.push(settingsHref('team', 'github-teams', created.team_id))
    } catch (err) {
      if (err instanceof AgentAPIProxyError && err.status === 409) {
        setError('同じIDのチームがすでに存在します')
      } else {
        setError('チームを作成できませんでした')
      }
    } finally {
      setCreating(false)
    }
  }

  if (loading) {
    return (
      <div className="flex items-center justify-center py-16">
        <div className="h-8 w-8 animate-spin rounded-full border-b-2 border-blue-600" />
      </div>
    )
  }

  return (
    <div className="mx-auto max-w-2xl">
      <Link
        href="/settings/personal/ai-providers"
        className="mb-4 inline-flex items-center gap-1.5 text-sm text-gray-600 transition-colors hover:text-gray-900 dark:text-gray-400 dark:hover:text-white"
      >
        <ArrowLeft className="h-4 w-4" />
        パーソナル設定に戻る
      </Link>

      <SettingsPageHeader
        title="チームを選択"
        description="設定を表示するチームを選んでください。"
        action={isAdmin ? (
          <button
            type="button"
            onClick={() => setShowCreate((current) => !current)}
            className="inline-flex items-center gap-1.5 rounded-md bg-blue-600 px-3 py-2 text-sm font-medium text-white hover:bg-blue-700"
          >
            <Plus className="h-4 w-4" />
            新規作成
          </button>
        ) : undefined}
      />

      {showCreate && (
        <form onSubmit={createTeam} className="mb-5 rounded-lg border border-gray-200 p-4 dark:border-gray-700">
          <label htmlFor="new-team-id" className="block text-sm font-medium text-gray-900 dark:text-white">
            チームID
          </label>
          <p className="mt-1 text-xs text-gray-500">例: platform または organization/platform</p>
          <div className="mt-3 flex gap-2">
            <input
              id="new-team-id"
              value={newTeamId}
              onChange={(event) => setNewTeamId(event.target.value)}
              placeholder="organization/team"
              autoComplete="off"
              className="min-w-0 flex-1 rounded-md border border-gray-300 bg-white px-3 py-2 text-sm dark:border-gray-600 dark:bg-gray-900"
            />
            <button
              type="submit"
              disabled={creating || !newTeamId.trim()}
              className="rounded-md bg-blue-600 px-4 py-2 text-sm font-medium text-white disabled:opacity-50"
            >
              {creating ? '作成中...' : '作成'}
            </button>
          </div>
        </form>
      )}

      {error && (
        <div className="mb-5 rounded-lg border border-red-200 bg-red-50 p-4 dark:border-red-800 dark:bg-red-900/20">
          <p className="text-sm text-red-600 dark:text-red-400">{error}</p>
        </div>
      )}

      <ItemList>
        {teams.length === 0 && !error && (
          <ItemListEmpty>所属しているチームがありません</ItemListEmpty>
        )}
        {teams.map((team) => (
          <ItemListRow
            key={team}
            name={
              <span className="flex items-center gap-2">
                <Users className="h-4 w-4 text-gray-500 dark:text-gray-400" />
                {team}
              </span>
            }
            actions={
              <Link
                href={settingsHref('team', DEFAULT_SETTINGS_SLUG, team)}
                className="rounded-md border border-gray-300 px-3 py-1 text-xs text-gray-600 transition-colors hover:bg-gray-100 dark:border-gray-600 dark:text-gray-300 dark:hover:bg-gray-700"
              >
                設定を開く
              </Link>
            }
          />
        ))}
      </ItemList>
    </div>
  )
}
