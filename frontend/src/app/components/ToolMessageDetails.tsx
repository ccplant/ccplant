'use client';
import type { ToolMessageState } from './toolMessageState';

export function ToolMessageDetails(context: ToolMessageState) {
  const { toolResult, result, isSuccess, inputForDisplay, hasInput } = context;
  return (
    <div className="ml-3 mt-1 mb-2 text-xs">
      {/* ツール入力 */}
      {hasInput && (
        <div className="mb-2">
          <div className="font-semibold text-gray-700 dark:text-gray-300 mb-1">Input:</div>
          <pre className="bg-gray-50 dark:bg-gray-800 p-2 rounded text-gray-600 dark:text-gray-400 overflow-x-auto">
            {JSON.stringify(inputForDisplay, null, 2)}
          </pre>
        </div>
      )}

      {/* ツール結果 */}
      {toolResult && (
        <div>
          <div
            className={`font-semibold mb-1 ${
              isSuccess ? 'text-green-700 dark:text-green-300' : 'text-red-700 dark:text-red-300'
            }`}
          >
            {isSuccess ? 'Result:' : 'Error:'}
          </div>
          <pre
            className={`p-2 rounded overflow-x-auto ${
              isSuccess
                ? 'bg-green-50 dark:bg-green-900/20 text-green-700 dark:text-green-300'
                : 'bg-red-50 dark:bg-red-900/20 text-red-700 dark:text-red-300'
            }`}
          >
            {result?.error || result?.result || toolResult.content}
          </pre>
        </div>
      )}
    </div>
  );
}
