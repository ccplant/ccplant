'use client';
import type { MessageExpansionProps, MessageItemProps } from './messageItemTypes';

export function ToolResultMessage({
  message,
  formatTimestamp,
  isExpanded,
  setIsExpanded,
}: MessageItemProps & MessageExpansionProps) {
  const isSuccess = message.status === 'success';
  const contentPreview = message.content.length > 60 ? message.content.substring(0, 60) + '...' : message.content;

  return (
    <div className="px-4 sm:px-6 py-0.5">
      <button
        onClick={() => setIsExpanded(!isExpanded)}
        className="w-full flex items-center py-1 hover:opacity-80 transition-opacity group text-left"
      >
        {/* 細い縦線 */}
        <div
          className={`w-px h-4 mr-2 flex-shrink-0 ${
            isSuccess ? 'bg-green-400 dark:bg-green-500' : 'bg-red-400 dark:bg-red-500'
          }`}
        ></div>

        {/* ステータスアイコン */}
        <span
          className={`text-xs flex-shrink-0 ${
            isSuccess ? 'text-green-600 dark:text-green-400' : 'text-red-600 dark:text-red-400'
          }`}
        >
          {isSuccess ? '✓' : '✗'}
        </span>

        {/* 内容プレビュー */}
        <span className="ml-2 text-xs text-gray-400 dark:text-gray-500 truncate">{contentPreview}</span>

        {/* タイムスタンプ */}
        <span className="ml-auto text-xs text-gray-400 dark:text-gray-500 flex-shrink-0">
          {formatTimestamp(message.timestamp || message.time || '')}
        </span>

        {/* 展開アイコン */}
        <span className="ml-2 text-xs text-gray-400 flex-shrink-0">{isExpanded ? '−' : '+'}</span>
      </button>

      {/* 展開された詳細 */}
      {isExpanded && (
        <div className="ml-3 mt-1 mb-2 text-xs">
          <pre
            className={`p-2 rounded overflow-x-auto whitespace-pre-wrap break-words ${
              isSuccess
                ? 'bg-green-50 dark:bg-green-900/20 text-green-700 dark:text-green-300'
                : 'bg-red-50 dark:bg-red-900/20 text-red-700 dark:text-red-300'
            }`}
          >
            {message.content}
          </pre>
        </div>
      )}
    </div>
  );
}
