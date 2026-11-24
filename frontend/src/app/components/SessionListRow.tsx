'use client'
import { LoaderCircle } from 'lucide-react'
import { Session } from '../../types/agentapi'
import { truncateText } from '../../utils/textUtils'
import { buildSessionListRowState } from './sessionListRowState'
import { SessionRowActions } from './SessionRowActions'
import { SessionRowDetails } from './SessionRowDetails'
import { SessionRowTags } from './SessionRowTags'
import { SessionRowTitle } from './SessionRowTitle'
import type { SessionListState } from './useSessionListViewState'
export function SessionListRow(context: SessionListState & { session: Session }) {
  const rowContext = buildSessionListRowState(context)
  const {
    isMobile,
    isSelectionMode,
    expandedErrorSessionId,
    toggleSessionSelection,
    session,
    isNew,
    isOld,
    annotations,
    isSelected,
  } = rowContext
  return (
    <div
      key={session.session_id}
      onClick={isSelectionMode ? () => toggleSessionSelection(session.session_id) : undefined}
      className={`group px-3 py-4 sm:px-4 transition-colors duration-150 ${isSelectionMode ? 'cursor-pointer' : ''} ${
        isSelected
          ? 'bg-blue-50 dark:bg-blue-900/20 border-l-2 border-blue-500'
          : session.status === 'error' || session.status === 'timeout'
            ? 'bg-red-50 dark:bg-red-900/20 hover:bg-red-100 dark:hover:bg-red-900/30 border-l-4 border-red-500'
            : session.status === 'creating' || session.status === 'starting'
              ? 'bg-gradient-to-r from-blue-50 to-indigo-50 dark:from-blue-900/20 dark:to-indigo-900/20 hover:from-blue-100 hover:to-indigo-100 dark:hover:from-blue-900/30 dark:hover:to-indigo-900/30 border-l-4 border-blue-500'
              : isNew
                ? 'bg-gradient-to-r from-blue-50 to-indigo-50 dark:from-blue-900/10 dark:to-indigo-900/10 hover:from-blue-100 hover:to-indigo-100 dark:hover:from-blue-900/20 dark:hover:to-indigo-900/20'
                : 'hover:bg-gray-50 dark:hover:bg-gray-800/50'
      }`}
      style={isOld ? { filter: 'grayscale(50%)', opacity: 0.7 } : {}}
    >
      <div className="flex gap-3">
        {/* カスタムチェックボックス（選択モード時は常時表示、非選択モードではホバー時のみ表示） */}
        <div className="flex-shrink-0 pt-1">
          <button
            onClick={(e) => {
              e.stopPropagation()
              toggleSessionSelection(session.session_id)
            }}
            aria-label={`セッション ${session.session_id.substring(0, 8)} を選択`}
            className={`flex w-4 h-4 rounded border-2 items-center justify-center transition-all duration-150 focus:outline-none focus:ring-2 focus:ring-blue-500 focus:ring-offset-1 ${
              isSelected
                ? 'bg-blue-600 border-blue-600 opacity-100'
                : isSelectionMode
                  ? 'bg-white dark:bg-gray-800 border-gray-300 dark:border-gray-600 hover:border-blue-400 opacity-100'
                  : 'bg-white dark:bg-gray-800 border-gray-300 dark:border-gray-600 hover:border-blue-400 opacity-0 group-hover:opacity-100'
            }`}
          >
            {isSelected && (
              <svg className="w-2.5 h-2.5 text-white" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={3} d="M5 13l4 4L19 7" />
              </svg>
            )}
          </button>
        </div>
        <div className="flex flex-col sm:flex-row sm:items-start sm:justify-between space-y-3 sm:space-y-0 flex-1 min-w-0">
          <div className="flex-1 min-w-0">
            {/* タイトルとステータス */}
            <SessionRowTitle {...rowContext} />

            {annotations.runningTask && (
              <div className="flex flex-wrap items-center gap-2 mb-2">
                <span
                  className="inline-flex max-w-full items-center gap-1.5 text-xs text-gray-500 dark:text-gray-400"
                  title={annotations.runningTask}
                >
                  <LoaderCircle className="h-3 w-3 flex-shrink-0" aria-hidden="true" />
                  <span>作業中:</span>
                  <span className="truncate">{truncateText(annotations.runningTask, isMobile ? 32 : 64)}</span>
                </span>
              </div>
            )}

            {(session.status === 'error' || session.status === 'timeout') &&
              expandedErrorSessionId === session.session_id && (
                <div
                  id={`session-error-${session.session_id}`}
                  className="mb-3 rounded-md border border-red-200 bg-red-50 px-3 py-2 text-sm text-red-700 dark:border-red-800 dark:bg-red-950/40 dark:text-red-300"
                  role="alert"
                >
                  {session.error_code === 'codex_auth_required' ? (
                    <>
                      <span>Codex の認証期限が切れています。再認証してください。</span>{' '}
                      <a href="/settings#codex-device-auth" className="font-medium underline">
                        Codex を再認証
                      </a>
                    </>
                  ) : (
                    session.error_message ||
                    'セッションの起動に失敗しました。もう一度実行してください。繰り返し失敗する場合は、セッション ID を添えて管理者にお問い合わせください。'
                  )}
                </div>
              )}

            {/* セッション情報 */}
            <SessionRowDetails {...rowContext} />

            {/* タグとメタデータの表示 - モバイルでは最大2個まで表示 */}
            <SessionRowTags {...rowContext} />
          </div>

          {/* アクションボタン - モバイルでは縦並び */}
          <SessionRowActions {...rowContext} />
        </div>
      </div>
    </div>
  )
}
