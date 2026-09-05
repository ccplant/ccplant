'use client'

import { useEffect, useState } from 'react'
import { ModelConnection } from '@/types/settings'

interface Props {
  agent: 'codex' | 'claude'
  connection?: ModelConnection
  legacyMode?: ModelConnection['mode']
  onSave: (connection: ModelConnection) => Promise<void>
}

const modeLabels: Record<ModelConnection['mode'], string> = {
  auth_json: '既存の認証（auth.json）',
  openai_compatible: 'OpenAI 互換 API',
  oauth: 'OAuth',
  bedrock: 'Amazon Bedrock',
  anthropic_compatible: 'Anthropic 互換 API',
}

export function ModelConnectionSettings({ agent, connection, legacyMode, onSave }: Props) {
  const initialMode = connection?.mode || (agent === 'codex' ? 'auth_json' : legacyMode || 'oauth')
  const [draft, setDraft] = useState<ModelConnection>({ mode: initialMode, authentication: 'api_key', ...connection })
  const [key, setKey] = useState('')
  const [clearKey, setClearKey] = useState(false)
  const [saving, setSaving] = useState(false)
  const [error, setError] = useState('')
  const [saved, setSaved] = useState(false)
  useEffect(() => {
    setDraft({ mode: initialMode, authentication: 'api_key', ...connection })
    setKey('')
    setClearKey(false)
  }, [connection, initialMode])
  const compatible = draft.mode === 'openai_compatible' || draft.mode === 'anthropic_compatible'
  const fieldClass = 'w-full rounded-md border border-gray-300 bg-white p-2 text-sm text-gray-900 dark:border-gray-700 dark:bg-gray-800 dark:text-white'
  const change = (patch: Partial<ModelConnection>) => { setDraft(prev => ({ ...prev, ...patch })); setSaved(false) }
  const save = async () => {
    setSaving(true); setError(''); setSaved(false)
    try {
      await onSave({ ...draft, base_url: draft.base_url?.trim(), model: draft.model?.trim(), ...(key ? { api_key: key } : {}), ...(clearKey ? { clear_api_key: true } : {}) })
      setKey(''); setClearKey(false); setSaved(true)
    } catch (err) { setError(err instanceof Error ? err.message : '接続設定の保存に失敗しました') }
    finally { setSaving(false) }
  }
  return (
    <fieldset disabled={saving} className="space-y-4 py-4">
      <legend className="font-medium">{agent === 'codex' ? 'Codex の接続方式' : 'Claude Code 認証'}</legend>
      <p className="text-sm text-gray-500">現在: {modeLabels[initialMode]}。保存した設定は次のセッションから適用されます。</p>
      <label className="block text-sm">接続方式
        <select aria-label={`${agent} 接続方式`} className={fieldClass} value={draft.mode} onChange={e => change({ mode: e.target.value as ModelConnection['mode'] })}>
          {agent === 'codex' ? <>
            <option value="auth_json">既存の認証（auth.json）</option>
            <option value="openai_compatible">OpenAI 互換 API（Responses API）</option>
          </> : <>
            <option value="oauth">OAuth</option><option value="bedrock">Amazon Bedrock</option>
            <option value="anthropic_compatible">Anthropic 互換 API（Messages API）</option>
          </>}
        </select>
      </label>
      {compatible && <>
        <label className="block text-sm">Base URL
          <input aria-label={`${agent} Base URL`} className={fieldClass} value={draft.base_url || ''} onChange={e => change({ base_url: e.target.value })} placeholder={agent === 'codex' ? 'https://llm.example.com/v1' : 'https://llm.example.com/anthropic'} />
        </label>
        <p className="text-xs text-gray-500">{agent === 'codex' ? 'Responses API 対応の接続先を指定します。' : 'Messages API のルートを指定します。末尾の /v1/messages は不要です。'} localhost はセッションの実行環境を指します。</p>
        <label className="block text-sm">デフォルトモデル ID
          <input aria-label={`${agent} デフォルトモデル ID`} className={fieldClass} value={draft.model || ''} onChange={e => change({ model: e.target.value })} />
        </label>
        <p className="text-xs text-gray-500">セッションプロファイルでモデルを指定すると、このデフォルトを上書きします。</p>
        <label className="block text-sm">認証
          <select aria-label={`${agent} 認証`} className={fieldClass} value={draft.authentication || 'api_key'} onChange={e => change({ authentication: e.target.value as ModelConnection['authentication'] })}>
            <option value="api_key">API キー{agent === 'claude' ? '（x-api-key）' : ''}</option>
            {agent === 'codex' ? <option value="none">認証なし</option> : <option value="bearer_token">Bearer トークン</option>}
          </select>
        </label>
        {draft.authentication !== 'none' && <label className="block text-sm">API キー / トークン {connection?.has_api_key ? '（保存済み・未入力なら保持）' : '（未設定）'}
          <input aria-label={`${agent} API キー`} type="password" autoComplete="new-password" className={fieldClass} value={key} onChange={e => { setKey(e.target.value); setClearKey(false); setSaved(false) }} />
        </label>}
        <details><summary className="cursor-pointer text-sm">詳細設定</summary>
          <div className="space-y-3 pt-3">
            {agent === 'codex' ? <>
              <label className="block text-sm">コンテキスト長<input type="number" min="1" className={fieldClass} value={draft.context_window ?? ''} onChange={e => change({ context_window: e.target.value ? Number(e.target.value) : null })} /></label>
              <label className="block text-sm">自動圧縮開始トークン数<input type="number" min="1" className={fieldClass} value={draft.auto_compact_token_limit ?? ''} onChange={e => change({ auto_compact_token_limit: e.target.value ? Number(e.target.value) : null })} /></label>
              <label className="block text-sm">Reasoning summaries<select className={fieldClass} value={draft.supports_reasoning_summaries == null ? '' : String(draft.supports_reasoning_summaries)} onChange={e => change({ supports_reasoning_summaries: e.target.value === '' ? null : e.target.value === 'true' })}><option value="">未指定</option><option value="true">対応</option><option value="false">非対応</option></select></label>
            </> : <>
              <p className="text-xs text-gray-500">未指定の別名には、プロファイル上書き後のモデルを使用します。</p>
              {(['sonnet', 'opus', 'haiku'] as const).map(alias => <label className="block text-sm" key={alias}>{alias} モデル ID<input className={fieldClass} value={draft.model_aliases?.[alias] || ''} onChange={e => { const aliases = { ...draft.model_aliases }; if (e.target.value) aliases[alias] = e.target.value; else delete aliases[alias]; change({ model_aliases: aliases }) }} /></label>)}
            </>}
          </div>
        </details>
      </>}
      {connection?.has_api_key && <label className="flex items-center gap-2 text-sm"><input type="checkbox" checked={clearKey} onChange={e => { setClearKey(e.target.checked); setKey(''); setSaved(false) }} />保存済み API キーを削除（互換 API の認証を無効にして保存）</label>}
      {error && <p role="alert" className="text-sm text-red-600">{error}</p>}
      {saved && <p role="status" className="text-sm text-green-600">保存しました。次のセッションから適用されます。</p>}
      <button type="button" onClick={() => void save()} className="rounded-md bg-blue-600 px-4 py-2 text-sm text-white disabled:opacity-50">{saving ? '保存中...' : '保存して使用'}</button>
    </fieldset>
  )
}
