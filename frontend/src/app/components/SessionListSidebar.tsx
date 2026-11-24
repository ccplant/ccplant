'use client'

import { useRouter } from 'next/navigation'
import { SessionSidebarRow } from './SessionSidebarRow'
import { isRunningStatus } from './sessionSidebarFormatting'
import { useSessionListSidebar } from './useSessionListSidebar'

interface SessionListSidebarProps {
  currentSessionId: string
  isVisible?: boolean
}

export default function SessionListSidebar({ currentSessionId, isVisible = true }: SessionListSidebarProps) {
  const router = useRouter()
  const { deletingIds, handleDelete, loading, sessions } = useSessionListSidebar(currentSessionId, isVisible)

  if (!isVisible) return null

  const runningCount = sessions.filter((s) => isRunningStatus(s.status)).length

  return (
    <div className="flex flex-col w-56 h-full bg-gray-50 dark:bg-gray-950 border-r border-gray-200 dark:border-gray-800 flex-shrink-0 overflow-hidden">
      {/* Session list */}
      <div className="flex-1 overflow-y-auto py-1">
        {loading ? (
          <div className="flex items-center justify-center py-8">
            <div className="animate-spin rounded-full h-4 w-4 border-b-2 border-gray-400" />
          </div>
        ) : sessions.length === 0 ? (
          <p className="text-xs text-gray-400 dark:text-gray-600 text-center py-8 px-3">セッションなし</p>
        ) : (
          <ul>
            {sessions.map((session) => (
              <SessionSidebarRow
                key={session.session_id}
                currentSessionId={currentSessionId}
                isDeleting={deletingIds.has(session.session_id)}
                onDelete={handleDelete}
                session={session}
              />
            ))}
          </ul>
        )}
      </div>

      {/* New session button — just above footer */}
      {!loading && (
        <div className="flex-shrink-0 px-2 py-1.5 border-t border-gray-200 dark:border-gray-800">
          <button
            onClick={() => router.push('/sessions/new')}
            className="w-full flex items-center justify-center gap-1.5 px-2 py-1.5 rounded-md
                       text-xs text-gray-400 dark:text-gray-600
                       border border-dashed border-gray-300 dark:border-gray-700
                       hover:text-blue-600 dark:hover:text-blue-400
                       hover:border-blue-400 dark:hover:border-blue-600
                       hover:bg-blue-50 dark:hover:bg-blue-900/20
                       transition-colors"
          >
            <svg className="w-3.5 h-3.5" fill="none" stroke="currentColor" viewBox="0 0 24 24">
              <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M12 4v16m8-8H4" />
            </svg>
            新規セッション
          </button>
        </div>
      )}

      {/* Footer */}
      {!loading && sessions.length > 0 && (
        <div className="flex-shrink-0 px-3 py-1.5 border-t border-gray-200 dark:border-gray-800">
          <p className="text-[10px] text-gray-400 dark:text-gray-600 text-center">
            {runningCount > 0 ? (
              <>
                <span className="text-green-500">{runningCount}</span> 件稼働中 / {sessions.length} 件
              </>
            ) : (
              `${sessions.length} 件`
            )}
          </p>
        </div>
      )}
    </div>
  )
}
