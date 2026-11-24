'use client'
import { SessionListBulkActions } from './SessionListBulkActions'
import { SessionListCreating } from './SessionListCreating'
import { SessionListEmpty } from './SessionListEmpty'
import { SessionListRow } from './SessionListRow'
import { SessionListTemplateDialog } from './SessionListTemplateDialog'
import { SessionListToolbar } from './SessionListToolbar'
import type { SessionListState } from './useSessionListViewState'
export function SessionListContent(context: SessionListState) {
  const {
    loading,
    creatingSessions,
    error,
    success,
    selectedSessions,
    isSelectionMode,
    templateTarget,
    filteredSessions,
    pageState,
    paginatedSessions,
    totalPages,
    handlePageChange,
    toggleSelectAll,
  } = context
  if (loading) {
    return (
      <div className="flex items-center justify-center py-12">
        <svg className="animate-spin h-8 w-8 text-blue-600" fill="none" viewBox="0 0 24 24">
          <circle className="opacity-25" cx="12" cy="12" r="10" stroke="currentColor" strokeWidth="4"></circle>
          <path
            className="opacity-75"
            fill="currentColor"
            d="M4 12a8 8 0 018-8V0C5.373 0 0 5.373 0 12h4zm2 5.291A7.962 7.962 0 014 12H0c0 3.042 1.135 5.824 3 7.938l3-2.647z"
          ></path>
        </svg>
      </div>
    )
  }
  return (
    <div className="space-y-4">
      {/* フローティング一括操作バー */}
      <SessionListBulkActions {...context} />

      {error && (
        <div className="bg-red-50 dark:bg-red-900/20 border border-red-200 dark:border-red-800 rounded-lg p-4">
          <p className="text-red-600 dark:text-red-400 text-sm">{error}</p>
        </div>
      )}

      {success && (
        <div className="bg-green-50 dark:bg-green-900/20 border border-green-200 dark:border-green-800 rounded-lg p-4">
          <p className="text-green-600 dark:text-green-400 text-sm">{success}</p>
        </div>
      )}

      <SessionListToolbar {...context} />

      {/* 作成中セッション */}
      {creatingSessions.length > 0 && <SessionListCreating {...context} />}

      {filteredSessions.length === 0 && creatingSessions.length === 0 ? (
        <SessionListEmpty {...context} />
      ) : (
        <div className="space-y-4">
          {/* 既存セッション */}
          {filteredSessions.length > 0 && (
            <div>
              <div className="bg-white dark:bg-gray-800 border border-gray-200 dark:border-gray-700 rounded-lg divide-y divide-gray-200 dark:divide-gray-700">
                {/* 全選択ヘッダー行（複数選択モード時のみ表示） */}
                {isSelectionMode && (
                  <div
                    className={`px-3 py-2 sm:px-4 flex items-center gap-3 transition-colors duration-150 ${selectedSessions.size > 0 ? 'bg-blue-50 dark:bg-blue-900/20' : 'bg-gray-50 dark:bg-gray-800/50'}`}
                  >
                    <button
                      onClick={toggleSelectAll}
                      className="flex items-center gap-2.5 cursor-pointer select-none group"
                      aria-label="全て選択"
                    >
                      {/* カスタムチェックボックス */}
                      <span
                        className={`flex-shrink-0 flex w-4 h-4 rounded border-2 items-center justify-center transition-colors duration-150 group-hover:border-blue-400 ${
                          selectedSessions.size > 0
                            ? 'bg-blue-600 border-blue-600'
                            : 'bg-white dark:bg-gray-800 border-gray-300 dark:border-gray-600'
                        }`}
                      >
                        {selectedSessions.size > 0 && selectedSessions.size < paginatedSessions.length ? (
                          <span className="block w-2 h-0.5 bg-white rounded-full" />
                        ) : selectedSessions.size > 0 ? (
                          <svg className="w-2.5 h-2.5 text-white" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                            <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={3} d="M5 13l4 4L19 7" />
                          </svg>
                        ) : null}
                      </span>
                      <span
                        className={`text-xs font-medium transition-colors duration-150 ${selectedSessions.size > 0 ? 'text-blue-600 dark:text-blue-400' : 'text-gray-500 dark:text-gray-400 group-hover:text-gray-700 dark:group-hover:text-gray-300'}`}
                      >
                        {selectedSessions.size > 0 ? `${selectedSessions.size}件選択中` : '全て選択'}
                      </span>
                    </button>
                  </div>
                )}
                {paginatedSessions.map((session) => (
                  <SessionListRow key={session.session_id} {...context} session={session} />
                ))}

                {/* Pagination */}
                {totalPages > 1 && (
                  <div className="flex items-center justify-center gap-2 px-4 py-4 border-t border-gray-200 dark:border-gray-700">
                    <button
                      onClick={() => handlePageChange(pageState.page - 1)}
                      disabled={pageState.page === 1}
                      className="px-3 py-2 text-sm border border-gray-300 dark:border-gray-600 rounded-md disabled:opacity-50 disabled:cursor-not-allowed hover:bg-gray-50 dark:hover:bg-gray-700"
                    >
                      Previous
                    </button>

                    <span className="px-4 py-2 text-sm text-gray-700 dark:text-gray-300">
                      Page {pageState.page} of {totalPages}
                    </span>

                    <button
                      onClick={() => handlePageChange(pageState.page + 1)}
                      disabled={pageState.page === totalPages}
                      className="px-3 py-2 text-sm border border-gray-300 dark:border-gray-600 rounded-md disabled:opacity-50 disabled:cursor-not-allowed hover:bg-gray-50 dark:hover:bg-gray-700"
                    >
                      Next
                    </button>
                  </div>
                )}
              </div>
            </div>
          )}
        </div>
      )}

      {templateTarget && <SessionListTemplateDialog {...context} />}
    </div>
  )
}
