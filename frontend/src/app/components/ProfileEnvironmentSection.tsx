'use client'

import type { useSessionProfileEditor } from './useSessionProfileEditor'
import type { SessionProfileEditorProps } from './SessionProfileEditor'

type Props = Pick<
  ReturnType<typeof useSessionProfileEditor>,
  'envPairs' | 'setEnvPairs' | 'updatePair' | 'addPair' | 'removePair'
> &
  Pick<SessionProfileEditorProps, never>

export function ProfileEnvironmentSection({ envPairs, setEnvPairs, updatePair, addPair, removePair }: Props) {
  return (
    <div className="space-y-5">
      {/* Environment Variables */}
      <div>
        <label className="block text-sm font-medium text-gray-700 dark:text-gray-300 mb-2">環境変数</label>
        <div className="space-y-2">
          {envPairs.map((pair, idx) => (
            <div
              key={idx}
              className="rounded border border-gray-200 dark:border-gray-600 p-2 space-y-1.5 bg-gray-50 dark:bg-gray-900/40"
            >
              <input
                type="text"
                value={pair.key}
                onChange={(e) => updatePair(envPairs, setEnvPairs, idx, 'key', e.target.value)}
                placeholder="KEY"
                className="w-full px-2 py-1.5 border border-gray-300 dark:border-gray-600 rounded text-xs bg-white dark:bg-gray-700 text-gray-900 dark:text-white font-mono focus:outline-none focus:ring-1 focus:ring-blue-500"
              />
              <div className="flex items-center gap-2">
                <input
                  type="text"
                  value={pair.value}
                  onChange={(e) => updatePair(envPairs, setEnvPairs, idx, 'value', e.target.value)}
                  placeholder="value"
                  className="flex-1 min-w-0 px-2 py-1.5 border border-gray-300 dark:border-gray-600 rounded text-xs bg-white dark:bg-gray-700 text-gray-900 dark:text-white font-mono focus:outline-none focus:ring-1 focus:ring-blue-500"
                />
                <button
                  type="button"
                  onClick={() => removePair(envPairs, setEnvPairs, idx)}
                  className="shrink-0 p-1 text-gray-400 hover:text-red-500 dark:hover:text-red-400"
                >
                  <svg className="w-4 h-4" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                    <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M6 18L18 6M6 6l12 12" />
                  </svg>
                </button>
              </div>
            </div>
          ))}
          <button
            type="button"
            onClick={() => addPair(setEnvPairs)}
            className="text-xs text-blue-600 dark:text-blue-400 hover:text-blue-800 dark:hover:text-blue-300 flex items-center gap-1"
          >
            <svg className="w-3 h-3" fill="none" stroke="currentColor" viewBox="0 0 24 24">
              <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M12 6v6m0 0v6m0-6h6m-6 0H6" />
            </svg>
            追加
          </button>
        </div>
      </div>
    </div>
  )
}
