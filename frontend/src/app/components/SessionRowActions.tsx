'use client'
import { CircleDot, GitPullRequest, LoaderCircle, MoreHorizontal, Pause } from 'lucide-react'
import type { SessionListRowState } from './sessionListRowState'
export function SessionRowActions(context: SessionListRowState) {
  const {
    acpMode,
    deletingSession,
    suspendingSession,
    openAnnotationMenuId,
    setOpenAnnotationMenuId,
    navigateToChat,
    deleteSession,
    suspendSession,
    restartSessionWithSettings,
    openTemplateDialog,
    session,
    annotations,
    prNumber,
    issueNumber,
  } = context

  return (
    <div
      className="flex flex-row sm:flex-row items-center space-x-2 sm:space-x-2 sm:ml-4"
      onClick={(e) => e.stopPropagation()}
    >
      {session.status !== 'error' && session.status !== 'timeout' && (
        <button
          onClick={() => navigateToChat(session.session_id)}
          className="inline-flex items-center justify-center px-3 py-2 sm:px-3 sm:py-1.5 bg-blue-600 hover:bg-blue-700 text-white text-sm font-medium rounded-md transition-colors min-h-[44px] sm:min-h-0"
        >
          <svg className="w-4 h-4 sm:mr-1.5" fill="none" stroke="currentColor" viewBox="0 0 24 24">
            <path
              strokeLinecap="round"
              strokeLinejoin="round"
              strokeWidth={2}
              d="M8 12h.01M12 12h.01M16 12h.01M21 12c0 4.418-4.03 8-9 8a9.863 9.863 0 01-4.255-.949L3 20l1.395-3.72C3.512 15.042 3 13.574 3 12c0-4.418 4.03-8 9-8s9 3.582 9 8z"
            />
          </svg>
          <span className="hidden sm:inline">チャット</span>
        </button>
      )}

      {session.metadata?.claude_login_url ? (
        <a
          href={String(session.metadata.claude_login_url).replace(/\s+/g, '')}
          target="_blank"
          rel="noopener noreferrer"
          className="inline-flex items-center justify-center px-3 py-2 sm:px-3 sm:py-1.5 border border-blue-300 dark:border-blue-600 hover:bg-blue-50 dark:hover:bg-blue-700 text-blue-700 dark:text-blue-300 text-sm font-medium rounded-md transition-colors min-h-[44px] sm:min-h-0"
        >
          <svg className="w-4 h-4 sm:mr-1.5" fill="none" stroke="currentColor" viewBox="0 0 24 24">
            <path
              strokeLinecap="round"
              strokeLinejoin="round"
              strokeWidth={2}
              d="M8 9l3 3-3 3m5 0h3M5 20h14a2 2 0 002-2V6a2 2 0 00-2-2H5a2 2 0 00-2 2v12a2 2 0 002 2z"
            />
          </svg>
          <span className="hidden sm:inline">ログイン</span>
        </a>
      ) : null}

      {(annotations.prUrl || annotations.issueUrl || !acpMode) && (
        <div className="relative">
          <button
            type="button"
            onClick={() => setOpenAnnotationMenuId((prev) => (prev === session.session_id ? null : session.session_id))}
            className="inline-flex min-h-[44px] items-center justify-center rounded-md border border-gray-300 px-3 py-2 text-gray-600 transition-colors hover:bg-gray-50 hover:text-gray-900 dark:border-gray-600 dark:text-gray-300 dark:hover:bg-gray-700 dark:hover:text-white sm:min-h-0 sm:px-3 sm:py-1.5"
            aria-label="関連リンク"
            title="関連リンク"
          >
            <MoreHorizontal className="h-4 w-4" aria-hidden="true" />
          </button>
          {openAnnotationMenuId === session.session_id && (
            <div className="absolute right-0 top-full z-20 mt-2 min-w-32 overflow-hidden rounded-md border border-gray-200 bg-white py-1 text-xs shadow-lg dark:border-gray-700 dark:bg-gray-900">
              {annotations.prUrl && (
                <a
                  href={annotations.prUrl.replace(/\s+/g, '')}
                  target="_blank"
                  rel="noopener noreferrer"
                  onClick={() => setOpenAnnotationMenuId(null)}
                  className="flex items-center gap-2 px-3 py-2 text-gray-700 hover:bg-gray-50 dark:text-gray-200 dark:hover:bg-gray-800"
                  title={annotations.prUrl}
                >
                  <GitPullRequest className="h-3.5 w-3.5 text-violet-500" aria-hidden="true" />
                  {prNumber ? `PR #${prNumber}` : 'PR'}
                </a>
              )}
              {annotations.issueUrl && (
                <a
                  href={annotations.issueUrl.replace(/\s+/g, '')}
                  target="_blank"
                  rel="noopener noreferrer"
                  onClick={() => setOpenAnnotationMenuId(null)}
                  className="flex items-center gap-2 px-3 py-2 text-gray-700 hover:bg-gray-50 dark:text-gray-200 dark:hover:bg-gray-800"
                  title={annotations.issueUrl}
                >
                  <CircleDot className="h-3.5 w-3.5 text-emerald-500" aria-hidden="true" />
                  {issueNumber ? `Issue #${issueNumber}` : 'Issue'}
                </a>
              )}
              {!acpMode && (
                <>
                  <button
                    type="button"
                    disabled={suspendingSession === session.session_id}
                    onClick={() => {
                      setOpenAnnotationMenuId(null)
                      void restartSessionWithSettings(session.session_id)
                    }}
                    className="block w-full px-3 py-2 text-left hover:bg-gray-100 dark:hover:bg-gray-800 disabled:opacity-50"
                  >
                    設定を再読み込みして再起動
                  </button>
                </>
              )}
              {!acpMode && ['active', 'running'].includes(session.status) && (
                <button
                  type="button"
                  onClick={() => {
                    setOpenAnnotationMenuId(null)
                    void suspendSession(session.session_id)
                  }}
                  disabled={suspendingSession === session.session_id}
                  className="flex w-full items-center gap-2 px-3 py-2 text-left text-violet-700 hover:bg-violet-50 disabled:cursor-not-allowed disabled:opacity-50 dark:text-violet-300 dark:hover:bg-violet-900/30"
                >
                  {suspendingSession === session.session_id ? (
                    <LoaderCircle className="h-3.5 w-3.5 animate-spin" aria-hidden="true" />
                  ) : (
                    <Pause className="h-3.5 w-3.5" aria-hidden="true" />
                  )}
                  {suspendingSession === session.session_id ? 'サスペンド中...' : 'サスペンド'}
                </button>
              )}
              {!acpMode && ['active', 'running', 'suspended'].includes(session.status) && (
                <button
                  type="button"
                  onClick={() => openTemplateDialog(session)}
                  disabled={suspendingSession === session.session_id}
                  className="block w-full px-3 py-2 text-left text-amber-700 hover:bg-amber-50 disabled:cursor-not-allowed disabled:opacity-50 dark:text-amber-300 dark:hover:bg-amber-900/30"
                >
                  ワークスペースとして保存
                </button>
              )}
            </div>
          )}
        </div>
      )}

      <button
        onClick={() => deleteSession(session.session_id)}
        disabled={deletingSession === session.session_id}
        aria-label={`${deletingSession === session.session_id ? '削除中' : '削除'}: ${session.metadata?.description || `セッション ${session.session_id.substring(0, 8)}`}`}
        className="inline-flex items-center justify-center px-3 py-2 sm:px-3 sm:py-1.5 border border-red-300 dark:border-red-600 hover:bg-red-50 dark:hover:bg-red-700 text-red-700 dark:text-red-300 text-sm font-medium rounded-md transition-colors disabled:opacity-50 disabled:cursor-not-allowed min-h-[44px] sm:min-h-0"
      >
        {deletingSession === session.session_id ? (
          <svg
            className="animate-spin w-4 h-4 sm:mr-1.5 text-red-600 dark:text-red-400"
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
        ) : (
          <svg className="w-4 h-4 sm:mr-1.5" fill="none" stroke="currentColor" viewBox="0 0 24 24">
            <path
              strokeLinecap="round"
              strokeLinejoin="round"
              strokeWidth={2}
              d="M19 7l-.867 12.142A2 2 0 0116.138 21H7.862a2 2 0 01-1.995-1.858L5 7m5 4v6m4-6v6m1-10V4a1 1 0 00-1-1h-4a1 1 0 00-1 1v3M4 7h16"
            />
          </svg>
        )}
        <span className="hidden sm:inline">{deletingSession === session.session_id ? '削除中...' : '削除'}</span>
      </button>
    </div>
  )
}
