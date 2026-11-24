'use client'
import { truncateText } from '../../utils/textUtils'
import { formatDate, formatRelativeTime } from '../../utils/timeUtils'
import { getStatusColor, getStatusText } from './sessionListPresentation'
import type { SessionListState } from './useSessionListViewState'
export function SessionListCreating(context: SessionListState) {
  const { creatingSessions } = context

  return (
    <div className="space-y-4">
      <div className="flex items-center gap-2">
        <h4 className="text-md font-medium text-gray-700 dark:text-gray-300">
          作成中のセッション ({creatingSessions.length})
        </h4>
        <div className="flex items-center gap-1">
          <span className="w-2 h-2 bg-blue-500 rounded-full animate-pulse"></span>
          <span className="w-2 h-2 bg-blue-500 rounded-full animate-pulse" style={{ animationDelay: '0.2s' }}></span>
          <span className="w-2 h-2 bg-blue-500 rounded-full animate-pulse" style={{ animationDelay: '0.4s' }}></span>
        </div>
      </div>
      {creatingSessions.map((creatingSession) => (
        <div
          key={creatingSession.id}
          className={`relative overflow-hidden rounded-lg p-6 ${
            creatingSession.status === 'failed'
              ? 'bg-red-50 dark:bg-red-900/20 border-2 border-red-300 dark:border-red-700'
              : creatingSession.status === 'completed'
                ? 'bg-green-50 dark:bg-green-900/20 border-2 border-green-300 dark:border-green-700'
                : 'bg-gradient-to-r from-blue-50 to-indigo-50 dark:from-blue-900/20 dark:to-indigo-900/20 border-2 border-blue-300 dark:border-blue-600'
          }`}
        >
          {/* 作成中のアニメーションボーダー */}
          {creatingSession.status !== 'completed' && creatingSession.status !== 'failed' && (
            <div className="absolute inset-0 rounded-lg overflow-hidden pointer-events-none">
              <div className="absolute inset-0 animate-pulse bg-gradient-to-r from-transparent via-blue-200/30 dark:via-blue-500/20 to-transparent"></div>
            </div>
          )}

          <div className="relative flex items-start gap-4">
            {/* 左側のスピナー/アイコン */}
            <div className="flex-shrink-0">
              {creatingSession.status === 'failed' ? (
                <div className="w-12 h-12 rounded-full bg-red-100 dark:bg-red-900/50 flex items-center justify-center">
                  <svg
                    className="h-6 w-6 text-red-600 dark:text-red-400"
                    fill="none"
                    stroke="currentColor"
                    viewBox="0 0 24 24"
                  >
                    <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M6 18L18 6M6 6l12 12" />
                  </svg>
                </div>
              ) : creatingSession.status === 'completed' ? (
                <div className="w-12 h-12 rounded-full bg-green-100 dark:bg-green-900/50 flex items-center justify-center">
                  <svg
                    className="h-6 w-6 text-green-600 dark:text-green-400"
                    fill="none"
                    stroke="currentColor"
                    viewBox="0 0 24 24"
                  >
                    <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M5 13l4 4L19 7" />
                  </svg>
                </div>
              ) : (
                <div className="w-12 h-12 rounded-full bg-blue-100 dark:bg-blue-900/50 flex items-center justify-center">
                  <svg
                    className="animate-spin h-6 w-6 text-blue-600 dark:text-blue-400"
                    fill="none"
                    viewBox="0 0 24 24"
                  >
                    <circle
                      className="opacity-25"
                      cx="12"
                      cy="12"
                      r="10"
                      stroke="currentColor"
                      strokeWidth="4"
                    ></circle>
                    <path
                      className="opacity-75"
                      fill="currentColor"
                      d="M4 12a8 8 0 018-8V0C5.373 0 0 5.373 0 12h4zm2 5.291A7.962 7.962 0 014 12H0c0 3.042 1.135 5.824 3 7.938l3-2.647z"
                    ></path>
                  </svg>
                </div>
              )}
            </div>

            {/* コンテンツ */}
            <div className="flex-1 min-w-0">
              <div className="flex items-center gap-3 mb-2">
                <span
                  className={`inline-flex items-center px-3 py-1.5 text-sm font-semibold rounded-full ${getStatusColor(creatingSession.status)}`}
                >
                  {creatingSession.status !== 'completed' && creatingSession.status !== 'failed' && (
                    <svg className="animate-spin -ml-0.5 mr-2 h-3 w-3" fill="none" viewBox="0 0 24 24">
                      <circle
                        className="opacity-25"
                        cx="12"
                        cy="12"
                        r="10"
                        stroke="currentColor"
                        strokeWidth="4"
                      ></circle>
                      <path
                        className="opacity-75"
                        fill="currentColor"
                        d="M4 12a8 8 0 018-8V0C5.373 0 0 5.373 0 12h4zm2 5.291A7.962 7.962 0 014 12H0c0 3.042 1.135 5.824 3 7.938l3-2.647z"
                      ></path>
                    </svg>
                  )}
                  {getStatusText(creatingSession.status)}
                </span>
                <span className="text-sm text-gray-500 dark:text-gray-400">
                  {formatDate(creatingSession.startTime.toISOString())} (
                  {formatRelativeTime(creatingSession.startTime.toISOString())})
                </span>
              </div>

              {/* プログレスステップ表示 */}
              {creatingSession.status !== 'completed' && creatingSession.status !== 'failed' && (
                <div className="flex items-center gap-2 mb-3">
                  <div
                    className={`h-1.5 flex-1 rounded-full ${
                      creatingSession.status === 'creating' ||
                      creatingSession.status === 'waiting-agent' ||
                      creatingSession.status === 'sending-message'
                        ? 'bg-blue-500'
                        : 'bg-gray-200 dark:bg-gray-700'
                    }`}
                  ></div>
                  <div
                    className={`h-1.5 flex-1 rounded-full ${
                      creatingSession.status === 'waiting-agent' || creatingSession.status === 'sending-message'
                        ? 'bg-blue-500'
                        : 'bg-gray-200 dark:bg-gray-700'
                    }`}
                  ></div>
                  <div
                    className={`h-1.5 flex-1 rounded-full ${
                      creatingSession.status === 'sending-message' ? 'bg-blue-500' : 'bg-gray-200 dark:bg-gray-700'
                    }`}
                  ></div>
                </div>
              )}

              <div className="mb-2">
                <p className="text-gray-900 dark:text-white font-medium">{truncateText(creatingSession.message)}</p>
              </div>

              {/* リポジトリタグ */}
              {creatingSession.repository && (
                <div className="flex flex-wrap gap-2">
                  <span className="inline-flex items-center px-2 py-1 text-xs bg-gray-100 dark:bg-gray-700 text-gray-800 dark:text-gray-200 rounded-full">
                    <svg className="w-3 h-3 mr-1" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                      <path
                        strokeLinecap="round"
                        strokeLinejoin="round"
                        strokeWidth={2}
                        d="M3 7v10a2 2 0 002 2h14a2 2 0 002-2V9a2 2 0 00-2-2h-6l-2-2H5a2 2 0 00-2 2z"
                      />
                    </svg>
                    {creatingSession.repository}
                  </span>
                </div>
              )}

              {/* 失敗時のエラーメッセージ */}
              {creatingSession.status === 'failed' && (
                <div className="mt-3 p-3 bg-red-100 dark:bg-red-900/30 rounded-lg">
                  <p className="text-sm text-red-700 dark:text-red-300 font-medium">
                    セッションの作成に失敗しました。再度お試しください。
                  </p>
                </div>
              )}
            </div>
          </div>
        </div>
      ))}
    </div>
  )
}
