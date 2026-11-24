'use client'
import type { NewSessionFormState } from './useNewSession'

export function NewSessionTTLField(context: NewSessionFormState) {
  const { isCreating, sessionTTL, setSessionTTL } = context
  return (
    <div>
      <p className="text-xs font-medium text-gray-600 dark:text-gray-400 mb-1">セッション自動削除 TTL</p>
      <p className="text-xs text-gray-400 dark:text-gray-500 mb-2">
        処理が終了してからこの時間が経過するとセッションを自動削除します。実行中は削除されず、次の処理が終了すると再計測します。例:{' '}
        <code className="px-1 py-0.5 bg-gray-100 dark:bg-gray-700 rounded">24h</code>、
        <code className="px-1 py-0.5 bg-gray-100 dark:bg-gray-700 rounded">168h</code>（空欄 = 自動削除なし）
      </p>
      <input
        type="text"
        value={sessionTTL}
        onChange={(e) => setSessionTTL(e.target.value)}
        placeholder="例: 24h、72h、168h"
        disabled={isCreating}
        className="w-full px-3 py-2 text-sm border border-gray-300 dark:border-gray-600 rounded-lg focus:outline-none focus:ring-2 focus:ring-blue-500 dark:bg-gray-700 dark:text-white"
      />
    </div>
  )
}
