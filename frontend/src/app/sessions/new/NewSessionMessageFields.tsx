'use client'
import type { NewSessionFormState } from './useNewSession'

export function NewSessionMessageFields(context: NewSessionFormState) {
  const { initialMessage, setInitialMessage, isCreating, setShowTemplateModal } = context
  return (
    <div className="relative">
      <label htmlFor="initialMessage" className="block text-sm font-medium text-gray-700 dark:text-gray-300 mb-2">
        初期メッセージ <span className="text-red-500">*</span>
      </label>
      <div className="flex items-start gap-3">
        <textarea
          id="initialMessage"
          value={initialMessage}
          onChange={(e) => setInitialMessage(e.target.value)}
          placeholder="このセッションで何をしたいか説明してください..."
          className="flex-1 px-4 py-3 border border-gray-300 dark:border-gray-600 rounded-lg focus:outline-none focus:ring-2 focus:ring-blue-500 dark:bg-gray-700 dark:text-white resize-y min-h-[200px] text-base"
          disabled={isCreating}
          required
        />
        <button
          type="button"
          onClick={() => setShowTemplateModal(true)}
          className="px-4 py-3 bg-blue-600 hover:bg-blue-700 text-white rounded-lg transition-colors flex items-center flex-shrink-0"
          title="テンプレートから選択"
          disabled={isCreating}
        >
          <svg className="w-5 h-5" fill="none" stroke="currentColor" viewBox="0 0 24 24">
            <path
              strokeLinecap="round"
              strokeLinejoin="round"
              strokeWidth={2}
              d="M9 12h6m-6 4h6m2 5H7a2 2 0 01-2-2V5a2 2 0 012-2h5.586a1 1 0 01.707.293l5.414 5.414a1 1 0 01.293.707V19a2 2 0 01-2 2z"
            />
          </svg>
        </button>
      </div>
    </div>
  )
}
