'use client'

import { useCallback, useEffect, useState } from 'react'
import { SettingsPageHeader } from '@/components/settings'
import { createAgentAPIProxyClientFromStorage } from '@/lib/agentapi-proxy-client'
import type { SecretProjection, SettingsSecret } from '@/types/settings'
import { useSettingsScope } from '../SettingsScopeContext'

export function SecretsSection() {
  const { scopeId } = useSettingsScope()
  const [secrets, setSecrets] = useState<SettingsSecret[]>([])
  const [name, setName] = useState('')
  const [entries, setEntries] = useState([{ key: '', value: '', type: 'none', target: '' }])
  const [error, setError] = useState('')

  const reload = useCallback(async () => {
    if (!scopeId) return
    try { setSecrets(await createAgentAPIProxyClientFromStorage().listSettingsSecrets(scopeId)); setError('') }
    catch { setError('シークレットを読み込めませんでした') }
  }, [scopeId])
  useEffect(() => { void reload() }, [reload])

  const create = async () => {
    const valid = entries.filter(entry => entry.key.trim() && entry.value)
    if (!name.trim() || valid.length === 0) return
    try {
      const projections: SecretProjection[] = []
      for (const entry of valid) {
        if (entry.type === 'env') projections.push({ key: entry.key.trim(), type: 'env', env_name: entry.target.trim() })
        if (entry.type === 'file') projections.push({ key: entry.key.trim(), type: 'file', path: entry.target.trim(), permissions: '0600' })
      }
      await createAgentAPIProxyClientFromStorage().createSettingsSecret(scopeId, {
        name: name.trim(),
        values: Object.fromEntries(valid.map(entry => [entry.key.trim(), entry.value])),
        projections,
      })
      setName(''); setEntries([{ key: '', value: '', type: 'none', target: '' }]); await reload()
    } catch { setError('シークレットを保存できませんでした') }
  }

  return <>
    <SettingsPageHeader title="シークレット" description="任意のキーと値を安全に保存します。保存済みの値は再表示されません。SlackBot やセッションから参照できます。" />
    <div className="space-y-4">
      <div className="rounded-lg border border-gray-200 p-4 dark:border-gray-700">
        <h3 className="mb-3 font-medium">シークレットを追加</h3>
        <input aria-label="シークレット名" className="mb-3 w-full rounded border p-2 dark:bg-gray-900" placeholder="名前" value={name} onChange={e => setName(e.target.value)} />
        <div className="space-y-2">{entries.map((entry, index) => <div key={index} className="grid gap-2 md:grid-cols-4">
          <input aria-label={`キー ${index + 1}`} className="rounded border p-2 dark:bg-gray-900" placeholder="キー" value={entry.key} onChange={e => setEntries(rows => rows.map((row, i) => i === index ? { ...row, key: e.target.value } : row))} />
          <input aria-label={`値 ${index + 1}`} type="password" autoComplete="new-password" className="rounded border p-2 dark:bg-gray-900" placeholder="値" value={entry.value} onChange={e => setEntries(rows => rows.map((row, i) => i === index ? { ...row, value: e.target.value } : row))} />
          <select aria-label={`利用方法 ${index + 1}`} className="rounded border p-2 dark:bg-gray-900" value={entry.type} onChange={e => setEntries(rows => rows.map((row, i) => i === index ? { ...row, type: e.target.value, target: '' } : row))}>
            <option value="none">保存のみ</option><option value="env">環境変数</option><option value="file">ファイル</option>
          </select>
          {entry.type === 'none' ? <div /> : <input aria-label={`出力先 ${index + 1}`} className="rounded border p-2 dark:bg-gray-900" placeholder={entry.type === 'env' ? '環境変数名' : '/absolute/path'} value={entry.target} onChange={e => setEntries(rows => rows.map((row, i) => i === index ? { ...row, target: e.target.value } : row))} />}
        </div>)}</div>
        <div className="mt-3 flex gap-2"><button className="rounded border px-3 py-2 text-sm" onClick={() => setEntries(rows => [...rows, { key: '', value: '', type: 'none', target: '' }])}>キーを追加</button>
        <button className="rounded bg-blue-600 px-4 py-2 text-white disabled:opacity-50" disabled={!name.trim() || !entries.some(entry => entry.key.trim() && entry.value)} onClick={() => void create()}>保存</button></div>
      </div>
      {error && <p className="text-sm text-red-600">{error}</p>}
      {secrets.map(secret => <div key={secret.id} className="flex items-center justify-between rounded-lg border border-gray-200 p-4 dark:border-gray-700">
        <div><div className="font-medium">{secret.name}</div><div className="text-sm text-gray-500">キー: {secret.keys.join(', ')}</div></div>
        <button className="text-sm text-red-600" onClick={async () => { if (confirm(`「${secret.name}」を削除しますか？`)) { await createAgentAPIProxyClientFromStorage().deleteSettingsSecret(scopeId, secret.id); await reload() } }}>削除</button>
      </div>)}
      {!error && secrets.length === 0 && <p className="text-sm text-gray-500">シークレットはまだありません。</p>}
    </div>
  </>
}
