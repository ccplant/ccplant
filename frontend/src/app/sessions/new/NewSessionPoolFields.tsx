'use client'
import type { NewSessionFormState } from './useNewSession'

export function NewSessionPoolFields(context: NewSessionFormState) {
  const { isCreating, availablePools, selectedManagerId, setSelectedManagerId } = context
  return (
    <div>
      <p className="text-xs font-medium text-gray-600 dark:text-gray-400 mb-2">実行Pool</p>
      <div className="space-y-1.5">
        {/* Pool の自動選択 */}
        <label
          className={`flex items-start gap-3 p-2.5 rounded-lg border cursor-pointer transition-colors ${
            selectedManagerId === ''
              ? 'border-blue-500 bg-blue-50 dark:bg-blue-900/20'
              : 'border-gray-200 dark:border-gray-600 hover:border-gray-300 dark:hover:border-gray-500 hover:bg-gray-50 dark:hover:bg-gray-700/50'
          } ${isCreating ? 'opacity-50 cursor-not-allowed' : ''}`}
        >
          <input
            type="radio"
            name="session-manager"
            value=""
            checked={selectedManagerId === ''}
            onChange={() => setSelectedManagerId('')}
            disabled={isCreating}
            className="mt-0.5 w-3.5 h-3.5 text-blue-600 border-gray-300 dark:border-gray-600 focus:ring-blue-500 flex-shrink-0"
          />
          <span className="flex-1 min-w-0">
            <span className="block text-xs font-medium text-gray-800 dark:text-gray-200">自動選択</span>
            <span className="block text-xs text-gray-400 dark:text-gray-500 mt-0.5">
              利用可能なPoolから自動的に選択
            </span>
          </span>
        </label>
        {/* 各Poolオプション */}
        {availablePools.map((pool) => (
          <label
            key={pool.name}
            className={`flex items-start gap-3 p-2.5 rounded-lg border cursor-pointer transition-colors ${
              selectedManagerId === pool.name
                ? 'border-blue-500 bg-blue-50 dark:bg-blue-900/20'
                : 'border-gray-200 dark:border-gray-600 hover:border-gray-300 dark:hover:border-gray-500 hover:bg-gray-50 dark:hover:bg-gray-700/50'
            } ${isCreating ? 'opacity-50 cursor-not-allowed' : ''}`}
          >
            <input
              type="radio"
              name="session-manager"
              value={pool.name}
              checked={selectedManagerId === pool.name}
              onChange={() => setSelectedManagerId(pool.name)}
              disabled={isCreating}
              className="mt-0.5 w-3.5 h-3.5 text-blue-600 border-gray-300 dark:border-gray-600 focus:ring-blue-500 flex-shrink-0"
            />
            <span className="flex-1 min-w-0">
              <span className="flex items-center gap-1.5 flex-wrap">
                <span className="text-xs font-medium text-gray-800 dark:text-gray-200 truncate">{pool.name}</span>
              </span>
              <span className="block text-xs text-gray-400 dark:text-gray-500 mt-0.5">
                Poolに割り当てられたManagerで実行
              </span>
            </span>
          </label>
        ))}
      </div>
    </div>
  )
}
