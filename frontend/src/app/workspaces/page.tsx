'use client'

import { FormEvent, useCallback, useEffect, useState } from 'react'
import Link from 'next/link'
import { useRouter } from 'next/navigation'
import { Archive, Clock3, LoaderCircle, Pencil, Play, RefreshCw, Trash2, X } from 'lucide-react'
import TopBar from '../components/TopBar'
import NavigationTabs from '../components/NavigationTabs'
import { useTeamScope } from '../../contexts/TeamScopeContext'
import { createAgentAPIProxyClientFromStorage } from '../../lib/agentapi-proxy-client'
import { Workspace } from '../../types/agentapi'
import { formatDate } from '../../utils/timeUtils'

export default function WorkspacesPage() {
  const router = useRouter()
  const { selectedTeam, isLoading: isTeamScopeLoading } = useTeamScope()
  const [agentAPI] = useState(() => createAgentAPIProxyClientFromStorage())
  const [workspaces, setWorkspaces] = useState<Workspace[]>([])
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)
  const [launchTarget, setLaunchTarget] = useState<Workspace | null>(null)
  const [launchInstruction, setLaunchInstruction] = useState('')
  const [launching, setLaunching] = useState(false)
  const [editTarget, setEditTarget] = useState<Workspace | null>(null)
  const [editName, setEditName] = useState('')
  const [editDescription, setEditDescription] = useState('')
  const [saving, setSaving] = useState(false)
  const [deletingId, setDeletingId] = useState<string | null>(null)

  const fetchWorkspaces = useCallback(async () => {
    if (isTeamScopeLoading) return
    setLoading(true)
    setError(null)
    try {
      const result = await agentAPI.listWorkspaces()
      setWorkspaces(result.workspaces.filter(workspace => selectedTeam
        ? workspace.scope === 'team' && workspace.team_id === selectedTeam
        : workspace.scope !== 'team'))
    } catch (err) {
      setError(err instanceof Error ? err.message : 'ワークスペースの取得に失敗しました')
    } finally {
      setLoading(false)
    }
  }, [agentAPI, isTeamScopeLoading, selectedTeam])

  useEffect(() => { void fetchWorkspaces() }, [fetchWorkspaces])

  const openEditor = (workspace: Workspace) => {
    setEditTarget(workspace)
    setEditName(workspace.name)
    setEditDescription(workspace.description || '')
  }

  const updateWorkspace = async (event: FormEvent<HTMLFormElement>) => {
    event.preventDefault()
    if (!editTarget || !editName.trim()) return
    setSaving(true)
    try {
      const updated = await agentAPI.updateWorkspace(editTarget.id, { name: editName.trim(), description: editDescription.trim() })
      setWorkspaces(items => items.map(item => item.id === updated.id ? updated : item))
      setEditTarget(null)
    } catch (err) {
      setError(err instanceof Error ? err.message : 'ワークスペースの更新に失敗しました')
    } finally {
      setSaving(false)
    }
  }

  const deleteWorkspace = async (workspace: Workspace) => {
    if (!confirm(`「${workspace.name}」を削除しますか？保存されたファイルとコンテキストは復元できません。`)) return
    setDeletingId(workspace.id)
    setError(null)
    try {
      await agentAPI.deleteWorkspace(workspace.id)
      setWorkspaces(items => items.filter(item => item.id !== workspace.id))
    } catch (err) {
      setError(err instanceof Error ? err.message : 'ワークスペースの削除に失敗しました')
    } finally {
      setDeletingId(null)
    }
  }

  const launchWorkspace = async (event: FormEvent<HTMLFormElement>) => {
    event.preventDefault()
    if (!launchTarget) return
    setLaunching(true)
    setError(null)
    try {
      const message = launchInstruction.trim()
      const session = await agentAPI.start({
        workspace_id: launchTarget.id,
        params: message ? { message } : undefined,
        scope: launchTarget.scope,
        team_id: launchTarget.team_id,
      })
      router.push(`/sessions/${session.session_id}`)
    } catch (err) {
      setError(err instanceof Error ? err.message : 'ワークスペースからセッションを作成できませんでした')
      setLaunching(false)
    }
  }

  return (
    <main className="min-h-dvh bg-gray-50 dark:bg-gray-900">
      <TopBar title="Workspaces" showSettingsButton>
        <div className="md:hidden"><NavigationTabs /></div>
      </TopBar>

      <div className="flex">
        <aside className="hidden w-64 shrink-0 border-r border-gray-200 bg-white p-4 md:block dark:border-gray-700 dark:bg-gray-800">
          <NavigationTabs />
          <p className="mt-4 px-1 text-xs leading-5 text-gray-500 dark:text-gray-400">
            セッションのファイル、設定、会話コンテキストを保存し、必要なときに新しいセッションとして使用できます。
          </p>
        </aside>

        <div className="min-w-0 flex-1 px-4 py-6 md:px-8 md:py-8">
          <div className="mx-auto max-w-5xl">
            <div className="mb-6 flex items-start justify-between gap-4">
              <div>
                <h1 className="text-2xl font-semibold text-gray-950 dark:text-white">ワークスペース</h1>
                <p className="mt-1 text-sm text-gray-600 dark:text-gray-400">保存した作業状態を選んで、新しいセッションを開始します。</p>
              </div>
              <button type="button" onClick={() => void fetchWorkspaces()} disabled={loading} className="inline-flex items-center gap-2 rounded-md border border-gray-300 bg-white px-3 py-2 text-sm font-medium text-gray-700 hover:bg-gray-50 disabled:opacity-50 dark:border-gray-600 dark:bg-gray-800 dark:text-gray-200 dark:hover:bg-gray-700">
                <RefreshCw className={`h-4 w-4 ${loading ? 'animate-spin' : ''}`} /> 更新
              </button>
            </div>

            {error && <div className="mb-5 rounded-md border border-red-200 bg-red-50 px-4 py-3 text-sm text-red-800 dark:border-red-900 dark:bg-red-950/30 dark:text-red-200">{error}</div>}

            {loading ? (
              <div className="flex justify-center py-20"><LoaderCircle className="h-7 w-7 animate-spin text-amber-700" aria-label="読み込み中" /></div>
            ) : workspaces.length === 0 ? (
              <div className="rounded-xl border border-dashed border-gray-300 bg-white px-6 py-16 text-center dark:border-gray-700 dark:bg-gray-800">
                <Archive className="mx-auto h-9 w-9 text-amber-700 dark:text-amber-400" />
                <h2 className="mt-4 font-semibold text-gray-900 dark:text-white">ワークスペースはまだありません</h2>
                <p className="mx-auto mt-2 max-w-md text-sm leading-6 text-gray-600 dark:text-gray-400">セッションの「…」メニューから「ワークスペースとして保存」を選ぶと、ここから再利用できます。</p>
                <Link href="/chats" className="mt-5 inline-flex rounded-md bg-gray-900 px-4 py-2 text-sm font-medium text-white hover:bg-gray-700 dark:bg-white dark:text-gray-900 dark:hover:bg-gray-200">セッションを見る</Link>
              </div>
            ) : (
              <div className="overflow-hidden rounded-xl border border-gray-200 bg-white shadow-sm dark:border-gray-700 dark:bg-gray-800">
                <div className="divide-y divide-gray-100 dark:divide-gray-700">
                  {workspaces.map(workspace => (
                    <article key={workspace.id} className="flex flex-col gap-4 p-5 sm:flex-row sm:items-center sm:justify-between">
                      <div className="min-w-0">
                        <div className="flex items-center gap-2">
                          <span className="flex h-8 w-8 shrink-0 items-center justify-center rounded-md bg-amber-100 text-amber-800 dark:bg-amber-950 dark:text-amber-300"><Archive className="h-4 w-4" /></span>
                          <h2 className="truncate font-semibold text-gray-950 dark:text-white">{workspace.name}</h2>
                        </div>
                        {workspace.description && <p className="mt-2 text-sm text-gray-600 dark:text-gray-300">{workspace.description}</p>}
                        <div className="mt-2 flex flex-wrap items-center gap-x-4 gap-y-1 text-xs text-gray-500 dark:text-gray-400">
                          <span className="inline-flex items-center gap-1"><Clock3 className="h-3.5 w-3.5" />保存 {formatDate(workspace.created_at)}</span>
                          <span>{workspace.use_count} 回使用</span>
                          {workspace.last_used_at && <span>最終使用 {formatDate(workspace.last_used_at)}</span>}
                        </div>
                      </div>
                      <div className="flex shrink-0 items-center gap-2">
                        <button type="button" onClick={() => openEditor(workspace)} className="rounded-md border border-gray-300 p-2 text-gray-600 hover:bg-gray-50 dark:border-gray-600 dark:text-gray-300 dark:hover:bg-gray-700" aria-label={`${workspace.name}を編集`}><Pencil className="h-4 w-4" /></button>
                        <button type="button" onClick={() => void deleteWorkspace(workspace)} disabled={deletingId === workspace.id} className="rounded-md border border-red-200 p-2 text-red-700 hover:bg-red-50 disabled:opacity-50 dark:border-red-900 dark:text-red-300 dark:hover:bg-red-950/40" aria-label={`${workspace.name}を削除`}>{deletingId === workspace.id ? <LoaderCircle className="h-4 w-4 animate-spin" /> : <Trash2 className="h-4 w-4" />}</button>
                        <button type="button" onClick={() => { setLaunchTarget(workspace); setLaunchInstruction('') }} className="inline-flex items-center gap-2 rounded-md bg-amber-700 px-4 py-2 text-sm font-medium text-white hover:bg-amber-800"><Play className="h-4 w-4" />使用する</button>
                      </div>
                    </article>
                  ))}
                </div>
              </div>
            )}
          </div>
        </div>
      </div>

      {launchTarget && <div className="fixed inset-0 z-50 flex items-center justify-center bg-gray-950/50 px-4" onMouseDown={event => { if (event.target === event.currentTarget && !launching) setLaunchTarget(null) }}>
        <form onSubmit={launchWorkspace} className="w-full max-w-lg rounded-xl bg-white shadow-2xl dark:bg-gray-900" role="dialog" aria-modal="true" aria-labelledby="workspace-launch-title">
          <div className="flex items-start justify-between border-b border-gray-200 px-5 py-4 dark:border-gray-700"><div><p className="text-sm font-medium text-amber-700 dark:text-amber-300">{launchTarget.name}</p><h2 id="workspace-launch-title" className="mt-0.5 text-lg font-semibold text-gray-950 dark:text-white">ワークスペースを使用する</h2></div><button type="button" disabled={launching} onClick={() => setLaunchTarget(null)} className="rounded p-1 text-gray-500 hover:bg-gray-100 dark:hover:bg-gray-800" aria-label="閉じる"><X className="h-5 w-5" /></button></div>
          <div className="px-5 py-5"><label className="block"><span className="text-sm font-medium text-gray-800 dark:text-gray-200">最初のメッセージ <span className="font-normal text-gray-500">（任意）</span></span><textarea autoFocus rows={5} value={launchInstruction} onChange={event => setLaunchInstruction(event.target.value)} placeholder="例: 前回の続きから、残っているテストを直してください" className="mt-2 w-full resize-y rounded-md border border-gray-300 bg-white px-3 py-2 text-gray-900 outline-none focus:border-amber-500 focus:ring-2 focus:ring-amber-200 dark:border-gray-600 dark:bg-gray-950 dark:text-white dark:focus:ring-amber-900" /><span className="mt-1.5 block text-xs text-gray-500">復元後、このメッセージを新しいセッションへ投稿します。</span></label></div>
          <div className="flex justify-end gap-2 border-t border-gray-200 bg-gray-50 px-5 py-3 dark:border-gray-700 dark:bg-gray-900"><button type="button" disabled={launching} onClick={() => setLaunchTarget(null)} className="rounded-md px-4 py-2 text-sm font-medium text-gray-700 hover:bg-gray-200 dark:text-gray-300 dark:hover:bg-gray-800">キャンセル</button><button type="submit" disabled={launching} className="inline-flex min-w-36 items-center justify-center gap-2 rounded-md bg-amber-700 px-4 py-2 text-sm font-medium text-white hover:bg-amber-800 disabled:opacity-50">{launching && <LoaderCircle className="h-4 w-4 animate-spin" />}{launching ? '復元中…' : 'セッションを作成'}</button></div>
        </form>
      </div>}

      {editTarget && <div className="fixed inset-0 z-50 flex items-center justify-center bg-gray-950/50 px-4" onMouseDown={event => { if (event.target === event.currentTarget && !saving) setEditTarget(null) }}>
        <form onSubmit={updateWorkspace} className="w-full max-w-lg rounded-xl bg-white shadow-2xl dark:bg-gray-900" role="dialog" aria-modal="true" aria-labelledby="workspace-edit-title">
          <div className="flex justify-between border-b border-gray-200 px-5 py-4 dark:border-gray-700"><h2 id="workspace-edit-title" className="text-lg font-semibold text-gray-950 dark:text-white">ワークスペースを編集</h2><button type="button" onClick={() => setEditTarget(null)} disabled={saving} className="rounded p-1 text-gray-500 hover:bg-gray-100 dark:hover:bg-gray-800" aria-label="閉じる"><X className="h-5 w-5" /></button></div>
          <div className="space-y-4 px-5 py-5"><label className="block"><span className="text-sm font-medium text-gray-800 dark:text-gray-200">名前</span><input autoFocus required maxLength={120} value={editName} onChange={event => setEditName(event.target.value)} className="mt-1.5 w-full rounded-md border border-gray-300 bg-white px-3 py-2 text-gray-900 dark:border-gray-600 dark:bg-gray-950 dark:text-white" /></label><label className="block"><span className="text-sm font-medium text-gray-800 dark:text-gray-200">説明</span><textarea rows={3} maxLength={500} value={editDescription} onChange={event => setEditDescription(event.target.value)} className="mt-1.5 w-full resize-none rounded-md border border-gray-300 bg-white px-3 py-2 text-gray-900 dark:border-gray-600 dark:bg-gray-950 dark:text-white" /></label></div>
          <div className="flex justify-end gap-2 border-t border-gray-200 bg-gray-50 px-5 py-3 dark:border-gray-700 dark:bg-gray-900"><button type="button" onClick={() => setEditTarget(null)} disabled={saving} className="rounded-md px-4 py-2 text-sm font-medium text-gray-700 hover:bg-gray-200 dark:text-gray-300 dark:hover:bg-gray-800">キャンセル</button><button type="submit" disabled={saving || !editName.trim()} className="inline-flex items-center gap-2 rounded-md bg-gray-900 px-4 py-2 text-sm font-medium text-white hover:bg-gray-700 disabled:opacity-50 dark:bg-white dark:text-gray-900">{saving && <LoaderCircle className="h-4 w-4 animate-spin" />}保存</button></div>
        </form>
      </div>}
    </main>
  )
}
