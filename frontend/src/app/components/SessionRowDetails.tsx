'use client'
import { formatDate, formatRelativeTime } from '../../utils/timeUtils'
import type { SessionListRowState } from './sessionListRowState'
export function SessionRowDetails(context: SessionListRowState) {
  const { sessionAgentStatus, session } = context

  return (
    <div className="flex flex-col sm:flex-row sm:items-center text-xs text-gray-500 dark:text-gray-400 space-y-1 sm:space-y-0 sm:space-x-4 mb-3">
      <span>#{session.session_id.substring(0, 8)}</span>
      <div className="flex items-center space-x-2 sm:space-x-4">
        <span>
          開始: {formatDate(session.started_at)} ({formatRelativeTime(session.started_at)})
        </span>
        {session.updated_at && (
          <span className="hidden sm:inline">
            更新: {formatDate(session.updated_at)} ({formatRelativeTime(session.updated_at)})
          </span>
        )}
        {sessionAgentStatus[session.session_id]?.last_activity && (
          <span className="hidden sm:inline">
            Agent活動: {formatDate(sessionAgentStatus[session.session_id].last_activity!)} (
            {formatRelativeTime(sessionAgentStatus[session.session_id].last_activity!)})
          </span>
        )}
        {sessionAgentStatus[session.session_id]?.current_task && (
          <span className="hidden sm:inline text-blue-600 dark:text-blue-400">
            タスク: {sessionAgentStatus[session.session_id].current_task!.substring(0, 30)}...
          </span>
        )}
      </div>
      <span className="hidden sm:inline">by {session.user_id}</span>
      {session.environment?.WORKSPACE_NAME && (
        <span className="inline-flex items-center px-2 py-0.5 rounded-full text-xs bg-gray-100 text-gray-800 dark:bg-gray-800 dark:text-gray-300 self-start">
          {session.environment.WORKSPACE_NAME}
        </span>
      )}
    </div>
  )
}
