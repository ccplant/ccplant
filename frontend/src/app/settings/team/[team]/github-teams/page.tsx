'use client'

import { useCallback, useEffect, useState } from 'react'
import { Plus, Trash2 } from 'lucide-react'
import { createAgentAPIProxyClientFromStorage } from '@/lib/agentapi-proxy-client'
import type { ExternalTeamBinding, TeamConfig } from '@/types/team-config'
import { useSettingsScope } from '../../../SettingsScopeContext'

const emptyBinding = (): ExternalTeamBinding => ({ connection_id: '', organization: '', team_slug: '' })

export default function GitHubTeamsPage() {
  const { scopeId } = useSettingsScope()
  const [config, setConfig] = useState<TeamConfig | null>(null)
  const [editable, setEditable] = useState<ExternalTeamBinding[]>([])
  const [loading, setLoading] = useState(true)
  const [saving, setSaving] = useState(false)
  const [error, setError] = useState<string | null>(null)

  const load = useCallback(async () => {
    if (!scopeId) return
    setLoading(true)
    try {
      const value = await createAgentAPIProxyClientFromStorage().getTeamConfig(scopeId)
      setConfig(value)
      setEditable(value.external_teams.filter((item) => item.managed_by !== 'discovery'))
      setError(null)
    } catch {
      setError('GitHub チームマッピングを読み込めませんでした')
    } finally {
      setLoading(false)
    }
  }, [scopeId])

  useEffect(() => { void load() }, [load])

  const save = async () => {
    setSaving(true)
    try {
      const value = await createAgentAPIProxyClientFromStorage().updateTeamConfig(scopeId, editable)
      setConfig(value)
      setEditable(value.external_teams.filter((item) => item.managed_by !== 'discovery'))
      setError(null)
    } catch {
      setError('GitHub チームマッピングを保存できませんでした')
    } finally {
      setSaving(false)
    }
  }

  if (loading) return <p className="text-sm text-gray-500">読み込み中...</p>

  const discovered = config?.external_teams.filter((item) => item.managed_by === 'discovery') ?? []
  return (
    <div className="max-w-3xl space-y-6">
      <div>
        <h1 className="text-xl font-semibold">GitHub チーム</h1>
        <p className="mt-1 text-sm text-gray-500">この ccplant チームに所属させる外部 GitHub チームを設定します。</p>
        {config && <p className="mt-2 font-mono text-xs text-gray-500">Principal ID: {config.principal_id}</p>}
      </div>
      {error && <p className="rounded-md bg-red-50 p-3 text-sm text-red-700">{error}</p>}
      {discovered.map((item) => (
        <div key={`${item.connection_id}/${item.organization}/${item.team_slug}`} className="rounded-md border p-3 text-sm">
          <span className="font-mono">{item.connection_id}: {item.organization}/{item.team_slug}</span>
          <span className="ml-2 text-xs text-gray-500">config から自動作成・読み取り専用</span>
        </div>
      ))}
      {editable.map((item, index) => (
        <div key={index} className="grid grid-cols-[1fr_1fr_1fr_auto] gap-2">
          {(['connection_id', 'organization', 'team_slug'] as const).map((field) => (
            <input key={field} value={item[field]} placeholder={field} onChange={(event) => setEditable((current) => current.map((value, i) => i === index ? { ...value, [field]: event.target.value } : value))} className="rounded-md border px-3 py-2 text-sm dark:bg-gray-900" />
          ))}
          <button type="button" aria-label="削除" onClick={() => setEditable((current) => current.filter((_, i) => i !== index))} className="rounded-md border p-2"><Trash2 className="h-4 w-4" /></button>
        </div>
      ))}
      <div className="flex gap-2">
        <button type="button" onClick={() => setEditable((current) => [...current, emptyBinding()])} className="inline-flex items-center gap-1 rounded-md border px-3 py-2 text-sm"><Plus className="h-4 w-4" />追加</button>
        <button type="button" disabled={saving} onClick={save} className="rounded-md bg-blue-600 px-4 py-2 text-sm text-white disabled:opacity-50">{saving ? '保存中...' : '保存'}</button>
      </div>
    </div>
  )
}
