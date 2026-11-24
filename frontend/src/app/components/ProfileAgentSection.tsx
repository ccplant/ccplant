'use client'

import type { useSessionProfileEditor } from './useSessionProfileEditor'
import type { SessionProfileEditorProps } from './SessionProfileEditor'

type Props = Pick<ReturnType<typeof useSessionProfileEditor>, 'name' | 'description' | 'agentType' | 'setAgentType'> &
  Pick<SessionProfileEditorProps, never>

export function ProfileAgentSection({ agentType, setAgentType }: Props) {
  return (
    <div className="space-y-5">
      {/* Agent Type */}
      <div>
        <label className="block text-sm font-medium text-gray-700 dark:text-gray-300 mb-2">エージェントタイプ</label>
        <div className="space-y-2">
          {[
            { value: '', label: 'デフォルト', description: '継承元のエージェント設定を使用します' },
            {
              value: 'auto',
              label: '自動選択',
              description: 'Codex の互換 API または auth.json があれば Codex ACP、それ以外は Claude ACP',
            },
            { value: 'claude-legacy', label: 'Claude Legacy', description: 'Claude の従来の実行方式' },
            { value: 'claude-acp', label: 'Claude ACP', description: 'Claude Code を使用します' },
            { value: 'codex-acp', label: 'Codex ACP', description: 'Codex を使用します' },
            { value: 'pi', label: 'Pi', description: '登録した互換 API に接続する Pi を使用します' },
            { value: 'cursor', label: 'Cursor ACP', description: 'Cursor を使用します' },
            { value: 'antigravity', label: 'Antigravity', description: 'Google Antigravity を使用します' },
          ].map(({ value: v, label, description }) => (
            <label key={v} className="flex items-start cursor-pointer group">
              <input
                type="radio"
                name="profile-agent-type"
                value={v}
                checked={agentType === v}
                onChange={() => setAgentType(v)}
                className="mt-0.5 w-3.5 h-3.5 text-blue-600 border-gray-300 dark:border-gray-600 focus:ring-blue-500"
              />
              <span className="ml-2">
                <span className="block text-xs font-medium text-gray-600 dark:text-gray-400 group-hover:text-gray-800 dark:group-hover:text-gray-200">
                  {label}
                </span>
                <span className="block text-xs text-gray-400 dark:text-gray-500 mt-0.5">{description}</span>
              </span>
            </label>
          ))}
        </div>
      </div>
    </div>
  )
}
