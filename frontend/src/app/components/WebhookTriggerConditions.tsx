'use client'

import { GITHUB_EVENTS } from '../../types/webhook'
import type { useWebhookForm } from './useWebhookForm'
import type { TriggerFormData } from './webhookFormTypes'

export function WebhookTriggerGitHubConditions(
  props: ReturnType<typeof useWebhookForm> & { trigger: TriggerFormData; index: number }
) {
  const { webhookType, updateTrigger, toggleTriggerEvent, toggleTriggerAction, getAvailableActions, trigger, index } =
    props
  return (
    <>
      {webhookType === 'github' && (
        <>
          {/* Events */}
          <div>
            <label className="block text-sm font-medium text-gray-700 dark:text-gray-300 mb-2">イベント</label>
            <div className="flex flex-wrap gap-2">
              {GITHUB_EVENTS.map((event) => (
                <button
                  key={event.value}
                  type="button"
                  onClick={() => toggleTriggerEvent(index, event.value)}
                  className={`px-2.5 py-1 text-xs rounded border transition-colors ${
                    trigger.events.includes(event.value)
                      ? 'bg-green-100 dark:bg-green-900/30 border-green-300 dark:border-green-700 text-green-700 dark:text-green-300'
                      : 'bg-gray-50 dark:bg-gray-700 border-gray-200 dark:border-gray-600 text-gray-600 dark:text-gray-400 hover:bg-gray-100 dark:hover:bg-gray-600'
                  }`}
                >
                  {event.label}
                </button>
              ))}
            </div>
          </div>

          {/* Actions */}
          {trigger.events.length > 0 && (
            <div>
              <label className="block text-sm font-medium text-gray-700 dark:text-gray-300 mb-2">アクション</label>
              <div className="flex flex-wrap gap-2">
                {getAvailableActions(trigger.events).map((action) => (
                  <button
                    key={action.value}
                    type="button"
                    onClick={() => toggleTriggerAction(index, action.value)}
                    className={`px-2.5 py-1 text-xs rounded border transition-colors ${
                      trigger.actions.includes(action.value)
                        ? 'bg-purple-100 dark:bg-purple-900/30 border-purple-300 dark:border-purple-700 text-purple-700 dark:text-purple-300'
                        : 'bg-gray-50 dark:bg-gray-700 border-gray-200 dark:border-gray-600 text-gray-600 dark:text-gray-400 hover:bg-gray-100 dark:hover:bg-gray-600'
                    }`}
                  >
                    {action.label}
                  </button>
                ))}
              </div>
            </div>
          )}

          {/* Base Branches */}
          <div>
            <label className="block text-sm font-medium text-gray-700 dark:text-gray-300 mb-1">ベースブランチ</label>
            <input
              type="text"
              value={trigger.baseBranches.join(', ')}
              onChange={(e) =>
                updateTrigger(
                  index,
                  'baseBranches',
                  e.target.value
                    .split(',')
                    .map((b) => b.trim())
                    .filter((b) => b)
                )
              }
              placeholder="例: main, develop"
              className="w-full px-3 py-2 border border-gray-300 dark:border-gray-600 rounded-md focus:outline-none focus:ring-2 focus:ring-blue-500 dark:bg-gray-700 dark:text-white text-sm"
            />
            <p className="text-xs text-gray-500 dark:text-gray-400 mt-1">
              PRのベースブランチでフィルタ（カンマ区切り）
            </p>
          </div>

          {/* Go Template Matcher for GitHub */}
          <div>
            <label className="block text-sm font-medium text-gray-700 dark:text-gray-300 mb-1">
              Go Template Matcher（追加フィルター）
            </label>
            <textarea
              value={trigger.goTemplate || ''}
              onChange={(e) => updateTrigger(index, 'goTemplate', e.target.value)}
              placeholder={'例: {{ and (eq .action "opened") (not .pull_request.draft) }}'}
              rows={3}
              className="w-full px-3 py-2 border border-gray-300 dark:border-gray-600 rounded-md focus:outline-none focus:ring-2 focus:ring-blue-500 dark:bg-gray-700 dark:text-white text-sm font-mono resize-y"
            />
            <p className="text-xs text-gray-500 dark:text-gray-400 mt-1">
              より高度な条件判定が必要な場合に使用します。trueまたはfalseを返すGo template式を記述してください。
            </p>
            <p className="text-xs text-gray-500 dark:text-gray-400 mt-1">
              例: {'{{ contains .pull_request.title "feat:" }}'},{' '}
              {'{{ and (eq .action "opened") (not .pull_request.draft) }}'}
            </p>
          </div>
        </>
      )}
    </>
  )
}

export function WebhookTriggerCustomConditions(
  props: ReturnType<typeof useWebhookForm> & { trigger: TriggerFormData; index: number }
) {
  const { webhookType, updateTrigger, trigger, index } = props
  return (
    <>
      {webhookType === 'custom' && (
        <>
          {/* Go Template */}
          <div>
            <label className="block text-sm font-medium text-gray-700 dark:text-gray-300 mb-1">
              Go Template Matcher
            </label>
            <textarea
              value={trigger.goTemplate || ''}
              onChange={(e) => updateTrigger(index, 'goTemplate', e.target.value)}
              placeholder={'例: {{ eq .event.type "push" }}'}
              rows={3}
              className="w-full px-3 py-2 border border-gray-300 dark:border-gray-600 rounded-md focus:outline-none focus:ring-2 focus:ring-blue-500 dark:bg-gray-700 dark:text-white text-sm font-mono resize-y"
            />
            <p className="text-xs text-gray-500 dark:text-gray-400 mt-1">
              Goテンプレート式。trueまたはfalseを返す式を記述してください。
            </p>
            <p className="text-xs text-gray-500 dark:text-gray-400 mt-1">
              例: {'{{ eq .event.type "push" }}'}, {'{{ and (eq .action "opened") (contains .title "fix") }}'}
            </p>
          </div>
        </>
      )}
    </>
  )
}
