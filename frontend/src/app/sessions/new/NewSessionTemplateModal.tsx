'use client'
import type { NewSessionFormState } from './useNewSession'

export function NewSessionTemplateModal(context: NewSessionFormState) {
  const { setInitialMessage, templates, recentMessages, setShowTemplateModal, selectTemplate } = context
  return (
    <div
      className="fixed inset-0 bg-black bg-opacity-50 flex items-center justify-center z-50 p-4"
      onClick={(e) => {
        if (e.target === e.currentTarget) {
          setShowTemplateModal(false)
        }
      }}
    >
      <div className="bg-white dark:bg-gray-800 rounded-xl shadow-xl w-full max-w-4xl max-h-[85vh] overflow-hidden">
        <div className="px-6 py-4 border-b border-gray-200 dark:border-gray-700">
          <div className="flex items-center justify-between">
            <h2 className="text-lg font-semibold text-gray-900 dark:text-white">テンプレートから選択</h2>
            <button
              onClick={() => setShowTemplateModal(false)}
              className="text-gray-400 hover:text-gray-500 dark:hover:text-gray-300"
            >
              <svg className="w-6 h-6" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M6 18L18 6M6 6l12 12" />
              </svg>
            </button>
          </div>
        </div>

        <div className="overflow-y-auto max-h-[calc(85vh-5rem)]">
          {/* Recent Messages Section */}
          {recentMessages.length > 0 && (
            <div className="px-6 py-4 border-b border-gray-200 dark:border-gray-700">
              <h3 className="text-sm font-medium text-gray-700 dark:text-gray-300 mb-3">最近のメッセージ</h3>
              <div className="grid grid-cols-1 md:grid-cols-2 gap-3">
                {recentMessages.map((message, index) => (
                  <button
                    key={index}
                    onClick={() => {
                      setInitialMessage(message)
                      setShowTemplateModal(false)
                    }}
                    className="text-left px-4 py-3 bg-gray-50 dark:bg-gray-700 hover:bg-blue-50 dark:hover:bg-blue-900/30 rounded-lg transition-colors border border-transparent hover:border-blue-300 dark:hover:border-blue-700"
                  >
                    <div className="text-sm text-gray-900 dark:text-white line-clamp-2">{message}</div>
                  </button>
                ))}
              </div>
            </div>
          )}

          {/* Templates Section */}
          <div className="px-6 py-4">
            <h3 className="text-sm font-medium text-gray-700 dark:text-gray-300 mb-3">テンプレート</h3>
            {templates.length > 0 ? (
              <div className="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-3 gap-4">
                {templates.map((template) => (
                  <button
                    key={template.id}
                    onClick={() => selectTemplate(template)}
                    className="text-left px-4 py-4 bg-gray-50 dark:bg-gray-700 hover:bg-blue-50 dark:hover:bg-blue-900/30 rounded-lg transition-colors border border-gray-200 dark:border-gray-600 hover:border-blue-300 dark:hover:border-blue-700"
                  >
                    <div className="font-medium text-sm text-gray-900 dark:text-white flex items-center gap-2">
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
                          d="M9 12h6m-6 4h6m2 5H7a2 2 0 01-2-2V5a2 2 0 012-2h5.586a1 1 0 01.707.293l5.414 5.414a1 1 0 01.293.707V19a2 2 0 01-2 2z"
                        />
                      </svg>
                      <span className="truncate">{template.name}</span>
                    </div>
                    <div className="text-sm text-gray-600 dark:text-gray-400 mt-2 line-clamp-3">{template.content}</div>
                  </button>
                ))}
              </div>
            ) : (
              <div className="text-center py-12 text-gray-500 dark:text-gray-400">
                <svg
                  className="w-12 h-12 mx-auto mb-3 text-gray-300 dark:text-gray-600"
                  fill="none"
                  stroke="currentColor"
                  viewBox="0 0 24 24"
                >
                  <path
                    strokeLinecap="round"
                    strokeLinejoin="round"
                    strokeWidth={1.5}
                    d="M9 12h6m-6 4h6m2 5H7a2 2 0 01-2-2V5a2 2 0 012-2h5.586a1 1 0 01.707.293l5.414 5.414a1 1 0 01.293.707V19a2 2 0 01-2 2z"
                  />
                </svg>
                <p className="text-sm">テンプレートがありません</p>
                <a
                  href="/settings?tab=templates"
                  target="_blank"
                  rel="noopener noreferrer"
                  className="mt-3 inline-flex items-center gap-1 text-sm text-blue-600 dark:text-blue-400 hover:underline"
                >
                  <svg className="w-4 h-4" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                    <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M12 4v16m8-8H4" />
                  </svg>
                  テンプレートを作成
                </a>
              </div>
            )}
          </div>
        </div>
      </div>
    </div>
  )
}
