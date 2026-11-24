'use client'

import { useRouter } from 'next/navigation'
import { Session } from '../../types/agentapi'
import { formatRelativeTime, getSessionAnnotations, getSessionTitle, getStatusDotClass, isRunningStatus } from './sessionSidebarFormatting'

interface SessionSidebarRowProps {
  currentSessionId: string
  isDeleting: boolean
  onDelete: (event: React.MouseEvent, sessionId: string) => void
  session: Session
}

export function SessionSidebarRow({ currentSessionId, isDeleting, onDelete, session }: SessionSidebarRowProps) {
  const router = useRouter()
const isActive = session.session_id === currentSessionId
const running = isRunningStatus(session.status)
const title = getSessionTitle(session)
const annotations = getSessionAnnotations(session)
const timeStr = formatRelativeTime(session.updated_at || session.started_at)

return (
  <li key={session.session_id} className="group/item relative">
    {/* Navigate button (whole row) */}
    <button
      onClick={() =>
        session.status !== 'error' &&
        session.status !== 'timeout' &&
        router.push(`/sessions/${session.session_id}`)
      }
      disabled={isDeleting || session.status === 'error' || session.status === 'timeout'}
      className={`w-full text-left px-3 py-2 pr-9 flex items-start gap-2 transition-colors ${
        isActive
          ? 'bg-white dark:bg-gray-900 border-r-2 border-blue-500'
          : 'hover:bg-white/60 dark:hover:bg-gray-900/60'
      } ${isDeleting ? 'opacity-40 pointer-events-none' : ''} ${session.status === 'error' || session.status === 'timeout' ? 'cursor-default bg-red-50/60 dark:bg-red-950/20' : ''}`}
    >
      {/* Status dot */}
      <div className="mt-[5px] flex-shrink-0 relative">
        <div className={`w-1.5 h-1.5 rounded-full ${getStatusDotClass(session.status)}`} />
        {session.status === 'active' && (
          <div className="absolute inset-0 rounded-full bg-green-400 opacity-50 animate-ping" />
        )}
      </div>

      {/* Session info */}
      <div className="flex-1 min-w-0">
        <p
          className={`text-xs truncate leading-snug ${
            isActive ? 'text-gray-900 dark:text-gray-100 font-medium' : 'text-gray-700 dark:text-gray-300'
          }`}
          title={title}
        >
          {title}
        </p>
        {annotations.runningTask && (
          <p
            className="text-[10px] text-amber-600 dark:text-amber-400 truncate mt-0.5 leading-none"
            title={annotations.runningTask}
          >
            {annotations.runningTask}
          </p>
        )}
        <p className="text-[10px] text-gray-400 dark:text-gray-600 mt-0.5 leading-none">
          {session.status === 'error' || session.status === 'timeout' ? (
            <span className="text-red-600 dark:text-red-400" title={session.error_message}>
              {session.status === 'timeout' ? '起動タイムアウト' : '起動失敗'}
            </span>
          ) : session.status === 'suspended' ? (
            <span className="text-violet-600 dark:text-violet-400">サスペンド中</span>
          ) : session.status === 'running' ? (
            <span className="text-yellow-500 dark:text-yellow-400">実行中</span>
          ) : running && session.status !== 'active' ? (
            <span className="text-yellow-500 dark:text-yellow-400">
              {session.status === 'starting' ? '起動中' : '作成中'}
            </span>
          ) : (
            timeStr
          )}
        </p>
      </div>
    </button>

    {/* Delete button — visible on row hover */}
    <button
      onClick={(e) => onDelete(e, session.session_id)}
      disabled={isDeleting}
      className="absolute right-1 top-1/2 -translate-y-1/2 p-1.5 rounded-md
                 text-gray-400 dark:text-gray-600
                 opacity-0 group-hover/item:opacity-100
                 hover:text-red-500 dark:hover:text-red-400
                 hover:bg-red-50 dark:hover:bg-red-900/20
                 transition-all duration-100
                 disabled:pointer-events-none"
      title="セッションを削除"
    >
      {isDeleting ? (
        <svg className="w-4 h-4 animate-spin" fill="none" viewBox="0 0 24 24">
          <circle className="opacity-25" cx="12" cy="12" r="10" stroke="currentColor" strokeWidth="4" />
          <path
            className="opacity-75"
            fill="currentColor"
            d="M4 12a8 8 0 018-8V0C5.373 0 0 5.373 0 12h4z"
          />
        </svg>
      ) : (
        <svg className="w-4 h-4" fill="none" stroke="currentColor" viewBox="0 0 24 24">
          <path
            strokeLinecap="round"
            strokeLinejoin="round"
            strokeWidth={2}
            d="M19 7l-.867 12.142A2 2 0 0116.138 21H7.862a2 2 0 01-1.995-1.858L5 7m5 4v6m4-6v6m1-10V4a1 1 0 00-1-1h-4a1 1 0 00-1 1v3M4 7h16"
          />
        </svg>
      )}
    </button>
  </li>
)
}
