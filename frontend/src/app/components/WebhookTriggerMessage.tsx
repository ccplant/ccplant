'use client'

import SessionProfileSelect from './SessionProfileSelect'
import type { useWebhookForm } from './useWebhookForm'
import type { TriggerFormData } from './webhookFormTypes'

export function WebhookTriggerInitialMessage(
  props: ReturnType<typeof useWebhookForm> & { trigger: TriggerFormData; index: number }
) {
  const { updateTrigger, trigger, index } = props
  return (
    <div>
      <label className="block text-sm font-medium text-gray-700 dark:text-gray-300 mb-1">
        初期メッセージテンプレート
      </label>
      <textarea
        value={trigger.initialMessageTemplate}
        onChange={(e) => updateTrigger(index, 'initialMessageTemplate', e.target.value)}
        placeholder={'例: Review PR #{{.pull_request.Number}} in {{.repository.FullName}}'}
        rows={2}
        className="w-full px-3 py-2 border border-gray-300 dark:border-gray-600 rounded-md focus:outline-none focus:ring-2 focus:ring-blue-500 dark:bg-gray-700 dark:text-white text-sm font-mono resize-y"
      />
      <p className="text-xs text-gray-500 dark:text-gray-400 mt-1">
        Goテンプレート形式。例: {'{{.pull_request.Number}}'}
      </p>
    </div>
  )
}

export function WebhookTriggerModel(
  props: ReturnType<typeof useWebhookForm> & { trigger: TriggerFormData; index: number }
) {
  const { isSubmitting, updateTrigger, trigger, index } = props
  return (
    <div>
      <label
        htmlFor={`trigger-model-${index}`}
        className="block text-sm font-medium text-gray-700 dark:text-gray-300 mb-1"
      >
        モデル
      </label>
      <input
        id={`trigger-model-${index}`}
        type="text"
        value={trigger.model}
        onChange={(e) => updateTrigger(index, 'model', e.target.value)}
        placeholder="例: gpt-5.4、claude-sonnet-4-6"
        disabled={isSubmitting}
        className="w-full px-3 py-2 border border-gray-300 dark:border-gray-600 rounded-md focus:outline-none focus:ring-2 focus:ring-blue-500 dark:bg-gray-700 dark:text-white"
      />
      <p className="text-xs text-gray-500 dark:text-gray-400 mt-1">
        このトリガーから起動するセッションのモデルを指定します。空欄の場合はセッションプロファイルまたは既定値を使用します。
      </p>
    </div>
  )
}

export function WebhookTriggerProfile(
  props: ReturnType<typeof useWebhookForm> & { trigger: TriggerFormData; index: number }
) {
  const { isSubmitting, updateTrigger, trigger, index } = props
  return (
    <div>
      <label className="block text-sm font-medium text-gray-700 dark:text-gray-300 mb-1">セッションプロファイル</label>
      <SessionProfileSelect
        value={trigger.sessionProfileId}
        onChange={(val) => updateTrigger(index, 'sessionProfileId', val)}
        disabled={isSubmitting}
      />
      <p className="text-xs text-gray-500 dark:text-gray-400 mt-1">
        プロファイルを選択すると、環境変数・タグ・テンプレートなどの設定を適用します
      </p>
    </div>
  )
}
