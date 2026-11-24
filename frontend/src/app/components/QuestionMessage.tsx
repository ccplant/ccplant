'use client';
import type { MessageItemProps } from './messageItemTypes';
import { renderContent } from './messageRendering';

export function QuestionMessage({ message, formatTimestamp, isClaudeAgent }: MessageItemProps) {
  return (
    <div className="px-4 sm:px-6 py-4 bg-blue-50 dark:bg-blue-900/20 border-l-4 border-blue-500">
      <div className="flex items-start space-x-2">
        <div className="flex-shrink-0">
          <svg className="w-6 h-6 text-blue-600 dark:text-blue-400" fill="currentColor" viewBox="0 0 24 24">
            <path d="M12 2C6.48 2 2 6.48 2 12s4.48 10 10 10 10-4.48 10-10S17.52 2 12 2zm1 15h-2v-2h2v2zm0-4h-2V7h2v6z" />
          </svg>
        </div>
        <div className="flex-1 min-w-0">
          <div className="flex items-center space-x-2 mb-2">
            <span className="text-sm font-medium text-blue-700 dark:text-blue-300">❓ Question</span>
            <span className="text-xs text-gray-500 dark:text-gray-400">
              {formatTimestamp(message.timestamp || message.time || '')}
            </span>
          </div>
          <div className="text-gray-700 dark:text-gray-300">{renderContent(message.content, isClaudeAgent)}</div>
        </div>
      </div>
    </div>
  );
}
