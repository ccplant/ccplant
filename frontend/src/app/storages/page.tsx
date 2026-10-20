'use client'

import { useEffect, useRef, useState } from 'react'
import { Database, Plus, Pencil, Trash2, X } from 'lucide-react'
import TopBar from '../components/TopBar'
import NavigationTabs from '../components/NavigationTabs'
import { useTeamScope } from '../../contexts/TeamScopeContext'
import { createAgentAPIProxyClientFromStorage } from '../../lib/agentapi-proxy-client'
import type { Storage } from '../../types/storage'

const field = 'w-full px-3 py-2 border border-gray-300 dark:border-gray-600 rounded-lg bg-white dark:bg-gray-700 text-gray-900 dark:text-white'
const primary = 'inline-flex items-center gap-2 px-4 py-2 bg-blue-600 hover:bg-blue-700 text-white rounded-lg disabled:opacity-50'

export default function StoragesPage() {
  const { getScopeParams } = useTeamScope()
  const [storages, setStorages] = useState<Storage[]>([])
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState('')
  const [refresh, setRefresh] = useState(0)
  const [editing, setEditing] = useState<Storage | null | undefined>(undefined)
  const [name, setName] = useState('')
  const [description, setDescription] = useState('')
  const [saving, setSaving] = useState(false)
  const [formError, setFormError] = useState('')
  const [deleting, setDeleting] = useState<string | null>(null)
  const dialogRef = useRef<HTMLDivElement>(null)

  useEffect(() => {
    let current = true
    setLoading(true)
    setError('')
    setEditing(undefined)
    createAgentAPIProxyClientFromStorage().getStorages(getScopeParams())
      .then(result => { if (current) setStorages(result.storages) })
      .catch(err => { if (current) setError(err instanceof Error ? err.message : 'Storage の取得に失敗しました') })
      .finally(() => { if (current) setLoading(false) })
    return () => { current = false }
  }, [getScopeParams, refresh])

  useEffect(() => {
    if (editing === undefined) return
    const previousFocus = document.activeElement as HTMLElement | null
    const escape = (event: KeyboardEvent) => {
      if (event.key === 'Escape' && !saving) setEditing(undefined)
      if (event.key === 'Tab') {
        const controls = dialogRef.current?.querySelectorAll<HTMLElement>('button:not(:disabled), input:not(:disabled), textarea:not(:disabled)')
        if (!controls?.length) return
        const first = controls[0]
        const last = controls[controls.length - 1]
        if (event.shiftKey && document.activeElement === first) { event.preventDefault(); last.focus() }
        else if (!event.shiftKey && document.activeElement === last) { event.preventDefault(); first.focus() }
      }
    }
    document.addEventListener('keydown', escape)
    return () => { document.removeEventListener('keydown', escape); previousFocus?.focus() }
  }, [editing, saving])

  const open = (storage: Storage | null) => {
    setName(storage?.name ?? '')
    setDescription(storage?.description ?? '')
    setFormError('')
    setEditing(storage)
  }

  const save = async (event: React.FormEvent) => {
    event.preventDefault()
    setSaving(true)
    setFormError('')
    try {
      const client = createAgentAPIProxyClientFromStorage()
      if (editing) await client.updateStorage(editing.id, { name: name.trim(), description })
      else await client.createStorage({ ...getScopeParams(), name: name.trim(), description, kind: 'object_snapshot' })
      setEditing(undefined)
      setRefresh(value => value + 1)
    } catch (err) {
      setFormError(err instanceof Error ? err.message : 'Storage の保存に失敗しました')
    } finally { setSaving(false) }
  }

  const remove = async (storage: Storage) => {
    if (!window.confirm(`「${storage.name}」を削除しますか？ この Storage を使うプロファイルやセッションから利用できなくなります。`)) return
    setDeleting(storage.id)
    try {
      await createAgentAPIProxyClientFromStorage().deleteStorage(storage.id)
      setStorages(previous => previous.filter(item => item.id !== storage.id))
    } catch (err) { setError(err instanceof Error ? err.message : 'Storage の削除に失敗しました') }
    finally { setDeleting(null) }
  }

  return <main className="min-h-dvh bg-gray-50 dark:bg-gray-900 text-gray-900 dark:text-white">
    <TopBar title="Storage" showSettingsButton><div className="md:hidden"><NavigationTabs /></div></TopBar>
    <div className="flex">
      <aside className="hidden md:block w-80 shrink-0 min-h-screen p-4 bg-white dark:bg-gray-800 border-r border-gray-200 dark:border-gray-700">
        <NavigationTabs />
        <h2 className="mt-6 mb-3 text-lg font-semibold">永続ストレージ</h2>
        <p className="text-sm text-gray-600 dark:text-gray-400">セッション間でファイルを共有します。Session Profile で Storage とマウント先を選んでください。</p>
        <p className="mt-3 text-sm text-gray-600 dark:text-gray-400">同じファイルを複数のセッションが更新した場合は、最後に保存した内容が残ります。</p>
      </aside>
      <div className="flex-1 min-w-0 p-4 md:p-8">
        <div className="flex justify-end mb-6"><button className={primary} onClick={() => open(null)}><Plus size={18} />新しい Storage</button></div>
        {error && <div role="alert" className="mb-4 p-4 rounded-lg bg-red-50 text-red-700 dark:bg-red-900/20 dark:text-red-300">{error}<button className="ml-4 underline" onClick={() => setRefresh(value => value + 1)}>再読み込み</button></div>}
        {loading ? <p role="status">読み込み中…</p> : !error && storages.length === 0 ? <div className="py-16 text-center text-gray-500 dark:text-gray-400"><Database className="mx-auto mb-4" size={36} /><p>Storage はまだありません</p><p className="mt-2 text-sm">新しい Storage を作成して、プロファイルに追加してください。</p></div> :
          <div className="grid grid-cols-1 xl:grid-cols-2 gap-4">{storages.map(storage => <article key={storage.id} className="p-5 bg-white dark:bg-gray-800 border border-gray-200 dark:border-gray-700 rounded-lg">
            <div className="flex items-start justify-between gap-3"><h2 className="font-semibold break-words">{storage.name}</h2><span className="text-xs px-2 py-1 rounded bg-gray-100 dark:bg-gray-700">{storage.state}</span></div>
            {storage.description && <p className="mt-2 text-sm text-gray-600 dark:text-gray-400 whitespace-pre-wrap break-words">{storage.description}</p>}
            <p className="mt-4 text-sm text-gray-500 dark:text-gray-400">{storage.file_count} ファイル · Revision {storage.revision}</p>
            <p className="mt-1 text-xs text-gray-500 dark:text-gray-400">更新: {new Date(storage.updated_at).toLocaleString()}</p>
            <div className="mt-4 flex justify-end gap-4 text-sm"><button onClick={() => open(storage)} className="inline-flex items-center gap-1 text-blue-600 dark:text-blue-400"><Pencil size={15} />編集</button><button disabled={deleting !== null} onClick={() => remove(storage)} className="inline-flex items-center gap-1 text-red-600 dark:text-red-400 disabled:opacity-50"><Trash2 size={15} />{deleting === storage.id ? '削除中…' : '削除'}</button></div>
          </article>)}</div>}
      </div>
    </div>
    {editing !== undefined && <div className="fixed inset-0 z-50 flex items-center justify-center p-4 bg-black/50">
      <div ref={dialogRef} role="dialog" aria-modal="true" aria-labelledby="storage-title" className="w-full max-w-lg max-h-[90dvh] overflow-y-auto rounded-xl bg-white dark:bg-gray-800 p-6 shadow-xl">
        <div className="flex justify-between items-center mb-6"><h2 id="storage-title" className="text-lg font-semibold">{editing ? 'Storage を編集' : '新しい Storage'}</h2><button aria-label="閉じる" disabled={saving} onClick={() => setEditing(undefined)}><X size={20} /></button></div>
        <form onSubmit={save} className="space-y-4">
          {formError && <p role="alert" className="text-red-600 dark:text-red-400">{formError}</p>}
          <label className="block text-sm">名前<input autoFocus required value={name} onChange={event => setName(event.target.value)} className={`${field} mt-1`} /></label>
          <label className="block text-sm">説明<textarea value={description} onChange={event => setDescription(event.target.value)} rows={3} className={`${field} mt-1`} /></label>
          <p className="text-sm text-gray-500 dark:text-gray-400">共有範囲: {editing?.scope === 'team' ? editing.team_id : editing ? '個人' : getScopeParams().team_id ?? '個人'}</p>
          <div className="flex justify-end gap-3 pt-3"><button type="button" disabled={saving} onClick={() => setEditing(undefined)} className="px-4 py-2">キャンセル</button><button disabled={saving || !name.trim()} className={primary}>{saving ? '保存中…' : '保存'}</button></div>
        </form>
      </div>
    </div>}
  </main>
}
