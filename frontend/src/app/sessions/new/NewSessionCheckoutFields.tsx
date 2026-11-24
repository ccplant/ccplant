'use client'
import type { NewSessionFormState } from './useNewSession'

export function NewSessionCheckoutFields(context: NewSessionFormState) {
  const {
    freeFormRepository,
    checkoutTarget,
    checkoutBranch,
    setCheckoutBranch,
    checkoutPrNumber,
    setCheckoutPrNumber,
    isCreating,
    handleCheckoutTargetChange,
  } = context
  return (
    <div>
      <div className="flex items-center justify-between gap-3 mb-2">
        <label className="block text-sm font-medium text-gray-700 dark:text-gray-300">Checkout 対象</label>
        {!freeFormRepository.trim() && (
          <span className="text-xs text-gray-400 dark:text-gray-500">リポジトリ指定時に有効</span>
        )}
      </div>
      <div className="flex flex-wrap gap-2">
        <button
          type="button"
          onClick={() => handleCheckoutTargetChange('branch')}
          disabled={isCreating || !freeFormRepository.trim()}
          aria-pressed={checkoutTarget === 'branch'}
          className={`px-3 py-2 rounded-lg border text-sm font-medium transition-colors disabled:opacity-50 disabled:cursor-not-allowed ${
            checkoutTarget === 'branch'
              ? 'border-blue-500 bg-blue-50 text-blue-700 dark:bg-blue-900/20 dark:text-blue-300'
              : 'border-gray-300 dark:border-gray-600 text-gray-700 dark:text-gray-300 hover:border-blue-300 dark:hover:border-blue-700'
          }`}
        >
          ブランチ
        </button>
        <button
          type="button"
          onClick={() => handleCheckoutTargetChange('pr')}
          disabled={isCreating || !freeFormRepository.trim()}
          aria-pressed={checkoutTarget === 'pr'}
          className={`px-3 py-2 rounded-lg border text-sm font-medium transition-colors disabled:opacity-50 disabled:cursor-not-allowed ${
            checkoutTarget === 'pr'
              ? 'border-blue-500 bg-blue-50 text-blue-700 dark:bg-blue-900/20 dark:text-blue-300'
              : 'border-gray-300 dark:border-gray-600 text-gray-700 dark:text-gray-300 hover:border-blue-300 dark:hover:border-blue-700'
          }`}
        >
          Pull Request
        </button>
      </div>

      {freeFormRepository.trim() && checkoutTarget === 'branch' && (
        <div className="mt-3">
          <label className="block text-xs font-medium text-gray-600 dark:text-gray-400 mb-1">ブランチ名</label>
          <input
            type="text"
            value={checkoutBranch}
            onChange={(e) => {
              setCheckoutBranch(e.target.value)
              if (e.target.value.trim()) setCheckoutPrNumber('')
            }}
            placeholder="例: feature/session-form"
            disabled={isCreating || !freeFormRepository.trim()}
            className="w-full px-3 py-2 border border-gray-300 dark:border-gray-600 rounded-lg focus:outline-none focus:ring-2 focus:ring-blue-500 dark:bg-gray-700 dark:text-white text-sm font-mono disabled:bg-gray-100 dark:disabled:bg-gray-800 disabled:text-gray-400"
          />
        </div>
      )}

      {freeFormRepository.trim() && checkoutTarget === 'pr' && (
        <div className="mt-3">
          <label className="block text-xs font-medium text-gray-600 dark:text-gray-400 mb-1">PR 番号</label>
          <input
            type="text"
            inputMode="numeric"
            value={checkoutPrNumber}
            onChange={(e) => {
              setCheckoutPrNumber(e.target.value)
              if (e.target.value.trim()) setCheckoutBranch('')
            }}
            placeholder="例: 123"
            disabled={isCreating || !freeFormRepository.trim()}
            className="w-full px-3 py-2 border border-gray-300 dark:border-gray-600 rounded-lg focus:outline-none focus:ring-2 focus:ring-blue-500 dark:bg-gray-700 dark:text-white text-sm font-mono disabled:bg-gray-100 dark:disabled:bg-gray-800 disabled:text-gray-400"
          />
        </div>
      )}
    </div>
  )
}
