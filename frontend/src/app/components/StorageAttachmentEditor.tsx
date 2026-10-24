'use client'

import { useEffect, useState } from 'react'
import Link from 'next/link'
import { createAgentAPIProxyClientFromStorage } from '../../lib/agentapi-proxy-client'
import type { Storage, StorageAttachment } from '../../types/storage'

export function validateStorageAttachments(values: StorageAttachment[]): string | null {
  const paths: string[] = []
  const sources = new Map<string, string[]>()
  for (const value of values) {
    if (!value.storage_id) return 'Storage を選択してください'
    const storagePath = (value.storage_path ?? '').trim().replace(/^\/+|\/+$/g, '')
    if (storagePath && storagePath.split('/').some(segment => segment === '.' || segment === '..' || segment === '')) return 'Storage内のパスには相対パスを指定してください'
    const sameStorageSources = sources.get(value.storage_id) ?? []
    if (sameStorageSources.some(other => !storagePath || !other || other === storagePath || other.startsWith(storagePath + '/') || storagePath.startsWith(other + '/'))) return '同じStorage内のパスが重複しています'
    sources.set(value.storage_id, [...sameStorageSources, storagePath])
    const segments = value.mount_path.trim().split('/').filter(Boolean)
    const path = '/' + segments.join('/')
    if (!value.mount_path.startsWith('/') || segments.length === 0 || segments.some(segment => segment === '.' || segment === '..')) return 'マウント先には / 以外の絶対パスを指定してください'
    const reserved = ['/bin', '/boot', '/dev', '/etc', '/proc', '/sys', '/usr', '/var/run', '/session-settings', '/credentials', '/opt/ccplant', '/home/agentapi/.codex', '/home/agentapi/.claude', '/home/agentapi/.docker']
    if (path === '/home/agentapi/workdir' || reserved.some(prefix => path === prefix || path.startsWith(prefix + '/'))) return 'このパスはセッションが使用しています。別のマウント先を指定してください'
    if (paths.some(other => other === path || other.startsWith(path + '/') || path.startsWith(other + '/'))) return 'マウント先が重複しています'
    paths.push(path)
  }
  return null
}

export default function StorageAttachmentEditor({ value, onChange, scope, teamId }: {
  value: StorageAttachment[]
  onChange: (value: StorageAttachment[]) => void
  scope: 'user' | 'team'
  teamId?: string
}) {
  const [storages, setStorages] = useState<Storage[]>([])
  const [error, setError] = useState('')
  const [loading, setLoading] = useState(true)
  const [retry, setRetry] = useState(0)
  useEffect(() => {
    let current = true
    setLoading(true)
    setError('')
    createAgentAPIProxyClientFromStorage().getStorages({ scope, team_id: teamId })
      .then(result => { if (current) setStorages(result.storages) })
      .catch(err => { if (current) setError(err instanceof Error ? err.message : 'Storage の取得に失敗しました') })
      .finally(() => { if (current) setLoading(false) })
    return () => { current = false }
  }, [scope, teamId, retry])
  const update = (index: number, patch: Partial<StorageAttachment>) => onChange(value.map((item, i) => i === index ? { ...item, ...patch } : item))
  const field = 'w-full border rounded-lg px-3 py-2 dark:bg-gray-700 dark:text-white'
  return <div className="space-y-4">
    <p className="text-sm text-gray-500 dark:text-gray-400">セッション起動時にファイルを復元し、編集した内容を自動で保存します。同じファイルへの更新は最後の保存が優先されます。</p>
    <Link href="/storages" className="inline-block text-sm text-blue-600 dark:text-blue-400">Storage を管理</Link>
    {loading && <p role="status">Storage を読み込み中…</p>}
    {error && <p role="alert" className="text-red-600 dark:text-red-400">{error} <button type="button" className="underline" onClick={() => setRetry(count => count + 1)}>再試行</button></p>}
    {!loading && !error && storages.length === 0 && <p className="text-sm text-gray-500">このスコープには Storage がありません。Storage 管理画面で作成してください。</p>}
    {value.map((attachment, index) => <fieldset key={index} className="space-y-3 p-4 border border-gray-200 dark:border-gray-700 rounded-lg">
      <legend className="px-1 text-sm">Storage {index + 1}</legend>
      <label className="block text-sm">Storage<select value={attachment.storage_id} onChange={event => update(index, { storage_id: event.target.value })} className={`${field} mt-1`}>
        <option value="">選択してください</option>
        {attachment.storage_id && !storages.some(storage => storage.id === attachment.storage_id) && <option value={attachment.storage_id}>未取得の Storage ({attachment.storage_id})</option>}
        {storages.map(storage => <option key={storage.id} value={storage.id}>{storage.name}</option>)}
      </select></label>
      <label className="block text-sm">Storage内のパス（任意）<input value={attachment.storage_path ?? ''} onChange={event => update(index, { storage_path: event.target.value })} placeholder="datasets/shared" className={`${field} mt-1`} /></label>
      <label className="block text-sm">マウント先<input value={attachment.mount_path} onChange={event => update(index, { mount_path: event.target.value })} placeholder="/home/agentapi/workdir/shared" className={`${field} mt-1`} /></label>
      <label className="flex items-center gap-2 text-sm"><input type="checkbox" checked={attachment.read_only ?? false} onChange={event => update(index, { read_only: event.target.checked })} />読み取り専用</label>
      <button type="button" onClick={() => onChange(value.filter((_, i) => i !== index))} className="text-sm text-red-600 dark:text-red-400">取り外す</button>
    </fieldset>)}
    <button type="button" disabled={loading || !!error || storages.length === 0} onClick={() => onChange([...value, { storage_id: '', storage_path: '', mount_path: '', read_only: false }])} className="px-3 py-2 text-sm rounded-lg border border-gray-300 dark:border-gray-600 disabled:opacity-50">Storage を追加</button>
  </div>
}
