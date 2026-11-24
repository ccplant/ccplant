'use client'

import type { useSessionProfileEditor } from './useSessionProfileEditor'
import type { SessionProfileEditorProps } from './SessionProfileEditor'

type Props = Pick<ReturnType<typeof useSessionProfileEditor>, 'name' | 'pool' | 'setPool' | 'availablePools'> &
  Pick<SessionProfileEditorProps, never>

export function ProfilePoolSection({ pool, setPool, availablePools }: Props) {
  return (
    <div className="space-y-5">
      {/* Session Runner Pool */}
      <div>
        <label
          htmlFor="session-profile-pool"
          className="block text-sm font-medium text-gray-700 dark:text-gray-300 mb-1"
        >
          Session Runner Pool
        </label>
        <p className="text-xs text-gray-500 dark:text-gray-400 mb-2">
          このプロファイルで作成するセッションの実行先Poolを指定します。空欄の場合は自動選択されます。
        </p>
        <select
          id="session-profile-pool"
          value={pool}
          onChange={(e) => setPool(e.target.value)}
          className="w-full px-3 py-2 border border-gray-300 dark:border-gray-600 rounded-lg text-sm bg-white dark:bg-gray-700 text-gray-900 dark:text-white focus:outline-none focus:ring-2 focus:ring-blue-500 dark:focus:ring-blue-400"
        >
          <option value="">自動選択</option>
          {pool && !availablePools.some((availablePool) => availablePool.name === pool) && (
            <option value={pool}>{pool}（現在の設定）</option>
          )}
          {availablePools.map((availablePool) => (
            <option key={availablePool.name} value={availablePool.name}>
              {availablePool.name}
            </option>
          ))}
        </select>
        {availablePools.length === 0 && (
          <p className="mt-1 text-xs text-gray-400 dark:text-gray-500">選択可能なPool候補がありません。</p>
        )}
      </div>
    </div>
  )
}
