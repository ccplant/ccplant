'use client'

import { useCallback, useEffect, useState } from 'react'
import { Plus, RefreshCw, Trash2, Users } from 'lucide-react'
import { createAgentAPIProxyClientFromStorage } from '@/lib/agentapi-proxy-client'
import type { TeamConfig, TeamMembershipState, TeamMembershipSyncResult } from '@/types/team-config'
import { useSettingsScope } from '../../../SettingsScopeContext'

export default function GitHubTeamsPage() {
  const { scopeId } = useSettingsScope()
  const [config, setConfig] = useState<TeamConfig | null>(null)
  const [editable, setEditable] = useState<string[]>([])
  const [loading, setLoading] = useState(true)
  const [saving, setSaving] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const [members, setMembers] = useState<TeamMembershipState | null>(null)
  const [syncing, setSyncing] = useState(false)
  const [syncResult, setSyncResult] = useState<TeamMembershipSyncResult | null>(null)
  const [now, setNow] = useState(() => Date.now())

  const load = useCallback(async () => {
    if (!scopeId) return
    setLoading(true)
    try {
      const client = createAgentAPIProxyClientFromStorage()
      const [value, membership] = await Promise.all([client.getTeamConfig(scopeId), client.getTeamMembers(scopeId)])
      setConfig(value)
      setMembers(membership)
      setEditable(value.github_teams)
      setError(null)
    } catch {
      setError('GitHub チームマッピングを読み込めませんでした')
    } finally {
      setLoading(false)
    }
  }, [scopeId])

  useEffect(() => { void load() }, [load])

  useEffect(() => {
    const timer = window.setInterval(() => setNow(Date.now()), 1000)
    return () => window.clearInterval(timer)
  }, [])

  const save = async () => {
    setSaving(true)
    try {
      const value = await createAgentAPIProxyClientFromStorage().updateTeamConfig(scopeId, editable)
      setConfig(value)
      setEditable(value.github_teams)
      setError(null)
    } catch {
      setError('GitHub チームマッピングを保存できませんでした')
    } finally {
      setSaving(false)
    }
  }

  const syncMembers = async () => {
    setSyncing(true)
    setSyncResult(null)
    try {
      const client = createAgentAPIProxyClientFromStorage()
      const result = await client.syncTeamMembers(scopeId)
      setSyncResult(result)
      setMembers(await client.getTeamMembers(scopeId))
      setError(null)
    } catch {
      setError('GitHub チームメンバーを同期できませんでした。1分以内の再実行、連携アカウントの権限、チーム設定を確認してください。')
    } finally {
      setSyncing(false)
    }
  }

  if (loading) return <p className="text-sm text-gray-500">読み込み中...</p>

  const nextSyncAt = members?.sync.next_sync_at ? new Date(members.sync.next_sync_at).getTime() : 0
  const retrySeconds = Math.max(0, Math.ceil((nextSyncAt - now) / 1000))
  const syncDisabled = syncing || retrySeconds > 0 || !config?.github_teams.length
  return (
    <div className="max-w-3xl space-y-6">
      <div>
        <h1 className="text-xl font-semibold">GitHub チーム</h1>
        <p className="mt-1 text-sm text-gray-500">この ccplant チームに所属させる外部 GitHub チームを設定します。</p>
        {config && <p className="mt-2 font-mono text-xs text-gray-500">Principal ID: {config.principal_id}</p>}
      </div>
      {error && <p className="rounded-md bg-red-50 p-3 text-sm text-red-700">{error}</p>}
      {editable.map((item, index) => (
        <div key={index} className="grid grid-cols-[1fr_auto] gap-2">
          <input value={item} placeholder="org/team-slug" onChange={(event) => setEditable((current) => current.map((value, i) => i === index ? event.target.value : value))} className="rounded-md border px-3 py-2 text-sm dark:bg-gray-900" />
          <button type="button" aria-label="削除" onClick={() => setEditable((current) => current.filter((_, i) => i !== index))} className="rounded-md border p-2"><Trash2 className="h-4 w-4" /></button>
        </div>
      ))}
      <div className="flex gap-2">
        <button type="button" onClick={() => setEditable((current) => [...current, ''])} className="inline-flex items-center gap-1 rounded-md border px-3 py-2 text-sm"><Plus className="h-4 w-4" />追加</button>
        <button type="button" disabled={saving} onClick={save} className="rounded-md bg-blue-600 px-4 py-2 text-sm text-white disabled:opacity-50">{saving ? '保存中...' : '保存'}</button>
      </div>
      <section className="rounded-lg border p-4">
        <div className="flex flex-wrap items-start justify-between gap-3">
          <div>
            <h2 className="flex items-center gap-2 font-medium"><Users className="h-4 w-4" />メンバー</h2>
            <p className="mt-1 text-sm text-gray-500">
              {members?.sync.status === 'never'
                ? 'まだ同期されていません。'
                : `同期済み ${members?.members.length ?? 0}人・未連携 ${members?.unlinked_external_member_count ?? 0}人`}
            </p>
            {members?.sync.synced_at && <p className="mt-1 text-xs text-gray-500">最終同期: {new Date(members.sync.synced_at).toLocaleString()}</p>}
          </div>
          <button type="button" disabled={syncDisabled} onClick={syncMembers} className="inline-flex items-center gap-2 rounded-md bg-blue-600 px-4 py-2 text-sm text-white disabled:opacity-50">
            <RefreshCw className={`h-4 w-4 ${syncing ? 'animate-spin' : ''}`} />
            {syncing ? '同期中...' : retrySeconds > 0 ? `${retrySeconds}秒後に同期可能` : 'GitHub からメンバーを同期'}
          </button>
        </div>
        {syncResult && <p className="mt-3 rounded-md bg-green-50 p-3 text-sm text-green-700">同期しました（追加 {syncResult.added_count}人・削除 {syncResult.removed_count}人）</p>}
        {members && members.members.length > 0 && (
          <ul className="mt-4 divide-y">
            {members.members.map((member) => <li key={member.principal_id} className="flex justify-between py-2 text-sm"><span>{member.login || member.principal_id}</span><span className="font-mono text-xs text-gray-500">{member.principal_id}</span></li>)}
          </ul>
        )}
      </section>
    </div>
  )
}
