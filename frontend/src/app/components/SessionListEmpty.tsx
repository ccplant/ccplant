'use client'
import type { SessionListState } from './useSessionListViewState'
export function SessionListEmpty(context: SessionListState) {
  const { creatingSessions, sessions, showHiddenSessions, setShowHiddenSessions, hiddenSessionsCount } = context

  return (
    <div className="text-center py-12 bg-white dark:bg-gray-800 rounded-lg border border-gray-200 dark:border-gray-700">
      <div className="text-gray-400 dark:text-gray-500 mb-2">
        <svg className="w-12 h-12 mx-auto" fill="none" stroke="currentColor" viewBox="0 0 24 24">
          <path
            strokeLinecap="round"
            strokeLinejoin="round"
            strokeWidth={2}
            d="M9 12h6m-6 4h6m2 5H7a2 2 0 01-2-2V5a2 2 0 012-2h5.586a1 1 0 01.707.293l5.414 5.414a1 1 0 01.293.707V19a2 2 0 01-2 2z"
          />
        </svg>
      </div>
      <h3 className="text-lg font-medium text-gray-900 dark:text-white mb-1">セッションが見つかりません</h3>
      <p className="text-gray-500 dark:text-gray-400">
        {sessions.length === 0 && creatingSessions.length === 0
          ? '新しいセッションを開始してください。'
          : 'フィルタ条件に一致するセッションがありません。'}
      </p>
      {hiddenSessionsCount > 0 && !showHiddenSessions && (
        <div className="mt-3">
          <button
            onClick={() => setShowHiddenSessions(true)}
            className="inline-flex items-center gap-1.5 text-sm text-gray-400 dark:text-gray-500 hover:text-gray-600 dark:hover:text-gray-400"
          >
            <svg className="w-4 h-4" fill="none" stroke="currentColor" viewBox="0 0 24 24">
              <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M15 12a3 3 0 11-6 0 3 3 0 016 0z" />
              <path
                strokeLinecap="round"
                strokeLinejoin="round"
                strokeWidth={2}
                d="M2.458 12C3.732 7.943 7.523 5 12 5c4.478 0 8.268 2.943 9.542 7-1.274 4.057-5.064 7-9.542 7-4.477 0-8.268-2.943-9.542-7z"
              />
            </svg>
            非表示セッションを表示する ({hiddenSessionsCount}件)
          </button>
        </div>
      )}
    </div>
  )
}
