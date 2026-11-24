'use client'
import type { SessionListState } from './useSessionListViewState'
export function SessionListToolbar(context: SessionListState) {
  const {
    creatingSessions,
    loading,
    setSelectedSessions,
    isSelectionMode,
    setIsSelectionMode,
    showHiddenSessions,
    setShowHiddenSessions,
    sortBy,
    setSortBy,
    sortOrder,
    setSortOrder,
    fetchSessions,
    hiddenSessionsCount,
    filteredSessions,
  } = context

  return (
    <div className="flex flex-col sm:flex-row sm:items-center justify-between gap-3 sm:gap-4">
      <div className="flex items-center gap-3">
        <h3 className="hidden sm:block text-lg font-medium text-gray-900 dark:text-white">
          セッション一覧 ({filteredSessions.length + creatingSessions.length})
        </h3>
      </div>
      <div className="flex items-center gap-2">
        {/* 選択モードトグルボタン */}
        <button
          onClick={() => {
            setIsSelectionMode((prev) => {
              if (prev) setSelectedSessions(new Set())
              return !prev
            })
          }}
          className={`inline-flex items-center gap-1.5 px-3 py-1.5 text-sm rounded-md border transition-colors font-medium ${
            isSelectionMode
              ? 'bg-blue-50 border-blue-400 text-blue-700 dark:bg-blue-900/30 dark:border-blue-500 dark:text-blue-300'
              : 'bg-gray-50 border-gray-200 text-gray-700 dark:bg-gray-800 dark:border-gray-700 dark:text-gray-300 hover:bg-gray-100 dark:hover:bg-gray-700'
          }`}
          title="複数選択モード"
        >
          <svg className="w-4 h-4" fill="none" stroke="currentColor" viewBox="0 0 24 24">
            <rect x="3" y="3" width="18" height="18" rx="2" strokeWidth="2" />
            <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M9 12l2 2 4-4" />
          </svg>
          選択
        </button>
        {/* 非表示セッション表示ボタン（hidden タグ付きセッションが存在する場合のみ表示） */}
        {hiddenSessionsCount > 0 && (
          <button
            onClick={() => setShowHiddenSessions((prev) => !prev)}
            className={`inline-flex items-center gap-1.5 px-3 py-1.5 text-sm rounded-md border transition-colors font-medium ${
              showHiddenSessions
                ? 'bg-blue-50 border-blue-400 text-blue-700 dark:bg-blue-900/30 dark:border-blue-500 dark:text-blue-300'
                : 'bg-gray-50 border-gray-200 text-gray-700 dark:bg-gray-800 dark:border-gray-700 dark:text-gray-300 hover:bg-gray-100 dark:hover:bg-gray-700'
            }`}
            title={
              showHiddenSessions
                ? `非表示セッションを隠す (${hiddenSessionsCount}件)`
                : `非表示セッションを表示 (${hiddenSessionsCount}件)`
            }
          >
            {showHiddenSessions ? (
              <svg className="w-4 h-4" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                <path
                  strokeLinecap="round"
                  strokeLinejoin="round"
                  strokeWidth={2}
                  d="M13.875 18.825A10.05 10.05 0 0112 19c-4.478 0-8.268-2.943-9.543-7a9.97 9.97 0 011.563-3.029m5.858.908a3 3 0 114.243 4.243M9.878 9.878l4.242 4.242M9.88 9.88l-3.29-3.29m7.532 7.532l3.29 3.29M3 3l3.59 3.59m0 0A9.953 9.953 0 0112 5c4.478 0 8.268 2.943 9.543 7a10.025 10.025 0 01-4.132 5.411m0 0L21 21"
                />
              </svg>
            ) : (
              <svg className="w-4 h-4" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                <path
                  strokeLinecap="round"
                  strokeLinejoin="round"
                  strokeWidth={2}
                  d="M15 12a3 3 0 11-6 0 3 3 0 016 0z"
                />
                <path
                  strokeLinecap="round"
                  strokeLinejoin="round"
                  strokeWidth={2}
                  d="M2.458 12C3.732 7.943 7.523 5 12 5c4.478 0 8.268 2.943 9.542 7-1.274 4.057-5.064 7-9.542 7-4.477 0-8.268-2.943-9.542-7z"
                />
              </svg>
            )}
            <span className="hidden sm:inline">{showHiddenSessions ? '非表示を隠す' : '非表示を表示'}</span>
          </button>
        )}
        <div className="w-px h-6 bg-gray-300 dark:bg-gray-600"></div>
        {/* ソート機能 */}
        <select
          value={sortBy}
          onChange={(e) => setSortBy(e.target.value as 'started_at' | 'updated_at')}
          className="text-sm border border-gray-200 dark:border-gray-700 rounded-md px-3 py-1.5 bg-gray-50 dark:bg-gray-800 text-gray-700 dark:text-gray-300 hover:bg-gray-100 dark:hover:bg-gray-700 transition-colors focus:outline-none focus:ring-2 focus:ring-blue-500"
        >
          <option value="started_at">開始日時順</option>
          <option value="updated_at">更新日時順</option>
        </select>
        <button
          onClick={() => setSortOrder(sortOrder === 'asc' ? 'desc' : 'asc')}
          className="inline-flex items-center justify-center px-2 py-1.5 text-sm bg-gray-100 dark:bg-gray-700 hover:bg-gray-200 dark:hover:bg-gray-600 rounded-md transition-colors"
          title={sortOrder === 'asc' ? '昇順' : '降順'}
        >
          {sortOrder === 'asc' ? (
            <svg className="w-4 h-4" fill="none" stroke="currentColor" viewBox="0 0 24 24">
              <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M5 15l7-7 7 7" />
            </svg>
          ) : (
            <svg className="w-4 h-4" fill="none" stroke="currentColor" viewBox="0 0 24 24">
              <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M19 9l-7 7-7-7" />
            </svg>
          )}
        </button>
        <div className="w-px h-6 bg-gray-300 dark:bg-gray-600"></div>
        <button
          onClick={() => fetchSessions()}
          disabled={loading}
          className="inline-flex items-center px-3 py-1.5 text-sm text-blue-600 dark:text-blue-400 hover:text-blue-800 dark:hover:text-blue-300 disabled:opacity-50 font-medium"
        >
          <svg className="w-4 h-4 mr-1.5" fill="none" stroke="currentColor" viewBox="0 0 24 24">
            <path
              strokeLinecap="round"
              strokeLinejoin="round"
              strokeWidth={2}
              d="M4 4v5h.582m15.356 2A8.001 8.001 0 004.582 9m0 0H9m11 11v-5h-.581m0 0a8.003 8.003 0 01-15.357-2m15.357 2H15"
            />
          </svg>
          {loading ? '更新中...' : '更新'}
        </button>
      </div>
    </div>
  )
}
