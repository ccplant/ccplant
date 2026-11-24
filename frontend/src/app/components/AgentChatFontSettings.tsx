'use client';

import type { useAgentChat } from './useAgentChat';

export function AgentChatFontSettings(state: ReturnType<typeof useAgentChat>) {
  const { fontSettings, showFontSettings, setShowFontSettings, handleFontSizeChange, handleFontFamilyChange } = state;
  return (
    <>
      {showFontSettings && (
        <div
          className="fixed inset-0 bg-black bg-opacity-50 flex items-center justify-center z-50 p-4"
          onClick={(e) => {
            if (e.target === e.currentTarget) {
              setShowFontSettings(false);
            }
          }}
        >
          <div className="bg-white dark:bg-gray-800 rounded-lg shadow-xl max-w-md w-full">
            <div className="px-6 py-4 border-b border-gray-200 dark:border-gray-700">
              <div className="flex items-center justify-between">
                <h2 className="text-lg font-semibold text-gray-900 dark:text-white">フォント設定</h2>
                <button
                  onClick={() => setShowFontSettings(false)}
                  className="text-gray-400 hover:text-gray-500 dark:hover:text-gray-300"
                >
                  <svg className="w-5 h-5" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                    <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M6 18L18 6M6 6l12 12" />
                  </svg>
                </button>
              </div>
            </div>

            <div className="px-6 py-4 space-y-6">
              {/* Font Size Slider */}
              <div>
                <label className="block text-sm font-medium text-gray-700 dark:text-gray-300 mb-2">
                  フォントサイズ: {fontSettings.fontSize}px
                </label>
                <input
                  type="range"
                  min="12"
                  max="20"
                  value={fontSettings.fontSize}
                  onChange={(e) => handleFontSizeChange(parseInt(e.target.value))}
                  className="w-full h-2 bg-gray-200 dark:bg-gray-700 rounded-lg appearance-none cursor-pointer accent-purple-600"
                />
                <div className="flex justify-between text-xs text-gray-500 dark:text-gray-400 mt-1">
                  <span>12px</span>
                  <span>20px</span>
                </div>
              </div>

              {/* Font Family */}
              <div>
                <label className="block text-sm font-medium text-gray-700 dark:text-gray-300 mb-3">
                  フォントファミリー
                </label>
                <div className="space-y-2">
                  <label
                    className={`flex items-center p-3 rounded-lg border cursor-pointer transition-colors ${
                      fontSettings.fontFamily === 'sans-serif'
                        ? 'border-purple-500 bg-purple-50 dark:bg-purple-900/20'
                        : 'border-gray-200 dark:border-gray-700 hover:bg-gray-50 dark:hover:bg-gray-800'
                    }`}
                  >
                    <input
                      type="radio"
                      name="fontFamily"
                      value="sans-serif"
                      checked={fontSettings.fontFamily === 'sans-serif'}
                      onChange={() => handleFontFamilyChange('sans-serif')}
                      className="h-4 w-4 text-purple-600 focus:ring-purple-500"
                    />
                    <div className="ml-3">
                      <span className="block text-sm font-medium text-gray-900 dark:text-white">Sans-serif</span>
                      <span className="block text-xs text-gray-500 dark:text-gray-400">
                        通常のフォント（読みやすさ重視）
                      </span>
                    </div>
                  </label>

                  <label
                    className={`flex items-center p-3 rounded-lg border cursor-pointer transition-colors ${
                      fontSettings.fontFamily === 'monospace'
                        ? 'border-purple-500 bg-purple-50 dark:bg-purple-900/20'
                        : 'border-gray-200 dark:border-gray-700 hover:bg-gray-50 dark:hover:bg-gray-800'
                    }`}
                  >
                    <input
                      type="radio"
                      name="fontFamily"
                      value="monospace"
                      checked={fontSettings.fontFamily === 'monospace'}
                      onChange={() => handleFontFamilyChange('monospace')}
                      className="h-4 w-4 text-purple-600 focus:ring-purple-500"
                    />
                    <div className="ml-3">
                      <span className="block text-sm font-medium text-gray-900 dark:text-white font-mono">
                        Monospace
                      </span>
                      <span className="block text-xs text-gray-500 dark:text-gray-400">
                        等幅フォント（コード表示に適している）
                      </span>
                    </div>
                  </label>
                </div>
              </div>
            </div>

            <div className="px-6 py-4 border-t border-gray-200 dark:border-gray-700">
              <div className="flex justify-end">
                <button
                  onClick={() => setShowFontSettings(false)}
                  className="px-4 py-2 bg-purple-600 hover:bg-purple-700 text-white rounded-md transition-colors"
                >
                  閉じる
                </button>
              </div>
            </div>
          </div>
        </div>
      )}
    </>
  );
}
