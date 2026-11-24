'use client'
import type { NewSessionFormState } from './useNewSession'

export function NewSessionRepositoryFields(context: NewSessionFormState) {
  const {
    freeFormRepository,
    isCreating,
    repositoryHistory,
    repositoryList,
    showFreeFormRepositorySuggestions,
    handleFreeFormRepositoryChange,
    handleFreeFormRepositoryFocus,
    handleFreeFormRepositoryBlur,
    selectFreeFormRepositorySuggestion,
  } = context
  return (
    <div className="relative">
      <label htmlFor="freeFormRepository" className="block text-sm font-medium text-gray-700 dark:text-gray-300 mb-2">
        対象リポジトリ
      </label>
      <input
        id="freeFormRepository"
        type="text"
        value={freeFormRepository}
        onChange={handleFreeFormRepositoryChange}
        onFocus={handleFreeFormRepositoryFocus}
        onBlur={handleFreeFormRepositoryBlur}
        className="w-full px-4 py-3 border border-gray-300 dark:border-gray-600 rounded-lg focus:outline-none focus:ring-2 focus:ring-blue-500 dark:bg-gray-700 dark:text-white text-base"
        placeholder="例: owner/repository-name"
        disabled={isCreating}
      />

      {/* サジェストドロップダウン */}
      {showFreeFormRepositorySuggestions && (repositoryHistory.length > 0 || repositoryList.length > 0) && (
        <div className="absolute top-full left-0 right-0 bg-white dark:bg-gray-800 border border-gray-300 dark:border-gray-600 rounded-lg shadow-xl z-50 mt-1 max-h-80 overflow-y-auto">
          {/* リポジトリ履歴セクション */}
          {repositoryHistory.length > 0 && (
            <>
              <div className="p-3 border-b border-gray-200 dark:border-gray-700">
                <span className="text-xs font-medium text-gray-500 dark:text-gray-400">リポジトリ履歴</span>
              </div>
              <div className="p-3 grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-3 gap-2">
                {repositoryHistory.map((suggestion, index) => (
                  <button
                    key={`history-${index}`}
                    type="button"
                    onClick={() => selectFreeFormRepositorySuggestion(suggestion)}
                    className="text-left px-3 py-2 hover:bg-blue-50 dark:hover:bg-blue-900/30 text-gray-900 dark:text-white font-mono text-sm rounded-md border border-gray-200 dark:border-gray-600 hover:border-blue-300 dark:hover:border-blue-700 transition-colors break-all"
                    title={suggestion}
                  >
                    <span className="flex items-center gap-2">
                      <svg
                        className="w-4 h-4 text-blue-500 flex-shrink-0"
                        fill="none"
                        stroke="currentColor"
                        viewBox="0 0 24 24"
                      >
                        <path
                          strokeLinecap="round"
                          strokeLinejoin="round"
                          strokeWidth={2}
                          d="M12 8v4l3 3m6-3a9 9 0 11-18 0 9 9 0 0118 0z"
                        />
                      </svg>
                      <span className="break-all">{suggestion}</span>
                    </span>
                  </button>
                ))}
              </div>
            </>
          )}

          {/* リポジトリ一覧セクション */}
          {repositoryList.length > 0 && (
            <>
              <div className="p-3 border-b border-gray-200 dark:border-gray-700">
                <span className="text-xs font-medium text-gray-500 dark:text-gray-400">リポジトリ一覧</span>
              </div>
              <div className="p-3 grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-3 gap-2">
                {repositoryList.map((suggestion, index) => (
                  <button
                    key={`repo-${index}`}
                    type="button"
                    onClick={() => selectFreeFormRepositorySuggestion(suggestion)}
                    className="text-left px-3 py-2 hover:bg-blue-50 dark:hover:bg-blue-900/30 text-gray-900 dark:text-white font-mono text-sm rounded-md border border-gray-200 dark:border-gray-600 hover:border-blue-300 dark:hover:border-blue-700 transition-colors break-all"
                    title={suggestion}
                  >
                    <span className="flex items-center gap-2">
                      <svg
                        className="w-4 h-4 text-gray-400 flex-shrink-0"
                        fill="none"
                        stroke="currentColor"
                        viewBox="0 0 24 24"
                      >
                        <path
                          strokeLinecap="round"
                          strokeLinejoin="round"
                          strokeWidth={2}
                          d="M3 7v10a2 2 0 002 2h14a2 2 0 002-2V9a2 2 0 00-2-2h-6l-2-2H5a2 2 0 00-2 2z"
                        />
                      </svg>
                      <span className="break-all">{suggestion}</span>
                    </span>
                  </button>
                ))}
              </div>
            </>
          )}
        </div>
      )}

      <p className="text-sm text-gray-500 dark:text-gray-400 mt-2">
        リポジトリを指定しない場合は一般的なチャットになります
      </p>
    </div>
  )
}
