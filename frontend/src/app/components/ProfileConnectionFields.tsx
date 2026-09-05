'use client'

import type { ModelConnection } from '../../types/settings'

export default function ProfileConnectionFields({ agent, value, onChange }: {
  agent: 'codex' | 'claude'
  value: ModelConnection | null
  onChange: (value: ModelConnection | null) => void
}) {
  const label = agent === 'codex' ? 'Codex' : 'Claude Code'
  const fieldClass = 'w-full px-3 py-2 text-sm border rounded-lg dark:bg-gray-700 dark:text-white'
  return <fieldset className="mt-3 space-y-3 rounded-lg border border-gray-200 dark:border-gray-600 p-3">
    <legend className="text-sm">{label} の接続設定</legend>
    <label className="flex items-center gap-2 text-sm">
      <input type="checkbox" checked={value !== null} onChange={e => onChange(e.target.checked ? {
        mode: agent === 'codex' ? 'openai_compatible' : 'anthropic_compatible',
        base_url: '', authentication: 'api_key', model: '',
      } : null)} />
      このプロファイル専用の接続先・API キーを使う
    </label>
    {value && <>
      <label className="block text-sm">{label} Base URL
        <input type="url" required value={value.base_url ?? ''} onChange={e => onChange({ ...value, base_url: e.target.value })} placeholder={agent === 'codex' ? 'https://ollama.com/v1' : 'https://ollama.com'} className={fieldClass} />
      </label>
      <p className="text-xs text-gray-500">{agent === 'codex' ? '/responses を付けない API のベース URL を指定します。' : '/v1 や /messages を付けない URL を指定します。'}</p>
      <label className="block text-sm">{label} API 認証方式
        <select value={value.authentication ?? 'api_key'} onChange={e => onChange({ ...value, authentication: e.target.value as ModelConnection['authentication'] })} className={fieldClass}>
          <option value="api_key">API キー</option>
          {agent === 'claude' ? <option value="bearer_token">Bearer トークン</option> : <option value="none">認証なし</option>}
        </select>
      </label>
      {value.authentication !== 'none' && <label className="block text-sm">{label} API キー
        <input type="password" autoComplete="new-password" value={value.api_key ?? ''} required={!value.has_api_key} onChange={e => onChange({ ...value, api_key: e.target.value })} placeholder={value.has_api_key ? '保存済み（空欄なら保持）' : 'API キーを入力'} className={fieldClass} />
      </label>}
      <label className="block text-sm">{label} 接続のデフォルトモデル
        <input value={value.model ?? ''} onChange={e => onChange({ ...value, model: e.target.value })} placeholder="空欄なら認証設定のモデルを継承" className={fieldClass} />
      </label>
      <p className="text-xs text-gray-500">API キーは暗号化して保存し、再表示しません。チェックを外すと専用の接続設定を削除して、認証設定の継承に戻ります。</p>
    </>}
  </fieldset>
}
