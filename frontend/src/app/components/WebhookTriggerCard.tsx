'use client'

import type { useWebhookForm } from './useWebhookForm'
import type { TriggerFormData } from './webhookFormTypes'
import { WebhookTriggerAdvancedSettings } from './WebhookTriggerAdvanced'
import { WebhookTriggerCustomConditions, WebhookTriggerGitHubConditions } from './WebhookTriggerConditions'
import { WebhookTriggerInitialMessage, WebhookTriggerModel, WebhookTriggerProfile } from './WebhookTriggerMessage'
import {
  WebhookTriggerCredentials,
  WebhookTriggerMountPayload,
  WebhookTriggerOneshot,
  WebhookTriggerReuseMessage,
  WebhookTriggerReuseSession,
  WebhookTriggerTTL,
} from './WebhookTriggerRuntime'

export function WebhookTriggerBody(
  props: ReturnType<typeof useWebhookForm> & { trigger: TriggerFormData; index: number }
) {
  const state = props
  const { expandedTrigger, updateTrigger, trigger, index } = props
  return (
    <>
      {expandedTrigger === index && (
        <div className="px-4 py-4 space-y-4">
          {/* Trigger Name */}
          <div>
            <label className="block text-sm font-medium text-gray-700 dark:text-gray-300 mb-1">トリガー名 *</label>
            <input
              type="text"
              value={trigger.name}
              onChange={(e) => updateTrigger(index, 'name', e.target.value)}
              placeholder="例: Review PR on open"
              className="w-full px-3 py-2 border border-gray-300 dark:border-gray-600 rounded-md focus:outline-none focus:ring-2 focus:ring-blue-500 dark:bg-gray-700 dark:text-white text-sm"
            />
          </div>

          {/* Enabled Toggle */}
          <div className="flex items-center gap-2">
            <input
              type="checkbox"
              id={`trigger-enabled-${index}`}
              checked={trigger.enabled}
              onChange={(e) => updateTrigger(index, 'enabled', e.target.checked)}
              className="rounded border-gray-300 text-blue-600 focus:ring-blue-500"
            />
            <label htmlFor={`trigger-enabled-${index}`} className="text-sm text-gray-700 dark:text-gray-300">
              このトリガーを有効にする
            </label>
          </div>

          {/* GitHub Conditions */}
          <WebhookTriggerGitHubConditions {...state} trigger={trigger} index={index} />

          {/* Custom Webhook Conditions */}
          <WebhookTriggerCustomConditions {...state} trigger={trigger} index={index} />

          {/* Initial Message Template */}
          <WebhookTriggerInitialMessage {...state} trigger={trigger} index={index} />

          {/* Model */}
          <WebhookTriggerModel {...state} trigger={trigger} index={index} />

          {/* Session Profile */}
          <WebhookTriggerProfile {...state} trigger={trigger} index={index} />

          {/* Advanced Settings Toggle */}
          <div>
            <button
              type="button"
              onClick={() => updateTrigger(index, 'showAdvanced', !trigger.showAdvanced)}
              className="flex items-center gap-2 text-sm text-gray-700 dark:text-gray-300 hover:text-gray-900 dark:hover:text-white"
            >
              <svg
                className={`w-4 h-4 transition-transform ${trigger.showAdvanced ? 'rotate-90' : ''}`}
                fill="none"
                stroke="currentColor"
                viewBox="0 0 24 24"
              >
                <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M9 5l7 7-7 7" />
              </svg>
              高度な設定
            </button>
          </div>

          {/* Advanced Settings Content */}
          <WebhookTriggerAdvancedSettings {...state} trigger={trigger} index={index} />

          {/* Reuse Session Toggle */}
          <WebhookTriggerReuseSession {...state} trigger={trigger} index={index} />
          <p className="text-xs text-gray-500 dark:text-gray-400 -mt-3 ml-6">
            同じ webhook と trigger
            の組み合わせで実行中のセッションがある場合、新しいセッションを作成せずに既存のセッションにメッセージを送信します
          </p>

          {/* Mount Payload Toggle */}
          <WebhookTriggerMountPayload {...state} trigger={trigger} index={index} />
          <p className="text-xs text-gray-500 dark:text-gray-400 -mt-3 ml-6">
            有効にすると、Webhookペイロードが /opt/webhook/payload.json としてセッションコンテナにマウントされます
          </p>

          {/* Oneshot Toggle */}
          <WebhookTriggerOneshot {...state} trigger={trigger} index={index} />
          <p className="text-xs text-gray-500 dark:text-gray-400 -mt-3 ml-6">
            有効にすると、セッション終了後に自動的に削除されます
          </p>

          {/* Session TTL */}
          <WebhookTriggerTTL {...state} trigger={trigger} index={index} />

          {/* Credential Source */}
          <WebhookTriggerCredentials {...state} trigger={trigger} index={index} />

          {/* Reuse Message Template */}
          <WebhookTriggerReuseMessage {...state} trigger={trigger} index={index} />
        </div>
      )}
    </>
  )
}

export function WebhookTriggerCard(
  props: ReturnType<typeof useWebhookForm> & { trigger: TriggerFormData; index: number }
) {
  const state = props
  const { triggers, expandedTrigger, setExpandedTrigger, removeTrigger, trigger, index } = props
  return (
    <div key={index} className="border border-gray-200 dark:border-gray-700 rounded-md overflow-hidden">
      {/* Trigger Header */}
      <div
        className="flex items-center justify-between px-4 py-3 bg-gray-50 dark:bg-gray-700/50 cursor-pointer"
        onClick={() => setExpandedTrigger(expandedTrigger === index ? null : index)}
      >
        <div className="flex items-center gap-3">
          <svg
            className={`w-4 h-4 text-gray-500 transition-transform ${expandedTrigger === index ? 'rotate-90' : ''}`}
            fill="none"
            stroke="currentColor"
            viewBox="0 0 24 24"
          >
            <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M9 5l7 7-7 7" />
          </svg>
          <span className="text-sm font-medium text-gray-700 dark:text-gray-300">
            {trigger.name || `トリガー ${index + 1}`}
          </span>
          {!trigger.enabled && (
            <span className="text-xs px-2 py-0.5 bg-yellow-100 dark:bg-yellow-900/30 text-yellow-700 dark:text-yellow-300 rounded">
              無効
            </span>
          )}
        </div>
        {triggers.length > 1 && (
          <button
            type="button"
            onClick={(e) => {
              e.stopPropagation()
              removeTrigger(index)
            }}
            className="text-red-500 hover:text-red-700 dark:hover:text-red-400"
          >
            <svg className="w-4 h-4" fill="none" stroke="currentColor" viewBox="0 0 24 24">
              <path
                strokeLinecap="round"
                strokeLinejoin="round"
                strokeWidth={2}
                d="M19 7l-.867 12.142A2 2 0 0116.138 21H7.862a2 2 0 01-1.995-1.858L5 7m5 4v6m4-6v6m1-10V4a1 1 0 00-1-1h-4a1 1 0 00-1 1v3M4 7h16"
              />
            </svg>
          </button>
        )}
      </div>

      {/* Trigger Body */}
      <WebhookTriggerBody {...state} trigger={trigger} index={index} />
    </div>
  )
}
