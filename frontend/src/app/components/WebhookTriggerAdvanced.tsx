'use client'

import type { useWebhookForm } from './useWebhookForm'
import type { TriggerFormData } from './webhookFormTypes'

export function WebhookTriggerEnvironment(
  props: ReturnType<typeof useWebhookForm> & { trigger: TriggerFormData; index: number }
) {
  const { addKeyValuePair, updateKeyValuePair, removeKeyValuePair, trigger, index } = props
  return (
    <div>
      <div className="flex items-center justify-between mb-2">
        <label className="block text-sm font-medium text-gray-700 dark:text-gray-300">環境変数</label>
        <button
          type="button"
          onClick={() => addKeyValuePair(index, 'environment')}
          className="text-xs text-blue-600 dark:text-blue-400 hover:text-blue-800 dark:hover:text-blue-300 flex items-center gap-1"
        >
          <svg className="w-3 h-3" fill="none" stroke="currentColor" viewBox="0 0 24 24">
            <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M12 6v6m0 0v6m0-6h6m-6 0H6" />
          </svg>
          追加
        </button>
      </div>
      {Object.entries(trigger.environment).length > 0 ? (
        <div className="space-y-2">
          {Object.entries(trigger.environment).map(([key, value], entryIndex) => (
            <div key={`environment-${entryIndex}`} className="flex flex-col sm:flex-row gap-2">
              <input
                type="text"
                value={key}
                onChange={(e) => updateKeyValuePair(index, 'environment', key, e.target.value, value)}
                placeholder="KEY"
                className="w-full sm:w-1/3 px-2 py-1.5 border border-gray-300 dark:border-gray-600 rounded text-xs font-mono dark:bg-gray-700 dark:text-white"
              />
              <input
                type="text"
                value={value}
                onChange={(e) => updateKeyValuePair(index, 'environment', key, key, e.target.value)}
                placeholder="value (テンプレート可)"
                className="flex-1 px-2 py-1.5 border border-gray-300 dark:border-gray-600 rounded text-xs font-mono dark:bg-gray-700 dark:text-white"
              />
              <button
                type="button"
                onClick={() => removeKeyValuePair(index, 'environment', key)}
                className="text-red-500 hover:text-red-700 px-2 self-start sm:self-center"
              >
                <svg className="w-4 h-4" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                  <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M6 18L18 6M6 6l12 12" />
                </svg>
              </button>
            </div>
          ))}
        </div>
      ) : (
        <p className="text-xs text-gray-500 dark:text-gray-400">環境変数は設定されていません</p>
      )}
      <p className="text-xs text-gray-500 dark:text-gray-400 mt-1">
        値にGoテンプレート構文を使用できます。例: {'{{.pull_request.number}}'}
      </p>
    </div>
  )
}

export function WebhookTriggerTags(
  props: ReturnType<typeof useWebhookForm> & { trigger: TriggerFormData; index: number }
) {
  const { addKeyValuePair, updateKeyValuePair, removeKeyValuePair, trigger, index } = props
  return (
    <div>
      <div className="flex items-center justify-between mb-2">
        <label className="block text-sm font-medium text-gray-700 dark:text-gray-300">タグ</label>
        <button
          type="button"
          onClick={() => addKeyValuePair(index, 'tags')}
          className="text-xs text-blue-600 dark:text-blue-400 hover:text-blue-800 dark:hover:text-blue-300 flex items-center gap-1"
        >
          <svg className="w-3 h-3" fill="none" stroke="currentColor" viewBox="0 0 24 24">
            <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M12 6v6m0 0v6m0-6h6m-6 0H6" />
          </svg>
          追加
        </button>
      </div>
      {Object.entries(trigger.tags).length > 0 ? (
        <div className="space-y-2">
          {Object.entries(trigger.tags).map(([key, value], entryIndex) => (
            <div key={`tags-${entryIndex}`} className="flex flex-col sm:flex-row gap-2">
              <input
                type="text"
                value={key}
                onChange={(e) => updateKeyValuePair(index, 'tags', key, e.target.value, value)}
                placeholder="key"
                className="w-full sm:w-1/3 px-2 py-1.5 border border-gray-300 dark:border-gray-600 rounded text-xs font-mono dark:bg-gray-700 dark:text-white"
              />
              <input
                type="text"
                value={value}
                onChange={(e) => updateKeyValuePair(index, 'tags', key, key, e.target.value)}
                placeholder="value (テンプレート可)"
                className="flex-1 px-2 py-1.5 border border-gray-300 dark:border-gray-600 rounded text-xs font-mono dark:bg-gray-700 dark:text-white"
              />
              <button
                type="button"
                onClick={() => removeKeyValuePair(index, 'tags', key)}
                className="text-red-500 hover:text-red-700 px-2 self-start sm:self-center"
              >
                <svg className="w-4 h-4" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                  <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M6 18L18 6M6 6l12 12" />
                </svg>
              </button>
            </div>
          ))}
        </div>
      ) : (
        <p className="text-xs text-gray-500 dark:text-gray-400">タグは設定されていません</p>
      )}
      <p className="text-xs text-gray-500 dark:text-gray-400 mt-1">
        値にGoテンプレート構文を使用できます。例: {'{{.pull_request.number}}'}
      </p>
    </div>
  )
}

export function WebhookTriggerAdvancedSettings(
  props: ReturnType<typeof useWebhookForm> & { trigger: TriggerFormData; index: number }
) {
  const state = props
  const { trigger, index } = props
  return (
    <>
      {trigger.showAdvanced && (
        <div className="space-y-4 pl-6 border-l-2 border-gray-200 dark:border-gray-700">
          {/* Environment Variables */}
          <WebhookTriggerEnvironment {...state} trigger={trigger} index={index} />

          {/* Tags */}
          <WebhookTriggerTags {...state} trigger={trigger} index={index} />
        </div>
      )}
    </>
  )
}
