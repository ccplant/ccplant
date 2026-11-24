'use client';
import { CopyableMessageImage } from './CopyableMessageImage';
import { ToolMessageDetails } from './ToolMessageDetails';
import { buildToolMessageState, type ToolUseMessageProps } from './toolMessageState';

export function ToolUseMessage(props: ToolUseMessageProps) {
  const context = buildToolMessageState(props);
  const { message, toolResult, formatTimestamp, isExpanded, setIsExpanded, toolUse, isSuccess, hasResult, briefInfo } =
    context;
  return (
    <div className="px-4 sm:px-6 py-0.5">
      <button
        onClick={() => setIsExpanded(!isExpanded)}
        className="w-full flex items-center py-1 hover:opacity-80 transition-opacity group text-left"
      >
        {/* 細い縦線 */}
        <div
          className={`w-px h-4 mr-2 flex-shrink-0 ${
            hasResult
              ? isSuccess
                ? 'bg-green-400 dark:bg-green-500'
                : 'bg-red-400 dark:bg-red-500'
              : 'bg-gray-300 dark:bg-gray-600'
          }`}
        ></div>

        {/* ツール名 */}
        <span
          className={`text-xs flex-shrink-0 max-w-[160px] truncate ${
            hasResult
              ? isSuccess
                ? 'text-green-600 dark:text-green-400'
                : 'text-red-600 dark:text-red-400'
              : 'text-gray-500 dark:text-gray-400'
          }`}
          title={toolUse.name}
        >
          {toolUse.name}
        </span>

        {/* 結果インジケーター */}
        {hasResult && (
          <span
            className={`ml-1 text-xs flex-shrink-0 ${
              isSuccess ? 'text-green-600 dark:text-green-400' : 'text-red-600 dark:text-red-400'
            }`}
          >
            {isSuccess ? '✓' : '✗'}
          </span>
        )}

        {/* 簡易情報 */}
        {briefInfo && <span className="ml-2 text-xs text-gray-400 dark:text-gray-500 truncate">{briefInfo}</span>}

        {/* タイムスタンプ */}
        <span className="ml-auto text-xs text-gray-400 dark:text-gray-500 flex-shrink-0">
          {formatTimestamp(message.timestamp || message.time || '')}
        </span>

        {/* 展開アイコン */}
        <span className="ml-2 text-xs text-gray-400 flex-shrink-0">{isExpanded ? '−' : '+'}</span>
      </button>

      {/* 展開された詳細 */}
      {isExpanded && <ToolMessageDetails {...context} />}
      {toolResult?.images && toolResult.images.length > 0 && (
        <div className="ml-3 mt-2 mb-2 grid grid-cols-1 gap-2 sm:grid-cols-2">
          {toolResult.images.map((image, index) => (
            <CopyableMessageImage
              key={`${image.mimeType}:${index}`}
              mimeType={image.mimeType}
              data={image.data}
              alt={`Agent output image ${index + 1}`}
            />
          ))}
        </div>
      )}
    </div>
  );
}
