'use client'

import type { ModelConnection } from '../../types/settings'
import { CodexModelMetadataFields } from '../../components/settings/CodexModelMetadataFields'

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
    {!value && <p className="text-xs text-gray-500">接続先・API キーはベースの設定を使用します。</p>}
    {value && <>
      <label className="block text-sm">{label} Base URL
        <input type="url" required value={value.base_url ?? ''} onChange={e => onChange({ ...value, base_url: e.target.value })} placeholder={agent === 'codex' ? 'https://ollama.com/v1' : 'https://ollama.com'} className={fieldClass} />
      </label>
      <label className="block text-sm">{label} API パス
        <input value={value.endpoint_path ?? ''} onChange={e => onChange({ ...value, endpoint_path: e.target.value })} placeholder={agent === 'codex' ? '/responses' : '/v1/messages'} className={fieldClass} />
      </label>
      <p className="text-xs text-gray-500">Base URL に続くパスを / から指定します。空欄なら {agent === 'codex' ? '/responses' : '/v1/messages'} を使用します。API の形式は {agent === 'codex' ? 'Responses API' : 'Messages API'} のままです。</p>
      {value.base_url && <p className="break-all text-xs text-gray-500">送信先: {value.base_url.replace(/\/+$/, '')}{value.endpoint_path || (agent === 'codex' ? '/responses' : '/v1/messages')}</p>}
      {agent === 'codex' && <label className="block text-sm">{label} API 認証方式
        <select value={value.authentication ?? 'api_key'} onChange={e => onChange({ ...value, authentication: e.target.value as ModelConnection['authentication'] })} className={fieldClass}>
          <option value="api_key">API キー</option>
          <option value="none">認証なし</option>
        </select>
      </label>}
      {value.authentication !== 'none' && <label className="block text-sm">{label} API キー
        <input type="password" autoComplete="new-password" value={value.api_key ?? ''} required={!value.has_api_key} onChange={e => onChange({ ...value, api_key: e.target.value })} placeholder={value.has_api_key ? '保存済み（空欄なら保持）' : 'API キーを入力'} className={fieldClass} />
      </label>}
      <label className="block text-sm">{label} 接続のデフォルトモデル
        <input value={value.model ?? ''} onChange={e => onChange({ ...value, model: e.target.value })} placeholder="空欄ならベースのデフォルトモデルを使用" className={fieldClass} />
      </label>
      {agent === 'codex' && <label className="block text-sm">Web search tool
        <select value={value.web_search_enabled == null ? '' : String(value.web_search_enabled)} onChange={e => onChange({ ...value, web_search_enabled: e.target.value === '' ? null : e.target.value === 'true' })} className={fieldClass}>
          <option value="">既定の設定を使用</option>
          <option value="true">有効</option>
          <option value="false">無効</option>
        </select>
        <span className="text-xs text-gray-500">接続先が Web search に対応していない場合は無効にしてください。</span>
      </label>}
      {agent === 'codex' && <details>
        <summary className="cursor-pointer text-sm">モデルメタデータ</summary>
        <div className="pt-3">
          <CodexModelMetadataFields value={value} onChange={patch => onChange({ ...value, ...patch })} fieldClass={fieldClass} idPrefix="profile-codex" />
        </div>
      </details>}
      <p className="text-xs text-gray-500">API キーは暗号化して保存し、再表示しません。チェックを外すと専用の接続設定を削除して、ベースの接続設定を使用します。</p>
    </>}
  </fieldset>
}
