'use client'

import { useCallback, useEffect, useState } from 'react'
import { KeyRound, Plus, X } from 'lucide-react'
import { SettingsPageHeader } from '@/components/settings'
import { createAgentAPIProxyClientFromStorage } from '@/lib/agentapi-proxy-client'
import type { SecretProjection, SettingsSecret } from '@/types/settings'
import { useSettingsScope } from '../SettingsScopeContext'

type SecretEntry = { key: string; value: string; type: 'unset' | 'env' | 'file' | 'kv'; target: string; permissions: '0400' | '0600' }
const emptyEntry = (): SecretEntry => ({ key: '', value: '', type: 'unset', target: '', permissions: '0600' })
const entryKey = (entry: SecretEntry) => entry.type === 'kv' ? entry.key.trim() : entry.target.trim()

export function SecretsSection() {
  const { scopeId } = useSettingsScope()
  const [secrets, setSecrets] = useState<SettingsSecret[]>([])
  const [isCreateOpen, setIsCreateOpen] = useState(false)
  const [name, setName] = useState('')
  const [entries, setEntries] = useState<SecretEntry[]>([emptyEntry()])
  const [error, setError] = useState('')
  const [isSubmitting, setIsSubmitting] = useState(false)

  const reload = useCallback(async () => {
    if (!scopeId) return
    try { setSecrets(await createAgentAPIProxyClientFromStorage().listSettingsSecrets(scopeId)); setError('') }
    catch { setError('シークレットを読み込めませんでした') }
  }, [scopeId])
  useEffect(() => { void reload() }, [reload])

  const resetCreate = () => {
    setIsCreateOpen(false); setName(''); setEntries([emptyEntry()]); setError('')
  }
  const closeCreate = () => { if (!isSubmitting) resetCreate() }

  const create = async (event: React.FormEvent) => {
    event.preventDefault()
    if (!name.trim()) { setError('名前を入力してください'); return }
    if (entries.some(entry => entry.type === 'unset')) { setError('各項目の用途を選択してください'); return }
    const valid = entries.filter(entry => entryKey(entry) && entry.value)
    if (valid.length !== entries.length) { setError('すべての項目に名前またはパスと値を入力してください'); return }
    if (new Set(valid.map(entryKey)).size !== valid.length) { setError('環境変数名、パス、またはキーが重複しています'); return }
    try {
      setIsSubmitting(true); setError('')
      const projections: SecretProjection[] = []
      for (const entry of valid) {
        const key = entryKey(entry)
        if (entry.type === 'env') projections.push({ key, type: 'env', env_name: entry.target.trim() })
        if (entry.type === 'file') projections.push({ key, type: 'file', path: entry.target.trim(), permissions: entry.permissions })
      }
      await createAgentAPIProxyClientFromStorage().createSettingsSecret(scopeId, {
        name: name.trim(), values: Object.fromEntries(valid.map(entry => [entryKey(entry), entry.value])), projections,
      })
      resetCreate(); await reload()
    } catch { setError('シークレットを保存できませんでした') }
    finally { setIsSubmitting(false) }
  }

  return <>
    <SettingsPageHeader title="シークレット" description="任意のキーと値を安全に保存します。保存済みの値は再表示されません。SlackBot やセッションプロファイルから参照できます。" />
    <div className="space-y-4">
      <div className="flex items-center justify-between gap-4">
        <p className="text-sm text-gray-600 dark:text-gray-400">登録したシークレットは、利用する機能側で選択して参照できます。</p>
        <button type="button" onClick={() => setIsCreateOpen(true)} className="inline-flex shrink-0 items-center gap-1.5 rounded-md bg-blue-600 px-4 py-2 text-sm font-medium text-white transition-colors hover:bg-blue-700"><Plus className="h-4 w-4" /> 追加</button>
      </div>
      {error && !isCreateOpen && <p role="alert" className="text-sm text-red-600">{error}</p>}
      {secrets.length > 0 ? <div className="space-y-2">{secrets.map(secret => <div key={secret.id} className="flex items-center justify-between rounded-lg border border-gray-200 p-4 transition-colors hover:border-gray-300 dark:border-gray-700 dark:hover:border-gray-600">
        <div className="flex min-w-0 items-center gap-3"><KeyRound className="h-5 w-5 shrink-0 text-gray-400" /><div className="min-w-0"><div className="font-medium text-gray-900 dark:text-white">{secret.name}</div><div className="mt-1 truncate text-xs text-gray-500">キー: {secret.keys.join(', ')}</div></div></div>
        <button type="button" className="rounded px-3 py-1 text-sm text-red-600 hover:bg-red-50 dark:text-red-400 dark:hover:bg-red-900/20" onClick={async () => { if (confirm(`「${secret.name}」を削除しますか？`)) { await createAgentAPIProxyClientFromStorage().deleteSettingsSecret(scopeId, secret.id); await reload() } }}>削除</button>
      </div>)}</div> : !error && <div className="rounded-lg bg-gray-50 py-8 text-center text-sm text-gray-500 dark:bg-gray-800 dark:text-gray-400">シークレットはまだありません</div>}
    </div>

    {isCreateOpen && <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/50 p-4">
      <div className="w-full max-w-2xl overflow-hidden rounded-lg bg-white shadow-xl dark:bg-gray-800">
        <div className="flex items-center justify-between border-b border-gray-200 px-6 py-4 dark:border-gray-700"><h2 className="text-lg font-semibold text-gray-900 dark:text-white">シークレットを追加</h2><button type="button" aria-label="閉じる" disabled={isSubmitting} onClick={closeCreate} className="text-gray-400 hover:text-gray-600 disabled:opacity-50 dark:hover:text-gray-300"><X className="h-5 w-5" /></button></div>
        <form onSubmit={create} className="max-h-[75vh] space-y-5 overflow-y-auto px-6 py-4">
          {error && <div role="alert" className="rounded-md border border-red-200 bg-red-50 p-3 text-sm text-red-700 dark:border-red-800 dark:bg-red-900/20 dark:text-red-400">{error}</div>}
          <label className="block text-sm font-medium text-gray-700 dark:text-gray-300">名前 <span className="text-red-500">*</span><input autoFocus value={name} onChange={e => setName(e.target.value)} placeholder="例: GitHub integration" className="mt-1 w-full rounded-md border border-gray-300 bg-white px-3 py-2 text-sm focus:border-transparent focus:outline-none focus:ring-2 focus:ring-blue-500 dark:border-gray-600 dark:bg-gray-700 dark:text-white" /></label>
          <div>
            <div className="mb-2"><div className="text-sm font-medium text-gray-700 dark:text-gray-300">登録する項目 <span className="text-red-500">*</span></div><p className="mt-1 text-xs text-gray-500">最初に用途を選んでください。値や内容は保存後に再表示されません。</p></div>
            <div className="space-y-3">{entries.map((entry, index) => <div key={index} className="rounded-lg border border-gray-200 bg-gray-50 p-3 dark:border-gray-600 dark:bg-gray-900/40">
              <div className="mb-3 flex items-start justify-between gap-2">
                <fieldset className="min-w-0 flex-1"><legend className="mb-2 text-xs font-medium text-gray-600 dark:text-gray-400">用途</legend><div className="grid grid-cols-3 gap-2">{([
                  { type: 'env', label: '環境変数', description: 'Env' },
                  { type: 'file', label: 'ファイル', description: 'File' },
                  { type: 'kv', label: 'その他', description: 'Key / Value' },
                ] as const).map(option => <button key={option.type} type="button" aria-pressed={entry.type === option.type} onClick={() => setEntries(rows => rows.map((row, i) => i === index ? { ...emptyEntry(), type: option.type } : row))} className={`rounded-md border px-2 py-2 text-left transition-colors ${entry.type === option.type ? 'border-blue-500 bg-blue-50 text-blue-700 ring-1 ring-blue-500 dark:bg-blue-900/30 dark:text-blue-300' : 'border-gray-300 bg-white text-gray-700 hover:border-gray-400 dark:border-gray-600 dark:bg-gray-700 dark:text-gray-300'}`}><span className="block text-sm font-medium">{option.label}</span><span className="block text-[10px] text-gray-500 dark:text-gray-400">{option.description}</span></button>)}</div></fieldset>
                <button type="button" aria-label={`項目 ${index + 1} を削除`} onClick={() => setEntries(rows => rows.length === 1 ? [emptyEntry()] : rows.filter((_, i) => i !== index))} className="mt-1 p-1 text-gray-400 hover:text-red-500"><X className="h-4 w-4" /></button>
              </div>
              {entry.type === 'unset' && <p className="text-center text-xs text-gray-500">用途を選ぶと入力欄が表示されます</p>}
              {entry.type === 'env' && <div className="grid gap-2 sm:grid-cols-2"><input aria-label={`環境変数名 ${index + 1}`} className="rounded-md border border-gray-300 bg-white px-3 py-2 font-mono text-sm dark:border-gray-600 dark:bg-gray-700" placeholder="API_TOKEN" value={entry.target} onChange={e => setEntries(rows => rows.map((row, i) => i === index ? { ...row, target: e.target.value } : row))} /><input aria-label={`環境変数の値 ${index + 1}`} type="password" autoComplete="new-password" className="rounded-md border border-gray-300 bg-white px-3 py-2 font-mono text-sm dark:border-gray-600 dark:bg-gray-700" placeholder="値" value={entry.value} onChange={e => setEntries(rows => rows.map((row, i) => i === index ? { ...row, value: e.target.value } : row))} /></div>}
              {entry.type === 'file' && <div className="space-y-2"><input aria-label={`配置パス ${index + 1}`} className="w-full rounded-md border border-gray-300 bg-white px-3 py-2 font-mono text-sm dark:border-gray-600 dark:bg-gray-700" placeholder="/absolute/path" value={entry.target} onChange={e => setEntries(rows => rows.map((row, i) => i === index ? { ...row, target: e.target.value } : row))} /><textarea aria-label={`ファイル内容 ${index + 1}`} rows={3} className="w-full rounded-md border border-gray-300 bg-white px-3 py-2 font-mono text-sm dark:border-gray-600 dark:bg-gray-700" placeholder="ファイルの内容" value={entry.value} onChange={e => setEntries(rows => rows.map((row, i) => i === index ? { ...row, value: e.target.value } : row))} /><label className="block text-xs text-gray-500">パーミッション<select aria-label={`パーミッション ${index + 1}`} className="ml-2 w-24 rounded-md border border-gray-300 bg-white px-2 py-1 font-mono text-sm dark:border-gray-600 dark:bg-gray-700" value={entry.permissions} onChange={e => setEntries(rows => rows.map((row, i) => i === index ? { ...row, permissions: e.target.value as SecretEntry['permissions'] } : row))}><option value="0600">0600</option><option value="0400">0400</option></select></label></div>}
              {entry.type === 'kv' && <div className="grid gap-2 sm:grid-cols-2"><input aria-label={`キー ${index + 1}`} className="rounded-md border border-gray-300 bg-white px-3 py-2 font-mono text-sm dark:border-gray-600 dark:bg-gray-700" placeholder="キー" value={entry.key} onChange={e => setEntries(rows => rows.map((row, i) => i === index ? { ...row, key: e.target.value } : row))} /><input aria-label={`値 ${index + 1}`} type="password" autoComplete="new-password" className="rounded-md border border-gray-300 bg-white px-3 py-2 font-mono text-sm dark:border-gray-600 dark:bg-gray-700" placeholder="値" value={entry.value} onChange={e => setEntries(rows => rows.map((row, i) => i === index ? { ...row, value: e.target.value } : row))} /></div>}
            </div>)}</div>
            <button type="button" className="mt-3 inline-flex items-center gap-1 text-sm text-blue-600 hover:text-blue-800 dark:text-blue-400" onClick={() => setEntries(rows => [...rows, emptyEntry()])}><Plus className="h-4 w-4" /> 項目を追加</button>
          </div>
          <div className="flex justify-end gap-3 border-t border-gray-200 pt-4 dark:border-gray-700"><button type="button" disabled={isSubmitting} onClick={closeCreate} className="rounded-md bg-gray-100 px-4 py-2 text-sm text-gray-700 hover:bg-gray-200 disabled:opacity-50 dark:bg-gray-700 dark:text-gray-300 dark:hover:bg-gray-600">キャンセル</button><button type="submit" disabled={isSubmitting} className="rounded-md bg-blue-600 px-4 py-2 text-sm font-medium text-white hover:bg-blue-700 disabled:opacity-50">{isSubmitting ? '保存中…' : '保存'}</button></div>
        </form>
      </div>
    </div>}
  </>
}
