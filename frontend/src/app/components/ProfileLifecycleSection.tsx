'use client'

import type { useSessionProfileEditor } from './useSessionProfileEditor'
import type { SessionProfileEditorProps } from './SessionProfileEditor'

type Props = Pick<ReturnType<typeof useSessionProfileEditor>, 'sessionTTL' | 'setSessionTTL'> &
  Pick<SessionProfileEditorProps, never>

export function ProfileLifecycleSection({ sessionTTL, setSessionTTL }: Props) {
  return (
    <div className="space-y-5">
      {/* セッション TTL */}
      <div>
        <label className="block text-sm font-medium text-gray-700 dark:text-gray-300 mb-1">
          セッション自動削除 TTL
        </label>
        <p className="text-xs text-gray-500 dark:text-gray-400 mb-2">
          処理が終了してからこの時間が経過するとセッションを自動削除します。実行中は削除されず、次の処理が終了すると再計測します。例:{' '}
          <code className="px-1 py-0.5 bg-gray-100 dark:bg-gray-700 rounded">24h</code>、
          <code className="px-1 py-0.5 bg-gray-100 dark:bg-gray-700 rounded">168h</code>（空欄 = 自動削除なし /
          グローバル設定に従う）
        </p>
        <input
          type="text"
          value={sessionTTL}
          onChange={(e) => setSessionTTL(e.target.value)}
          placeholder="例: 24h、72h、168h"
          className="w-full px-3 py-2 text-sm border border-gray-300 dark:border-gray-600 rounded-lg focus:outline-none focus:ring-2 focus:ring-blue-500 dark:bg-gray-700 dark:text-white"
        />
      </div>
    </div>
  )
}
