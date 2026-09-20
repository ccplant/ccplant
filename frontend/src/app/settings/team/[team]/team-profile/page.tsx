'use client'

import { useEffect, useState } from 'react'
import { useRouter } from 'next/navigation'
import { AgentAPIProxyError, createAgentAPIProxyClientFromStorage } from '@/lib/agentapi-proxy-client'
import { DangerZone, SettingsPageHeader, SettingsSubsection } from '@/components/settings'
import { useSettingsScope } from '../../../SettingsScopeContext'

export default function TeamProfilePage() {
  const router = useRouter()
  const { scopeId, userTeamNames, setUserTeamName } = useSettingsScope()
  const [name, setName] = useState(userTeamNames[scopeId] || '')
  const [loading, setLoading] = useState(!name)
  const [saving, setSaving] = useState(false)
  const [deleting, setDeleting] = useState(false)
  const [error, setError] = useState<string | null>(null)

  useEffect(() => {
    if (!scopeId || name) return
    let cancelled = false
    createAgentAPIProxyClientFromStorage().getTeamConfig(scopeId)
      .then((team) => {
        if (!cancelled) setName(team.name)
      })
      .catch(() => {
        if (!cancelled) setError('チーム情報を読み込めませんでした')
      })
      .finally(() => {
        if (!cancelled) setLoading(false)
      })
    return () => { cancelled = true }
  }, [scopeId, name])

  const rename = async (event: React.FormEvent) => {
    event.preventDefault()
    const nextName = name.trim()
    if (!nextName) return
    setSaving(true)
    setError(null)
    try {
      const updated = await createAgentAPIProxyClientFromStorage().renameTeam(scopeId, nextName)
      setName(updated.name)
      setUserTeamName(scopeId, updated.name)
    } catch (reason) {
      setError(reason instanceof AgentAPIProxyError ? reason.message : 'チーム名を変更できませんでした')
    } finally {
      setSaving(false)
    }
  }

  const remove = async () => {
    const currentName = userTeamNames[scopeId] || name || scopeId
    if (!confirm(`「${currentName}」を削除しますか？この操作は取り消せません。`)) return
    setDeleting(true)
    setError(null)
    try {
      await createAgentAPIProxyClientFromStorage().deleteTeam(scopeId)
      router.push('/settings/personal/ai-providers')
      router.refresh()
    } catch (reason) {
      setError(reason instanceof AgentAPIProxyError ? reason.message : 'チームを削除できませんでした')
      setDeleting(false)
    }
  }

  return (
    <div className="max-w-3xl space-y-6">
      <SettingsPageHeader title="チーム管理" description="このチームの名前変更や削除を行います。" />
      {error && <p className="rounded-md bg-red-50 p-3 text-sm text-red-700 dark:bg-red-950/30 dark:text-red-300">{error}</p>}

      <SettingsSubsection title="チーム名" description="設定画面やチーム切り替えに表示される名前です。">
        <form onSubmit={rename} className="flex max-w-xl gap-2">
          <input
            value={name}
            onChange={(event) => setName(event.target.value)}
            disabled={loading || saving}
            aria-label="チーム名"
            className="min-w-0 flex-1 rounded-md border border-gray-300 bg-white px-3 py-2 text-sm dark:border-gray-600 dark:bg-gray-900"
          />
          <button type="submit" disabled={loading || saving || !name.trim()} className="rounded-md bg-blue-600 px-4 py-2 text-sm font-medium text-white disabled:opacity-50">
            {saving ? '保存中...' : '名前を保存'}
          </button>
        </form>
        <p className="mt-2 font-mono text-xs text-gray-500">Team ID: {scopeId}</p>
      </SettingsSubsection>

      <DangerZone title="チームの削除">
        <div className="flex items-center justify-between gap-4">
          <div>
            <p className="text-sm font-medium text-gray-900 dark:text-white">このチームを削除</p>
            <p className="mt-1 text-xs text-gray-500">チーム設定と GitHub チームのマッピングが削除されます。この操作は取り消せません。</p>
          </div>
          <button type="button" onClick={remove} disabled={deleting} className="shrink-0 rounded-md border border-red-300 px-3 py-2 text-sm font-medium text-red-600 hover:bg-red-50 disabled:opacity-50 dark:border-red-800 dark:text-red-400 dark:hover:bg-red-950/30">
            {deleting ? '削除中...' : 'チームを削除'}
          </button>
        </div>
      </DangerZone>
    </div>
  )
}
