'use client'
import { CircleAlert } from 'lucide-react'
import { truncateText } from '../../utils/textUtils'
import type { SessionListRowState } from './sessionListRowState'
export function SessionRowTitle(context: SessionListRowState) {
  const { isMobile, expandedErrorSessionId, setExpandedErrorSessionId, session, isNew, agentStatusInfo, description } =
    context

  return (
    <div className="flex items-start gap-3 mb-2">
      {/* 作成中/起動中/準備中セッションの場合はスピナーを表示 */}
      {(session.status === 'creating' ||
        session.status === 'starting' ||
        (isNew && agentStatusInfo.status === 'preparing')) && (
        <div className="flex-shrink-0 mt-0.5">
          <svg
            className={`animate-spin h-4 w-4 ${
              session.status === 'creating' || session.status === 'starting' ? 'text-blue-500' : 'text-blue-500'
            }`}
            fill="none"
            viewBox="0 0 24 24"
          >
            <circle className="opacity-25" cx="12" cy="12" r="10" stroke="currentColor" strokeWidth="4"></circle>
            <path
              className="opacity-75"
              fill="currentColor"
              d="M4 12a8 8 0 018-8V0C5.373 0 0 5.373 0 12h4zm2 5.291A7.962 7.962 0 014 12H0c0 3.042 1.135 5.824 3 7.938l3-2.647z"
            ></path>
          </svg>
        </div>
      )}
      <h3
        className={`text-sm sm:text-base font-medium leading-5 sm:leading-6 ${
          description === '読み込み中...' ? 'text-gray-500 dark:text-gray-400 italic' : 'text-gray-900 dark:text-white'
        }`}
      >
        {truncateText(String(description), isMobile ? 60 : 80)}
      </h3>
      <div
        className={`w-3 h-3 rounded-full flex-shrink-0 ${agentStatusInfo.colorClass}`}
        title={`Agent: ${agentStatusInfo.text}`}
      />
      {/* 新規セッション/作成中/起動中バッジ */}
      {(isNew ||
        session.status === 'creating' ||
        session.status === 'starting' ||
        session.status === 'error' ||
        session.status === 'timeout') && (
        <span
          className={`inline-flex items-center px-2 py-0.5 text-xs font-medium rounded-full ${
            session.status === 'error' || session.status === 'timeout'
              ? 'bg-red-100 text-red-800 dark:bg-red-900 dark:text-red-200'
              : session.status === 'creating'
                ? 'bg-blue-100 text-blue-800 dark:bg-blue-900 dark:text-blue-200'
                : session.status === 'starting'
                  ? 'bg-indigo-100 text-indigo-800 dark:bg-indigo-900 dark:text-indigo-200'
                  : agentStatusInfo.status === 'preparing'
                    ? 'bg-blue-100 text-blue-800 dark:bg-blue-900 dark:text-blue-200'
                    : 'bg-green-100 text-green-800 dark:bg-green-900 dark:text-green-200'
          }`}
        >
          {session.status === 'error' || session.status === 'timeout'
            ? session.status === 'timeout'
              ? '起動タイムアウト'
              : '起動失敗'
            : session.status === 'creating'
              ? '作成中'
              : session.status === 'starting'
                ? '起動中'
                : agentStatusInfo.status === 'preparing'
                  ? '準備中'
                  : '新規'}
        </span>
      )}
      {(session.status === 'error' || session.status === 'timeout') && (
        <button
          type="button"
          onClick={(event) => {
            event.stopPropagation()
            setExpandedErrorSessionId((current) => (current === session.session_id ? null : session.session_id))
          }}
          className="inline-flex h-5 w-5 flex-shrink-0 items-center justify-center rounded-full text-red-600 transition-colors hover:bg-red-100 hover:text-red-800 focus:outline-none focus:ring-2 focus:ring-red-500 focus:ring-offset-1 dark:text-red-400 dark:hover:bg-red-900/50 dark:hover:text-red-200"
          aria-label="エラー詳細を表示"
          title="エラー詳細"
          aria-expanded={expandedErrorSessionId === session.session_id}
          aria-controls={`session-error-${session.session_id}`}
        >
          <CircleAlert className="h-4 w-4" aria-hidden="true" />
        </button>
      )}
    </div>
  )
}
