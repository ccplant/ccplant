'use client'

import type { useWebhookForm } from './useWebhookForm'
import type { TriggerFormData } from './webhookFormTypes'

export function WebhookTriggerReuseSession(
  props: ReturnType<typeof useWebhookForm> & { trigger: TriggerFormData; index: number }
) {
  const { updateTrigger, trigger, index } = props
  return (
    <div className="flex items-center gap-2">
      <input
        type="checkbox"
        id={`trigger-reuse-session-${index}`}
        checked={trigger.reuseSession}
        onChange={(e) => updateTrigger(index, 'reuseSession', e.target.checked)}
        className="rounded border-gray-300 text-blue-600 focus:ring-blue-500"
      />
      <label htmlFor={`trigger-reuse-session-${index}`} className="text-sm text-gray-700 dark:text-gray-300">
        セッションを再利用する
      </label>
    </div>
  )
}

export function WebhookTriggerMountPayload(
  props: ReturnType<typeof useWebhookForm> & { trigger: TriggerFormData; index: number }
) {
  const { updateTrigger, trigger, index } = props
  return (
    <div className="flex items-center gap-2">
      <input
        type="checkbox"
        id={`trigger-mount-payload-${index}`}
        checked={trigger.mountPayload}
        onChange={(e) => updateTrigger(index, 'mountPayload', e.target.checked)}
        className="rounded border-gray-300 text-blue-600 focus:ring-blue-500"
      />
      <label htmlFor={`trigger-mount-payload-${index}`} className="text-sm text-gray-700 dark:text-gray-300">
        Webhookペイロードをマウントする
      </label>
    </div>
  )
}

export function WebhookTriggerOneshot(
  props: ReturnType<typeof useWebhookForm> & { trigger: TriggerFormData; index: number }
) {
  const { updateTrigger, trigger, index } = props
  return (
    <div className="flex items-center gap-2">
      <input
        type="checkbox"
        id={`trigger-oneshot-${index}`}
        checked={trigger.oneshot}
        onChange={(e) => updateTrigger(index, 'oneshot', e.target.checked)}
        className="rounded border-gray-300 text-blue-600 focus:ring-blue-500"
      />
      <label htmlFor={`trigger-oneshot-${index}`} className="text-sm text-gray-700 dark:text-gray-300">
        ワンショットセッション
      </label>
    </div>
  )
}

export function WebhookTriggerTTL(
  props: ReturnType<typeof useWebhookForm> & { trigger: TriggerFormData; index: number }
) {
  const { isSubmitting, updateTrigger, trigger, index } = props
  return (
    <div>
      <label
        htmlFor={`trigger-session-ttl-${index}`}
        className="block text-sm font-medium text-gray-700 dark:text-gray-300 mb-1"
      >
        セッション自動削除 TTL
      </label>
      <input
        id={`trigger-session-ttl-${index}`}
        type="text"
        value={trigger.sessionTTL}
        onChange={(e) => updateTrigger(index, 'sessionTTL', e.target.value)}
        placeholder="例: 48h"
        disabled={isSubmitting}
        className="w-full px-3 py-2 border border-gray-300 dark:border-gray-600 rounded-md focus:outline-none focus:ring-2 focus:ring-blue-500 dark:bg-gray-700 dark:text-white text-sm font-mono"
      />
      <p className="text-xs text-gray-500 dark:text-gray-400 mt-1">
        このトリガーが作成したセッションを処理終了後に自動削除するまでの期間。Go duration 形式（例:
        30m、48h）で指定します
      </p>
    </div>
  )
}

export function WebhookTriggerCredentials(
  props: ReturnType<typeof useWebhookForm> & { trigger: TriggerFormData; index: number }
) {
  const { webhookType, updateCredentialSource, trigger, index } = props
  return (
    <div>
      <label
        htmlFor={`trigger-credential-source-${index}`}
        className="block text-sm font-medium text-gray-700 dark:text-gray-300 mb-1"
      >
        認証ファイルの参照元
      </label>
      <select
        id={`trigger-credential-source-${index}`}
        value={trigger.credentialSource}
        onChange={(e) => updateCredentialSource(index, e.target.value as TriggerFormData['credentialSource'])}
        className="w-full px-3 py-2 border border-gray-300 dark:border-gray-600 rounded-md text-sm dark:bg-gray-700 dark:text-white"
      >
        <option value="">デフォルト（team scopeでは認証ファイルなし）</option>
        <option value="triggered_user">Webhookをトリガーしたユーザー</option>
        <option value="team">チーム</option>
        <option value="session_user">Webhook設定の所有者</option>
        <option value="none">使用しない</option>
      </select>
      <p className="text-xs text-gray-500 dark:text-gray-400 mt-1">
        「Webhookをトリガーしたユーザー」を選ぶと、未設定の場合は username:{' '}
        {webhookType === 'github' ? '{{.sender.login}}' : '{{.user.login}}'}{' '}
        を自動追加します。認証ファイルが見つからない場合はチームへフォールバックします。
      </p>
    </div>
  )
}

export function WebhookTriggerReuseMessage(
  props: ReturnType<typeof useWebhookForm> & { trigger: TriggerFormData; index: number }
) {
  const { updateTrigger, trigger, index } = props
  return (
    <>
      {trigger.reuseSession && (
        <div>
          <label className="block text-sm font-medium text-gray-700 dark:text-gray-300 mb-1">
            セッション再利用時のメッセージテンプレート
          </label>
          <textarea
            value={trigger.reuseMessageTemplate || ''}
            onChange={(e) => updateTrigger(index, 'reuseMessageTemplate', e.target.value)}
            placeholder={'例: New update for PR #{{.pull_request.Number}}'}
            rows={2}
            className="w-full px-3 py-2 border border-gray-300 dark:border-gray-600 rounded-md focus:outline-none focus:ring-2 focus:ring-blue-500 dark:bg-gray-700 dark:text-white text-sm font-mono resize-y"
          />
          <p className="text-xs text-gray-500 dark:text-gray-400 mt-1">
            セッション再利用時に送信するメッセージ。未設定の場合、初期メッセージテンプレートが使用されます
          </p>
        </div>
      )}
    </>
  )
}
