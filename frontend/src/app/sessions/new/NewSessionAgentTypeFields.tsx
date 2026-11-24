'use client'
import { AgentApiType, getACPServerEnabled } from '../../../types/settings'
import { getAgentTypeLabel } from './newSessionPresentation'
import type { NewSessionFormState } from './useNewSession'

export function NewSessionAgentTypeFields(context: NewSessionFormState) {
  const { isCreating, selectedAgentType, setSelectedAgentType } = context
  return (
    <div>
      <p className="text-xs font-medium text-gray-600 dark:text-gray-400 mb-2">エージェントタイプ</p>
      {getACPServerEnabled() ? (
        <div className="flex items-center space-x-2 px-3 py-2 bg-blue-50 dark:bg-blue-900/20 border border-blue-200 dark:border-blue-700 rounded-md">
          <svg className="w-4 h-4 text-blue-500 flex-shrink-0" fill="none" stroke="currentColor" viewBox="0 0 24 24">
            <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M13 10V3L4 14h7v7l9-11h-7z" />
          </svg>
          <span className="text-xs text-blue-700 dark:text-blue-300">
            ACP サーバーモード: <strong>{getAgentTypeLabel(selectedAgentType)}</strong>（
            {selectedAgentType !== 'default' ? selectedAgentType : 'auto'}）が使用されます
          </span>
        </div>
      ) : (
        <div className="space-y-2">
          {(
            [
              { value: 'default', label: 'チーム設定', description: 'チーム設定（未設定なら自動選択）を使用' },
              {
                value: 'auto',
                label: '自動選択',
                description: 'Codex の互換 API または auth.json があれば Codex ACP、それ以外は Claude ACP',
              },
              { value: 'claude-legacy', label: 'Claude Legacy', description: 'agent_type=claude-legacy を送信' },
              { value: 'claude-acp', label: 'Claude ACP', description: 'agent_type=claude-acp を送信' },
              { value: 'codex-acp', label: 'Codex ACP', description: 'agent_type=codex-acp を送信' },
              { value: 'pi', label: 'Pi', description: 'agent_type=pi を送信' },
              { value: 'cursor', label: 'Cursor ACP', description: 'agent_type=cursor を送信' },
              { value: 'antigravity', label: 'Antigravity', description: 'agent_type=antigravity を送信' },
            ] as { value: AgentApiType; label: string; description: string }[]
          ).map(({ value, label, description }) => (
            <label key={value} className="flex items-start cursor-pointer group">
              <input
                type="radio"
                name="session-agent-type"
                value={value}
                checked={selectedAgentType === value}
                onChange={() => setSelectedAgentType(value)}
                className="mt-0.5 w-3.5 h-3.5 text-blue-600 border-gray-300 dark:border-gray-600 focus:ring-blue-500"
                disabled={isCreating}
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
      )}
    </div>
  )
}
