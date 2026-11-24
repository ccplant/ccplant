'use client'

import { Schedule } from '../../types/schedule'
import CopyableResourceId from './CopyableResourceId'
import ScheduleStatusBadge from './ScheduleStatusBadge'
import { formatScheduleDateTime } from './scheduleCardFormatting'

interface ScheduleCardDetailsProps {
  schedule: Schedule
  showExecutionHistory: boolean
  onToggleExecutionHistory: () => void
}

export function ScheduleCardDetails({ schedule, showExecutionHistory, onToggleExecutionHistory }: ScheduleCardDetailsProps) {
  return (
    <>
      {/* Header */}
      <div className="flex items-start justify-between mb-3">
        <div className="flex-1 min-w-0">
          <h3 className="text-lg font-medium text-gray-900 dark:text-white truncate">
            {schedule.name}
          </h3>
          <CopyableResourceId id={schedule.id} className="mt-1" />
          <div className="flex items-center gap-2 mt-1">
            <ScheduleStatusBadge status={schedule.status} />
            {schedule.timezone && (
              <span className="text-xs text-gray-500 dark:text-gray-400">
                {schedule.timezone}
              </span>
            )}
          </div>
        </div>
      </div>
      
      {/* Schedule Info */}
      <div className="space-y-2 text-sm">
        {/* Cron or Scheduled At */}
        <div className="flex items-center gap-2 text-gray-600 dark:text-gray-400">
          <svg className="w-4 h-4 flex-shrink-0" fill="none" stroke="currentColor" viewBox="0 0 24 24">
            <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M12 8v4l3 3m6-3a9 9 0 11-18 0 9 9 0 0118 0z" />
          </svg>
          {schedule.cron_expr ? (
            <span className="font-mono">{schedule.cron_expr || '-'}</span>
          ) : (
            <span>{formatScheduleDateTime(schedule.scheduled_at)}</span>
          )}
        </div>
      
        {/* Next Execution */}
        {schedule.next_execution_at && (
          <div className="flex items-center gap-2 text-gray-600 dark:text-gray-400">
            <svg className="w-4 h-4 flex-shrink-0" fill="none" stroke="currentColor" viewBox="0 0 24 24">
              <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M13 7l5 5m0 0l-5 5m5-5H6" />
            </svg>
            <span>次回: {formatScheduleDateTime(schedule.next_execution_at)}</span>
          </div>
        )}
      
        {/* Last Execution */}
        {schedule.last_execution && (
          <div className="flex items-center gap-2 text-gray-600 dark:text-gray-400">
            <svg className="w-4 h-4 flex-shrink-0" fill="none" stroke="currentColor" viewBox="0 0 24 24">
              <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M12 8v4l3 3m6-3a9 9 0 11-18 0 9 9 0 0118 0z" />
            </svg>
            <span>
              前回: {formatScheduleDateTime(schedule.last_execution.executed_at)}
              {' · '}
              {schedule.last_execution.status === 'success' ? '成功' : schedule.last_execution.status === 'failed' ? '失敗' : 'スキップ'}
              {schedule.last_execution.session_reused ? '（再利用）' : ''}
            </span>
          </div>
        )}
      
        {schedule.last_execution?.error && (
          <div className="rounded border border-red-200 bg-red-50 p-2 text-xs text-red-700 dark:border-red-900/60 dark:bg-red-950/30 dark:text-red-300">
            {schedule.last_execution.error}
          </div>
        )}
      
        {schedule.execution_history && schedule.execution_history.length > 0 && (
          <div>
            <button
              type="button"
              onClick={onToggleExecutionHistory}
              className="text-xs text-blue-600 hover:underline dark:text-blue-400"
            >
              {showExecutionHistory ? '実行履歴を閉じる' : `実行履歴を見る（${schedule.execution_history.length}件）`}
            </button>
            {showExecutionHistory && (
              <ul className="mt-2 space-y-1 rounded bg-gray-50 p-2 text-xs dark:bg-gray-700/50">
                {[...schedule.execution_history].reverse().map((execution, index) => (
                  <li key={execution.execution_id || `${execution.executed_at}-${index}`} className="text-gray-600 dark:text-gray-300">
                    <span className={execution.status === 'failed' ? 'text-red-600 dark:text-red-400' : ''}>
                      {formatScheduleDateTime(execution.executed_at)} · {execution.status}
                      {execution.session_reused ? ' · 再利用' : ''}
                    </span>
                    {execution.error && <div className="break-words text-red-600 dark:text-red-400">{execution.error}</div>}
                  </li>
                ))}
              </ul>
            )}
          </div>
        )}
      
        {/* Execution Count */}
        {schedule.execution_count !== undefined && schedule.execution_count > 0 && (
          <div className="flex items-center gap-2 text-gray-600 dark:text-gray-400">
            <svg className="w-4 h-4 flex-shrink-0" fill="none" stroke="currentColor" viewBox="0 0 24 24">
              <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M9 19v-6a2 2 0 00-2-2H5a2 2 0 00-2 2v6a2 2 0 002 2h2a2 2 0 002-2zm0 0V9a2 2 0 012-2h2a2 2 0 012 2v10m-6 0a2 2 0 002 2h2a2 2 0 002-2m0 0V5a2 2 0 012-2h2a2 2 0 012 2v14a2 2 0 01-2 2h-2a2 2 0 01-2-2z" />
            </svg>
            <span>実行回数: {schedule.execution_count}</span>
          </div>
        )}
      
        {/* Session Reuse Indicator */}
        {schedule.session_config?.reuse_session && (
          <div className="flex items-center gap-1.5 text-xs text-indigo-600 dark:text-indigo-400">
            <svg className="w-3.5 h-3.5 flex-shrink-0" fill="none" stroke="currentColor" viewBox="0 0 24 24">
              <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M4 4v5h.582m15.356 2A8.001 8.001 0 004.582 9m0 0H9m11 11v-5h-.581m0 0a8.003 8.003 0 01-15.357-2m15.357 2H15" />
            </svg>
            <span>セッション再利用</span>
            {schedule.last_execution?.session_reused && (
              <span className="px-1 py-0.5 bg-indigo-100 dark:bg-indigo-900/30 rounded text-indigo-700 dark:text-indigo-300">前回: 再利用</span>
            )}
          </div>
        )}
      
        {/* Session Config - Model */}
        {schedule.session_config?.params?.model && (
          <div className="flex items-center gap-1.5 text-xs text-gray-600 dark:text-gray-400">
            <span>モデル:</span>
            <span className="font-mono">{schedule.session_config.params.model}</span>
          </div>
        )}
      
        {/* Session Config - Message */}
        {schedule.session_config?.params?.message && (
          <div className="mt-2 p-2 bg-gray-50 dark:bg-gray-700/50 rounded text-xs text-gray-600 dark:text-gray-400 line-clamp-2">
            {schedule.session_config.params.message}
          </div>
        )}
      </div>
      
      
    </>
  )
}
