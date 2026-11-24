'use client'

import type { useWebhookForm } from './useWebhookForm'
import { WebhookTriggerCard } from './WebhookTriggerCard'

export function WebhookTriggers(props: ReturnType<typeof useWebhookForm>) {
  const state = props
  const { triggers, addTrigger } = props
  return (
    <div className="space-y-4">
      <div className="flex items-center justify-between border-b border-gray-200 dark:border-gray-700 pb-2">
        <h3 className="text-sm font-medium text-gray-700 dark:text-gray-300">トリガー</h3>
        <button
          type="button"
          onClick={addTrigger}
          className="text-sm text-blue-600 dark:text-blue-400 hover:text-blue-800 dark:hover:text-blue-300 flex items-center gap-1"
        >
          <svg className="w-4 h-4" fill="none" stroke="currentColor" viewBox="0 0 24 24">
            <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M12 6v6m0 0v6m0-6h6m-6 0H6" />
          </svg>
          トリガーを追加
        </button>
      </div>

      <div className="space-y-3">
        {triggers.map((trigger, index) => (
          <WebhookTriggerCard {...state} trigger={trigger} index={index} key={index} />
        ))}
      </div>
    </div>
  )
}
