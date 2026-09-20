'use client'

import { useEffect, useState } from 'react'
import Link from 'next/link'
import { useRouter } from 'next/navigation'
import { ArrowLeft, Pencil, Plus, Trash2, Users } from 'lucide-react'
import { AgentAPIProxyError, createAgentAPIProxyClientFromStorage } from '@/lib/agentapi-proxy-client'
import type { TeamConfig } from '@/types/team-config'
import { ItemList, ItemListEmpty, ItemListRow, SettingsPageHeader } from '@/components/settings'
import { DEFAULT_SETTINGS_SLUG, settingsHref } from '../navConfig'

export default function TeamSettingsIndexPage() {
  const router = useRouter()
  const [teams, setTeams] = useState<TeamConfig[]>([])
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)
  const [showCreate, setShowCreate] = useState(false)
  const [newTeamId, setNewTeamId] = useState('')
  const [creating, setCreating] = useState(false)
  const [editingTeam, setEditingTeam] = useState<TeamConfig | null>(null)
  const [editName, setEditName] = useState('')
  const [mutatingTeamId, setMutatingTeamId] = useState<string | null>(null)

  useEffect(() => {
    let cancelled = false
    const loadTeams = async () => {
      try {
        const client = createAgentAPIProxyClientFromStorage()
        const configs = await client.listTeamConfigs()
        if (cancelled) return
        setTeams(configs)
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
    const name = newTeamId.trim()
    if (!name) return
    setCreating(true)
    setError(null)
    try {
      const created = await createAgentAPIProxyClientFromStorage().createTeam(name)
      router.push(settingsHref('team', 'github-teams', created.team_id))
    } catch (err) {
      setError(err instanceof AgentAPIProxyError ? err.message : 'チームを作成できませんでした')
    } finally {
      setCreating(false)
    }
  }

  const renameTeam = async (event: React.FormEvent) => {
    event.preventDefault()
    if (!editingTeam || !editName.trim()) return
    setMutatingTeamId(editingTeam.team_id)
    setError(null)
    try {
      const updated = await createAgentAPIProxyClientFromStorage().renameTeam(editingTeam.team_id, editName.trim())
      setTeams((current) => current.map((team) => team.team_id === updated.team_id ? updated : team))
      setEditingTeam(null)
    } catch (err) {
      setError(err instanceof AgentAPIProxyError ? err.message : 'チーム名を変更できませんでした')
    } finally {
      setMutatingTeamId(null)
    }
  }

  const deleteTeam = async (team: TeamConfig) => {
    if (!confirm(`「${team.name}」を削除しますか？この操作は取り消せません。`)) return
    setMutatingTeamId(team.team_id)
    setError(null)
    try {
      await createAgentAPIProxyClientFromStorage().deleteTeam(team.team_id)
      setTeams((current) => current.filter((item) => item.team_id !== team.team_id))
    } catch (err) {
      setError(err instanceof AgentAPIProxyError ? err.message : 'チームを削除できませんでした')
    } finally {
      setMutatingTeamId(null)
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
        action={(
          <button
            type="button"
            onClick={() => setShowCreate((current) => !current)}
            className="inline-flex items-center gap-1.5 rounded-md bg-blue-600 px-3 py-2 text-sm font-medium text-white hover:bg-blue-700"
          >
            <Plus className="h-4 w-4" />
            新規作成
          </button>
        )}
      />

      {showCreate && (
        <form onSubmit={createTeam} className="mb-5 rounded-lg border border-gray-200 p-4 dark:border-gray-700">
          <label htmlFor="new-team-id" className="block text-sm font-medium text-gray-900 dark:text-white">
            チーム名
          </label>
          <p className="mt-1 text-xs text-gray-500">同じ名前のチームを複数作成できます</p>
          <div className="mt-3 flex gap-2">
            <input
              id="new-team-id"
              value={newTeamId}
              onChange={(event) => setNewTeamId(event.target.value)}
              placeholder="Platform Team"
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

      {editingTeam && (
        <form onSubmit={renameTeam} className="mb-5 rounded-lg border border-gray-200 p-4 dark:border-gray-700">
          <label htmlFor="edit-team-name" className="block text-sm font-medium text-gray-900 dark:text-white">チーム名を変更</label>
          <div className="mt-3 flex gap-2">
            <input
              id="edit-team-name"
              value={editName}
              onChange={(event) => setEditName(event.target.value)}
              autoFocus
              className="min-w-0 flex-1 rounded-md border border-gray-300 bg-white px-3 py-2 text-sm dark:border-gray-600 dark:bg-gray-900"
            />
            <button type="submit" disabled={!editName.trim() || mutatingTeamId === editingTeam.team_id} className="rounded-md bg-blue-600 px-4 py-2 text-sm font-medium text-white disabled:opacity-50">保存</button>
            <button type="button" onClick={() => setEditingTeam(null)} className="rounded-md border border-gray-300 px-4 py-2 text-sm dark:border-gray-600">キャンセル</button>
          </div>
        </form>
      )}

      <ItemList>
        {teams.length === 0 && !error && (
          <ItemListEmpty>所属しているチームがありません</ItemListEmpty>
        )}
        {teams.map((team) => (
          <ItemListRow
            key={team.team_id}
            name={
              <span className="flex items-center gap-2">
                <Users className="h-4 w-4 text-gray-500 dark:text-gray-400" />
                {team.name}
              </span>
            }
            actions={
              <div className="flex items-center gap-2">
                <button
                  type="button"
                  onClick={() => { setEditingTeam(team); setEditName(team.name) }}
                  disabled={mutatingTeamId === team.team_id}
                  className="rounded-md border border-gray-300 p-1.5 text-gray-600 hover:bg-gray-100 disabled:opacity-50 dark:border-gray-600 dark:text-gray-300 dark:hover:bg-gray-700"
                  aria-label={`${team.name}の名前を変更`}
                >
                  <Pencil className="h-3.5 w-3.5" />
                </button>
                <button
                  type="button"
                  onClick={() => deleteTeam(team)}
                  disabled={mutatingTeamId === team.team_id}
                  className="rounded-md border border-red-200 p-1.5 text-red-600 hover:bg-red-50 disabled:opacity-50 dark:border-red-800 dark:hover:bg-red-950/30"
                  aria-label={`${team.name}を削除`}
                >
                  <Trash2 className="h-3.5 w-3.5" />
                </button>
                <Link
                href={settingsHref('team', DEFAULT_SETTINGS_SLUG, team.team_id)}
                className="rounded-md border border-gray-300 px-3 py-1 text-xs text-gray-600 transition-colors hover:bg-gray-100 dark:border-gray-600 dark:text-gray-300 dark:hover:bg-gray-700"
                >
                  設定を開く
                </Link>
              </div>
            }
          />
        ))}
      </ItemList>
    </div>
  )
}
