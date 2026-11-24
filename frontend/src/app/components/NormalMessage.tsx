'use client';
import { MessageAvatar } from './MessageAvatar';
import { MessageThought } from './MessageThought';
import { CopyableMessageImage } from './CopyableMessageImage';
import type { MessageItemProps, MessageThoughtProps } from './messageItemTypes';
import { renderContent } from './messageRendering';

export function NormalMessage({
  message,
  formatTimestamp,
  fontSettings,
  isClaudeAgent,
  isThoughtExpanded,
  setIsThoughtExpanded,
}: MessageItemProps & MessageThoughtProps) {
  const role = message.role === 'assistant' || message.role === 'agent' ? 'agent' : message.role;
  return (
    <div className="px-4 sm:px-6 py-4">
      <div className="flex items-start space-x-2 sm:space-x-3">
        <div className="flex-shrink-0">
          <MessageAvatar role={role} />
        </div>
        <div className="flex-1 min-w-0">
          <div className="flex items-center space-x-1 sm:space-x-2 mb-2">
            <span className="text-sm font-medium text-gray-900 dark:text-white">
              {role === 'user' ? 'You' : 'AgentAPI'}
            </span>
            <span className="text-xs text-gray-500 dark:text-gray-400">
              {formatTimestamp(message.timestamp || message.time || '')}
            </span>
          </div>

          {/* ACP agent thought — subtle collapsible link before the main content */}
          {message.thought && role !== 'user' && (
            <MessageThought
              message={message}
              isThoughtExpanded={isThoughtExpanded}
              setIsThoughtExpanded={setIsThoughtExpanded}
            />
          )}

          <div
            className={`text-gray-700 dark:text-gray-300 ${fontSettings.fontFamily === 'monospace' ? 'font-mono' : ''}`}
            style={{ fontSize: `${fontSettings.fontSize}px` }}
          >
            {renderContent(message.content, isClaudeAgent)}
          </div>
          {message.images && message.images.length > 0 && (
            <div className="mt-3 grid grid-cols-1 gap-2 sm:grid-cols-2">
              {message.images.map((image, index) => (
                <CopyableMessageImage
                  key={`${image.mimeType}:${index}`}
                  mimeType={image.mimeType}
                  data={image.data}
                  alt={`Message image ${index + 1}`}
                />
              ))}
            </div>
          )}
        </div>
      </div>
    </div>
  );
}
