'use client';

import type { useAgentChat } from './useAgentChat';

export function AgentChatControls(state: ReturnType<typeof useAgentChat>) {
  const { isLoading, isConnected, showControlPanel, sendStopSignal, sendArrowUp, sendArrowDown, sendEnterKey } = state;
  return (
    <>
      {showControlPanel && (
        <div className="mb-4 p-3 bg-gray-50 dark:bg-gray-800 border border-gray-200 dark:border-gray-700 rounded-md">
          <h3 className="text-sm font-medium text-gray-900 dark:text-white mb-3">Agent Controls</h3>
          <div className="flex flex-wrap gap-2">
            {/* Arrow Up Button */}
            <button
              onClick={sendArrowUp}
              disabled={!isConnected || isLoading}
              className="px-2 py-1 bg-blue-600 hover:bg-blue-700 disabled:bg-gray-300 dark:disabled:bg-gray-600 text-white text-xs rounded-md transition-colors disabled:cursor-not-allowed flex items-center space-x-1"
              title="Send Up Arrow Key"
            >
              <svg className="w-3 h-3" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M5 15l7-7 7 7" />
              </svg>
              <span>↑</span>
            </button>

            {/* Arrow Down Button */}
            <button
              onClick={sendArrowDown}
              disabled={!isConnected || isLoading}
              className="px-2 py-1 bg-blue-600 hover:bg-blue-700 disabled:bg-gray-300 dark:disabled:bg-gray-600 text-white text-xs rounded-md transition-colors disabled:cursor-not-allowed flex items-center space-x-1"
              title="Send Down Arrow Key"
            >
              <svg className="w-3 h-3" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M19 9l-7 7-7-7" />
              </svg>
              <span>↓</span>
            </button>

            {/* Enter Button */}
            <button
              onClick={sendEnterKey}
              disabled={!isConnected || isLoading}
              className="px-2 py-1 bg-green-600 hover:bg-green-700 disabled:bg-gray-300 dark:disabled:bg-gray-600 text-white text-xs rounded-md transition-colors disabled:cursor-not-allowed flex items-center space-x-1"
              title="Send Enter Key"
            >
              <svg className="w-3 h-3" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                <path
                  strokeLinecap="round"
                  strokeLinejoin="round"
                  strokeWidth={2}
                  d="M9 12l2 2 4-4m6 2a9 9 0 11-18 0 9 9 0 0118 0z"
                />
              </svg>
              <span>Enter</span>
            </button>

            {/* ESC Button (existing functionality) */}
            <button
              onClick={sendStopSignal}
              disabled={!isConnected || isLoading}
              className="px-2 py-1 bg-red-600 hover:bg-red-700 disabled:bg-gray-300 dark:disabled:bg-gray-600 text-white text-xs rounded-md transition-colors disabled:cursor-not-allowed flex items-center space-x-1"
              title="Send ESC Key (Force Stop)"
            >
              <svg className="w-3 h-3" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                <path
                  strokeLinecap="round"
                  strokeLinejoin="round"
                  strokeWidth={2}
                  d="M10 14l2-2m0 0l2-2m-2 2l-2-2m2 2l2 2m7-2a9 9 0 11-18 0 9 9 0 0118 0z"
                />
              </svg>
              <span>ESC</span>
            </button>
          </div>
        </div>
      )}
    </>
  );
}
