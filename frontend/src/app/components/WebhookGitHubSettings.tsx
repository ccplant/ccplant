'use client'

import { GITHUB_EVENTS } from '../../types/webhook'
import type { useWebhookForm } from './useWebhookForm'

export function WebhookGitHubSettings(props: ReturnType<typeof useWebhookForm>) {
  const {
    webhookType,
    allowedEvents,
    allowedRepositories,
    repositorySuggestions,
    showRepositorySuggestions,
    isSubmitting,
    handleRepositoryChange,
    handleRepositoryFocus,
    handleRepositoryBlur,
    selectRepositorySuggestion,
    toggleAllowedEvent,
  } = props
  return (
    <>
      {webhookType === 'github' && (
        <div className="space-y-4">
          <h3 className="text-sm font-medium text-gray-700 dark:text-gray-300 border-b border-gray-200 dark:border-gray-700 pb-2">
            GitHub設定
          </h3>

          {/* Allowed Events */}
          <div>
            <label className="block text-sm font-medium text-gray-700 dark:text-gray-300 mb-2">許可イベント</label>
            <div className="flex flex-wrap gap-2">
              {GITHUB_EVENTS.map((event) => (
                <button
                  key={event.value}
                  type="button"
                  onClick={() => toggleAllowedEvent(event.value)}
                  className={`px-3 py-1.5 text-sm rounded-md border transition-colors ${
                    allowedEvents.includes(event.value)
                      ? 'bg-blue-100 dark:bg-blue-900/30 border-blue-300 dark:border-blue-700 text-blue-700 dark:text-blue-300'
                      : 'bg-gray-50 dark:bg-gray-700 border-gray-200 dark:border-gray-600 text-gray-700 dark:text-gray-300 hover:bg-gray-100 dark:hover:bg-gray-600'
                  }`}
                  title={event.description}
                >
                  {event.label}
                </button>
              ))}
            </div>
            <p className="text-xs text-gray-500 dark:text-gray-400 mt-1">空の場合、すべてのイベントを許可します</p>
          </div>

          {/* Allowed Repositories */}
          <div className="relative">
            <label
              htmlFor="allowedRepositories"
              className="block text-sm font-medium text-gray-700 dark:text-gray-300 mb-1"
            >
              許可リポジトリ
            </label>
            <input
              id="allowedRepositories"
              type="text"
              value={allowedRepositories}
              onChange={handleRepositoryChange}
              onFocus={handleRepositoryFocus}
              onBlur={handleRepositoryBlur}
              placeholder="例: owner/repo, owner/*"
              disabled={isSubmitting}
              className="w-full px-3 py-2 border border-gray-300 dark:border-gray-600 rounded-md focus:outline-none focus:ring-2 focus:ring-blue-500 dark:bg-gray-700 dark:text-white"
            />

            {/* Repository Suggestions */}
            {showRepositorySuggestions && repositorySuggestions.length > 0 && (
              <div className="absolute z-10 mt-1 w-full bg-white dark:bg-gray-800 border border-gray-200 dark:border-gray-700 rounded-md shadow-lg max-h-40 overflow-y-auto">
                {repositorySuggestions.map((suggestion, index) => (
                  <button
                    key={index}
                    type="button"
                    onClick={() => selectRepositorySuggestion(suggestion)}
                    className="w-full text-left px-3 py-2 text-sm hover:bg-gray-50 dark:hover:bg-gray-700 text-gray-900 dark:text-white font-mono"
                  >
                    {suggestion}
                  </button>
                ))}
              </div>
            )}

            <p className="text-xs text-gray-500 dark:text-gray-400 mt-1">
              カンマ区切りで複数指定可。空の場合、すべてのリポジトリを許可します
            </p>
          </div>
        </div>
      )}
    </>
  )
}
