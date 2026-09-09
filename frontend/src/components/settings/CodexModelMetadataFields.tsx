'use client'

import type { ModelConnection } from '@/types/settings'

interface Props {
  value: ModelConnection
  onChange: (patch: Partial<ModelConnection>) => void
  fieldClass: string
  idPrefix: string
}

export function CodexModelMetadataFields({ value, onChange, fieldClass, idPrefix }: Props) {
  return <div className="space-y-3">
    <p className="text-xs text-gray-500">互換 API のモデル情報を Codex に伝えます。未指定の場合は Codex の既定メタデータが使われます。</p>
    <label className="block text-sm" htmlFor={`${idPrefix}-context-window`}>コンテキスト長
      <input id={`${idPrefix}-context-window`} type="number" min="1" className={fieldClass} value={value.context_window ?? ''} onChange={e => onChange({ context_window: e.target.value ? Number(e.target.value) : null })} placeholder="例: 128000" />
    </label>
    <label className="block text-sm" htmlFor={`${idPrefix}-auto-compact-token-limit`}>自動圧縮開始トークン数
      <input id={`${idPrefix}-auto-compact-token-limit`} type="number" min="1" className={fieldClass} value={value.auto_compact_token_limit ?? ''} onChange={e => onChange({ auto_compact_token_limit: e.target.value ? Number(e.target.value) : null })} placeholder="例: 64000" />
    </label>
    <p className="text-xs text-gray-500">自動圧縮開始トークン数は、コンテキスト長より小さい値を指定してください。</p>
    <label className="block text-sm" htmlFor={`${idPrefix}-reasoning-summaries`}>Reasoning summaries
      <select id={`${idPrefix}-reasoning-summaries`} className={fieldClass} value={value.supports_reasoning_summaries == null ? '' : String(value.supports_reasoning_summaries)} onChange={e => onChange({ supports_reasoning_summaries: e.target.value === '' ? null : e.target.value === 'true' })}>
        <option value="">未指定</option><option value="true">対応</option><option value="false">非対応</option>
      </select>
    </label>
  </div>
}
