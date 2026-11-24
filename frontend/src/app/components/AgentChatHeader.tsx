'use client';

import Link from 'next/link';
import ShareSessionButton from './ShareSessionButton';

import type { useAgentChat } from './useAgentChat';

export function AgentChatHeader(state: ReturnType<typeof useAgentChat>) {
  const {
    router,
    sessionId,
    agentAPI,
    isLoading,
    agentStatus,
    error,
    isConnected,
    isDeleting,
    sidebarVisible,
    toggleSidebar,
    showPRLinks,
    setShowPRLinks,
    connectionStatusLabel,
    connectionStatusTextClass,
    connectionStatusDotClass,
    relatedLinks,
    sendStopSignal,
    deleteSession,
  } = state;
  return (
    <div className="bg-white dark:bg-gray-900 border-b border-gray-200 dark:border-gray-700 px-3 sm:px-4 py-1.5 sm:py-2 flex-shrink-0">
      <div className="flex items-center justify-between">
        <div className="flex items-center space-x-1 sm:space-x-2">
          {/* Sidebar toggle */}
          <button
            onClick={toggleSidebar}
            className="hidden md:flex items-center justify-center p-1.5 text-gray-400 hover:text-gray-600 dark:text-gray-500 dark:hover:text-gray-300 hover:bg-gray-100 dark:hover:bg-gray-800 rounded-md transition-colors"
            title={sidebarVisible ? 'セッション一覧を隠す' : 'セッション一覧を表示'}
          >
            <svg className="w-4 h-4" fill="none" stroke="currentColor" viewBox="0 0 24 24">
              <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M4 6h16M4 12h16M4 18h16" />
            </svg>
          </button>
          <Link
            href="/chats"
            className="flex items-center space-x-1 sm:space-x-2 px-2 py-1 text-xs sm:text-sm text-gray-600 dark:text-gray-400 hover:text-gray-900 dark:hover:text-white hover:bg-gray-100 dark:hover:bg-gray-800 rounded-md transition-colors"
            title="Go to Conversations"
          >
            <svg className="w-3 h-3 sm:w-4 sm:h-4" fill="none" stroke="currentColor" viewBox="0 0 24 24">
              <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M15 19l-7-7 7-7" />
            </svg>
            <span className="hidden sm:inline">Conversations</span>
          </Link>
          <div>
            <h2 className="text-sm sm:text-base font-semibold text-gray-900 dark:text-white">
              <span className="inline">Chat</span>
              {sessionId && (
                <span className="ml-2 text-xs font-normal text-gray-500 dark:text-gray-400 inline">
                  #{sessionId.substring(0, 6)}
                </span>
              )}
            </h2>
          </div>
        </div>
        <div className="flex items-center space-x-2 sm:space-x-4">
          {/* Agent Status */}
          {agentStatus && (
            <div
              className="flex items-center justify-center p-1"
              role="status"
              aria-label={agentStatus.status === 'stable' ? 'エージェント利用可能' : agentStatus.status === 'running' ? 'エージェント進行中' : `エージェント: ${agentStatus.status}`}
              title={agentStatus.status === 'stable' ? 'エージェント利用可能' : agentStatus.status === 'running' ? 'エージェント進行中' : `エージェント: ${agentStatus.status}`}
            >
              <div
                className={`h-2.5 w-2.5 rounded-full ${agentStatus.status === 'stable' ? 'bg-green-500' : 'bg-yellow-500'} ${agentStatus.status === 'running' ? 'animate-pulse' : ''}`}
              ></div>
            </div>
          )}

          {/* Stop Button */}
          {agentStatus?.status === 'running' && (
            <button
              onClick={sendStopSignal}
              disabled={!isConnected || isLoading}
              className="px-2 sm:px-3 py-1 bg-red-600 hover:bg-red-700 disabled:bg-gray-300 dark:disabled:bg-gray-600 text-white text-xs sm:text-sm rounded-md transition-colors disabled:cursor-not-allowed flex items-center space-x-1"
              title="Force Stop (ESC)"
            >
              <svg className="w-3 h-3 sm:w-4 sm:h-4" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                <path
                  strokeLinecap="round"
                  strokeLinejoin="round"
                  strokeWidth={2}
                  d="M21 12a9 9 0 11-18 0 9 9 0 0118 0z"
                />
                <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M9 10h6v4H9z" />
              </svg>
              <span className="hidden sm:inline">Stop</span>
            </button>
          )}

          {/* Connection Status */}
          <div className="flex items-center space-x-1 sm:space-x-2">
            <div className={`w-2 h-2 rounded-full ${connectionStatusDotClass}`}></div>
            <span className={`text-xs sm:text-sm ${connectionStatusTextClass} hidden sm:inline`}>
              {connectionStatusLabel}
            </span>
          </div>

          {/* Delete Session Button */}
          {sessionId && (
            <button
              onClick={deleteSession}
              disabled={isDeleting}
              className="p-2 text-red-500 hover:text-red-700 dark:text-red-400 dark:hover:text-red-200 hover:bg-red-50 dark:hover:bg-red-900/20 rounded-md transition-colors disabled:opacity-50 disabled:cursor-not-allowed"
              title={isDeleting ? 'セッションを削除中...' : 'セッションを削除'}
            >
              {isDeleting ? (
                <svg className="animate-spin w-5 h-5" fill="none" viewBox="0 0 24 24">
                  <circle className="opacity-25" cx="12" cy="12" r="10" stroke="currentColor" strokeWidth="4"></circle>
                  <path
                    className="opacity-75"
                    fill="currentColor"
                    d="M4 12a8 8 0 018-8V0C5.373 0 0 5.373 0 12h4zm2 5.291A7.962 7.962 0 014 12H0c0 3.042 1.135 5.824 3 7.938l3-2.647z"
                  ></path>
                </svg>
              ) : (
                <svg className="w-5 h-5" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                  <path
                    strokeLinecap="round"
                    strokeLinejoin="round"
                    strokeWidth={2}
                    d="M19 7l-.867 12.142A2 2 0 0116.138 21H7.862a2 2 0 01-1.995-1.858L5 7m5 4v6m4-6v6m1-10V4a1 1 0 00-1-1h-4a1 1 0 00-1 1v3M4 7h16"
                  />
                </svg>
              )}
            </button>
          )}

          {/* Related Links Button - 必要なときだけ表示 */}
          {relatedLinks.length > 0 && (
            <button
              onClick={() => setShowPRLinks(!showPRLinks)}
              className="p-2 text-gray-500 hover:text-gray-700 dark:text-gray-400 dark:hover:text-gray-200 hover:bg-gray-100 dark:hover:bg-gray-800 rounded-md transition-colors relative"
              title={`関連リンク (${relatedLinks.length}件)`}
            >
              <svg className="w-5 h-5" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                <path
                  strokeLinecap="round"
                  strokeLinejoin="round"
                  strokeWidth={2}
                  d="M13.5 6H15a3 3 0 010 6h-1.5m-3 0H9a3 3 0 010-6h1.5m-1 6h5"
                />
              </svg>
              <span className="absolute -top-1 -right-1 bg-gray-500 text-white text-xs rounded-full h-5 w-5 flex items-center justify-center">
                {relatedLinks.length}
              </span>
            </button>
          )}

          {/* Share Session Button */}
          {sessionId && agentAPI && <ShareSessionButton sessionId={sessionId} agentAPI={agentAPI} />}

          {/* Settings Navigation Button */}
          <button
            onClick={() => router.push('/settings')}
            className="p-2 text-gray-500 hover:text-gray-700 dark:text-gray-400 dark:hover:text-gray-200 hover:bg-gray-100 dark:hover:bg-gray-800 rounded-md transition-colors"
            title="Settings"
          >
            <svg className="w-5 h-5" fill="none" stroke="currentColor" viewBox="0 0 24 24">
              <path
                strokeLinecap="round"
                strokeLinejoin="round"
                strokeWidth={2}
                d="M10.325 4.317c.426-1.756 2.924-1.756 3.35 0a1.724 1.724 0 002.573 1.066c1.543-.94 3.31.826 2.37 2.37a1.724 1.724 0 001.065 2.572c1.756.426 1.756 2.924 0 3.35a1.724 1.724 0 00-1.066 2.573c.94 1.543-.826 3.31-2.37 2.37a1.724 1.724 0 00-2.572 1.065c-.426 1.756-2.924 1.756-3.35 0a1.724 1.724 0 00-2.573-1.066c-1.543.94-3.31-.826-2.37-2.37a1.724 1.724 0 00-1.065-2.572c-1.756-.426-1.756-2.924 0-3.35a1.724 1.724 0 001.066-2.573c-.94-1.543.826-3.31 2.37-2.37.996.608 2.296.07 2.572-1.065z"
              />
              <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M15 12a3 3 0 11-6 0 3 3 0 016 0z" />
            </svg>
          </button>
        </div>
      </div>
      {error && (
        <div className="mt-4 p-3 bg-red-50 dark:bg-red-900/20 border border-red-200 dark:border-red-800 rounded-md text-red-700 dark:text-red-400 text-sm">
          {error}
          {error === 'Codex の認証期限が切れています。再認証してください。' && (
            <>
              {' '}
              <a href="/settings#codex-device-auth" className="font-medium underline">
                Codex を再認証
              </a>
            </>
          )}
        </div>
      )}
    </div>
  );
}
