'use client'
import type { NewSessionFormState } from './useNewSession'

export function NewSessionCycleFields(context: NewSessionFormState) {
  const { isCreating, cycleEnabled, setCycleEnabled, cycleMessage, setCycleMessage, cycleMaxCount, setCycleMaxCount } =
    context
  return (
    <div>
      <div className="flex items-center justify-between mb-2">
        <p className="text-xs font-medium text-gray-600 dark:text-gray-400">サイクルセッション</p>
        <label className="flex items-center gap-2 cursor-pointer">
          <span className="text-xs text-gray-400 dark:text-gray-500">{cycleEnabled ? '有効' : '無効'}</span>
          <button
            type="button"
            role="switch"
            aria-checked={cycleEnabled}
            onClick={() => setCycleEnabled(!cycleEnabled)}
            disabled={isCreating}
            className={`relative inline-flex h-5 w-9 flex-shrink-0 cursor-pointer rounded-full border-2 border-transparent transition-colors duration-200 ease-in-out focus:outline-none focus:ring-2 focus:ring-purple-500 focus:ring-offset-1 disabled:opacity-50 disabled:cursor-not-allowed ${
              cycleEnabled ? 'bg-purple-600' : 'bg-gray-300 dark:bg-gray-600'
            }`}
          >
            <span
              className={`pointer-events-none inline-block h-4 w-4 transform rounded-full bg-white shadow ring-0 transition duration-200 ease-in-out ${
                cycleEnabled ? 'translate-x-4' : 'translate-x-0'
              }`}
            />
          </button>
        </label>
      </div>
      <p className="text-xs text-gray-400 dark:text-gray-500 mb-3">
        Claudeが停止するたびに指定したメッセージを自動送信し、タスクを繰り返します。サイクルを終了する条件を満たした場合に自動的に停止します。
      </p>
      {cycleEnabled && (
        <div className="space-y-3 pl-1">
          <div>
            <label className="block text-xs font-medium text-gray-600 dark:text-gray-400 mb-1">
              サイクルメッセージ <span className="text-red-500">*</span>
            </label>
            <textarea
              value={cycleMessage}
              onChange={(e) => setCycleMessage(e.target.value)}
              placeholder="例: タスクを続けてください。サイクルを終了する条件: すべてのテストが通過したとき。"
              rows={3}
              disabled={isCreating}
              className="w-full px-3 py-2 text-xs border border-gray-300 dark:border-gray-600 rounded-lg focus:outline-none focus:ring-2 focus:ring-purple-500 dark:bg-gray-700 dark:text-white resize-y"
            />
            <p className="text-xs text-gray-400 dark:text-gray-500 mt-1">
              Claudeが停止するたびに送信されるメッセージです。サイクルを終了する条件を記載してください。
            </p>
          </div>
          <div>
            <label className="block text-xs font-medium text-gray-600 dark:text-gray-400 mb-1">最大サイクル数</label>
            <input
              type="number"
              min={0}
              value={cycleMaxCount}
              onChange={(e) => setCycleMaxCount(Math.max(0, parseInt(e.target.value) || 0))}
              disabled={isCreating}
              className="w-32 px-3 py-2 text-xs border border-gray-300 dark:border-gray-600 rounded-lg focus:outline-none focus:ring-2 focus:ring-purple-500 dark:bg-gray-700 dark:text-white"
            />
            <p className="text-xs text-gray-400 dark:text-gray-500 mt-1">
              0 の場合は無制限。
              <code className="px-1 py-0.5 bg-gray-100 dark:bg-gray-700 rounded">/tmp/check/CYCLE_COUNT</code>{' '}
              で進捗を確認できます。
            </p>
          </div>
        </div>
      )}
    </div>
  )
}
