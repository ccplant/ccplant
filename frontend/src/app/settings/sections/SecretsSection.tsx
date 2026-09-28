'use client'

import { useCallback, useEffect, useState } from 'react'
import { KeyRound, Plus, X } from 'lucide-react'
import { SettingsPageHeader } from '@/components/settings'
import { createAgentAPIProxyClientFromStorage } from '@/lib/agentapi-proxy-client'
import type { SecretProjection, SettingsSecret } from '@/types/settings'
import { useSettingsScope } from '../SettingsScopeContext'

type SecretEntry = { key: string; value: string; type: 'none' | 'env' | 'file'; target: string }
const emptyEntry = (): SecretEntry => ({ key: '', value: '', type: 'none', target: '' })

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
    const valid = entries.filter(entry => entry.key.trim() && entry.value)
    if (!name.trim() || valid.length === 0) { setError('名前と1つ以上のキー・値を入力してください'); return }
    if (new Set(valid.map(entry => entry.key.trim())).size !== valid.length) { setError('キー名が重複しています'); return }
    if (valid.some(entry => entry.type !== 'none' && !entry.target.trim())) { setError('利用方法を指定したキーには出力先が必要です'); return }
    try {
      setIsSubmitting(true); setError('')
      const projections: SecretProjection[] = []
      for (const entry of valid) {
        if (entry.type === 'env') projections.push({ key: entry.key.trim(), type: 'env', env_name: entry.target.trim() })
        if (entry.type === 'file') projections.push({ key: entry.key.trim(), type: 'file', path: entry.target.trim(), permissions: '0600' })
      }
      await createAgentAPIProxyClientFromStorage().createSettingsSecret(scopeId, {
        name: name.trim(), values: Object.fromEntries(valid.map(entry => [entry.key.trim(), entry.value])), projections,
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
            <div className="mb-2"><div className="text-sm font-medium text-gray-700 dark:text-gray-300">キーと値 <span className="text-red-500">*</span></div><p className="mt-1 text-xs text-gray-500">値は保存後に再表示されません。必要なら環境変数またはファイルへの出力先を指定します。</p></div>
            <div className="space-y-3">{entries.map((entry, index) => <div key={index} className="rounded-lg border border-gray-200 bg-gray-50 p-3 dark:border-gray-600 dark:bg-gray-900/40">
              <div className="mb-2 flex items-start gap-2">
                <input aria-label={`キー ${index + 1}`} className="min-w-0 flex-1 rounded-md border border-gray-300 bg-white px-3 py-2 font-mono text-sm dark:border-gray-600 dark:bg-gray-700" placeholder="キー" value={entry.key} onChange={e => setEntries(rows => rows.map((row, i) => i === index ? { ...row, key: e.target.value } : row))} />
                <input aria-label={`値 ${index + 1}`} type="password" autoComplete="new-password" className="min-w-0 flex-1 rounded-md border border-gray-300 bg-white px-3 py-2 font-mono text-sm dark:border-gray-600 dark:bg-gray-700" placeholder="値" value={entry.value} onChange={e => setEntries(rows => rows.map((row, i) => i === index ? { ...row, value: e.target.value } : row))} />
                <button type="button" aria-label={`キー ${index + 1} を削除`} onClick={() => setEntries(rows => rows.length === 1 ? [emptyEntry()] : rows.filter((_, i) => i !== index))} className="mt-1 p-1 text-gray-400 hover:text-red-500"><X className="h-4 w-4" /></button>
              </div>
              <div className="grid gap-2 sm:grid-cols-2"><select aria-label={`利用方法 ${index + 1}`} className="rounded-md border border-gray-300 bg-white px-3 py-2 text-sm dark:border-gray-600 dark:bg-gray-700" value={entry.type} onChange={e => setEntries(rows => rows.map((row, i) => i === index ? { ...row, type: e.target.value as SecretEntry['type'], target: '' } : row))}><option value="none">保存のみ</option><option value="env">環境変数として使用</option><option value="file">ファイルとして使用</option></select>{entry.type !== 'none' && <input aria-label={`出力先 ${index + 1}`} className="rounded-md border border-gray-300 bg-white px-3 py-2 font-mono text-sm dark:border-gray-600 dark:bg-gray-700" placeholder={entry.type === 'env' ? '環境変数名' : '/absolute/path'} value={entry.target} onChange={e => setEntries(rows => rows.map((row, i) => i === index ? { ...row, target: e.target.value } : row))} />}</div>
            </div>)}</div>
            <button type="button" className="mt-3 inline-flex items-center gap-1 text-sm text-blue-600 hover:text-blue-800 dark:text-blue-400" onClick={() => setEntries(rows => [...rows, emptyEntry()])}><Plus className="h-4 w-4" /> キーを追加</button>
          </div>
          <div className="flex justify-end gap-3 border-t border-gray-200 pt-4 dark:border-gray-700"><button type="button" disabled={isSubmitting} onClick={closeCreate} className="rounded-md bg-gray-100 px-4 py-2 text-sm text-gray-700 hover:bg-gray-200 disabled:opacity-50 dark:bg-gray-700 dark:text-gray-300 dark:hover:bg-gray-600">キャンセル</button><button type="submit" disabled={isSubmitting} className="rounded-md bg-blue-600 px-4 py-2 text-sm font-medium text-white hover:bg-blue-700 disabled:opacity-50">{isSubmitting ? '保存中…' : '保存'}</button></div>
        </form>
      </div>
    </div>}
  </>
}
