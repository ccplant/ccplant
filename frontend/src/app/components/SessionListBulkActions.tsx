'use client'
import type { SessionListState } from './useSessionListViewState'
export function SessionListBulkActions(context: SessionListState) {
  const { selectedSessions, setSelectedSessions, isBulkDeleting, deleteSelectedSessions } = context

  return (
    <div
      className={`fixed bottom-8 left-1/2 -translate-x-1/2 z-50 flex items-center gap-3 px-5 py-3 bg-gray-900 dark:bg-gray-100 text-white dark:text-gray-900 rounded-full shadow-2xl transition-all duration-300 ${selectedSessions.size > 0 ? 'translate-y-0 opacity-100' : 'translate-y-24 opacity-0 pointer-events-none'}`}
    >
      <span className="text-sm font-semibold tabular-nums whitespace-nowrap">{selectedSessions.size}件選択中</span>
      <div className="w-px h-4 bg-gray-600 dark:bg-gray-400" />
      <button
        onClick={() => setSelectedSessions(new Set())}
        className="text-sm font-medium text-gray-300 dark:text-gray-600 hover:text-white dark:hover:text-gray-900 transition-colors whitespace-nowrap"
      >
        選択解除
      </button>
      <button
        onClick={deleteSelectedSessions}
        disabled={isBulkDeleting}
        className="flex items-center gap-1.5 px-3 py-1.5 bg-red-500 hover:bg-red-600 disabled:opacity-50 disabled:cursor-not-allowed text-white text-sm font-medium rounded-full transition-colors whitespace-nowrap"
      >
        {isBulkDeleting ? (
          <svg className="animate-spin w-3.5 h-3.5" fill="none" viewBox="0 0 24 24">
            <circle className="opacity-25" cx="12" cy="12" r="10" stroke="currentColor" strokeWidth="4"></circle>
            <path
              className="opacity-75"
              fill="currentColor"
              d="M4 12a8 8 0 018-8V0C5.373 0 0 5.373 0 12h4zm2 5.291A7.962 7.962 0 014 12H0c0 3.042 1.135 5.824 3 7.938l3-2.647z"
            ></path>
          </svg>
        ) : (
          <svg className="w-3.5 h-3.5" fill="none" stroke="currentColor" viewBox="0 0 24 24">
            <path
              strokeLinecap="round"
              strokeLinejoin="round"
              strokeWidth={2}
              d="M19 7l-.867 12.142A2 2 0 0116.138 21H7.862a2 2 0 01-1.995-1.858L5 7m5 4v6m4-6v6m1-10V4a1 1 0 00-1-1h-4a1 1 0 00-1 1v3M4 7h16"
            />
          </svg>
        )}
        {isBulkDeleting ? '削除中...' : '削除'}
      </button>
    </div>
  )
}
