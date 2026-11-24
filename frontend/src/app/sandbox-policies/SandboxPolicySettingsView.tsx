'use client'
import { SandboxPolicyBasicFields } from './SandboxPolicyBasicFields'
import { SandboxPolicyDomainFields } from './SandboxPolicyDomainFields'
import { SandboxPolicyEvaluationFields } from './SandboxPolicyEvaluationFields'
import type { SandboxPolicySettingsState } from './useSandboxPolicySettings'
export function SandboxPolicySettingsView(context: SandboxPolicySettingsState) {
  const { onClose, editing, activeTab, setActiveTab, isSubmitting, error, handleSubmit, tabs } = context
  return (
    <div className="space-y-5">
      <div className="flex flex-col gap-4 sm:flex-row sm:items-start sm:justify-between">
        <div className="min-w-0">
          <button
            type="button"
            onClick={onClose}
            className="mb-3 inline-flex items-center gap-1.5 text-sm font-medium text-gray-500 hover:text-gray-800 dark:text-gray-400 dark:hover:text-gray-200"
          >
            <svg className="h-4 w-4" fill="none" stroke="currentColor" viewBox="0 0 24 24">
              <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M15 19l-7-7 7-7" />
            </svg>
            一覧へ戻る
          </button>
          <h1 className="text-xl font-bold text-gray-900 dark:text-white">
            {editing ? 'サンドボックスポリシーを編集' : '新しいサンドボックスポリシー'}
          </h1>
          <p className="mt-1 text-sm text-gray-500 dark:text-gray-400">
            ネットワークフィルターの対象ドメインと適用方法を設定します。
          </p>
        </div>
        <div className="grid grid-cols-2 gap-2 sm:flex sm:shrink-0 sm:items-center">
          <button
            type="button"
            onClick={onClose}
            disabled={isSubmitting}
            className="px-4 py-2 text-sm text-gray-700 dark:text-gray-300 bg-white dark:bg-gray-800 border border-gray-300 dark:border-gray-600 rounded-lg hover:bg-gray-50 dark:hover:bg-gray-700 transition-colors disabled:opacity-50"
          >
            キャンセル
          </button>
          <button
            type="submit"
            form="sandbox-policy-settings-form"
            disabled={isSubmitting}
            className="px-4 py-2 text-sm text-white bg-blue-600 hover:bg-blue-700 rounded-lg transition-colors disabled:opacity-50"
          >
            {isSubmitting ? '保存中...' : editing ? '保存' : '作成'}
          </button>
        </div>
      </div>

      <div className="grid gap-5 lg:grid-cols-[220px_minmax(0,1fr)]">
        <aside className="lg:sticky lg:top-20 lg:self-start">
          <div className="rounded-lg border border-gray-200 bg-white p-2 dark:border-gray-700 dark:bg-gray-800">
            <nav className="grid grid-cols-3 gap-2 lg:grid-cols-1" aria-label="サンドボックスポリシー設定">
              {tabs.map((tab) => (
                <button
                  key={tab.id}
                  type="button"
                  onClick={() => setActiveTab(tab.id)}
                  className={`min-w-0 rounded-md px-3 py-2 text-left transition-colors ${
                    activeTab === tab.id
                      ? 'bg-blue-50 text-blue-700 dark:bg-blue-900/30 dark:text-blue-200'
                      : 'text-gray-600 hover:bg-gray-50 hover:text-gray-900 dark:text-gray-400 dark:hover:bg-gray-700 dark:hover:text-gray-100'
                  }`}
                >
                  <span className="block truncate text-sm font-medium">{tab.label}</span>
                  <span className="mt-0.5 block truncate text-xs opacity-75">{tab.description}</span>
                </button>
              ))}
            </nav>
          </div>
        </aside>

        <form
          id="sandbox-policy-settings-form"
          onSubmit={handleSubmit}
          className="min-w-0 rounded-lg border border-gray-200 bg-white dark:border-gray-700 dark:bg-gray-800"
        >
          {error && (
            <div className="mx-5 mt-5 rounded-lg border border-red-200 bg-red-50 p-3 dark:border-red-800 dark:bg-red-900/20">
              <p className="text-sm text-red-700 dark:text-red-400">{error}</p>
            </div>
          )}

          {activeTab === 'basic' && <SandboxPolicyBasicFields {...context} />}

          {activeTab === 'domains' && <SandboxPolicyDomainFields {...context} />}

          {activeTab === 'evaluation' && <SandboxPolicyEvaluationFields {...context} />}

          <div className="grid grid-cols-2 gap-3 border-t border-gray-200 px-5 py-4 dark:border-gray-700 sm:flex sm:items-center sm:justify-end">
            <button
              type="button"
              onClick={onClose}
              disabled={isSubmitting}
              className="px-4 py-2 text-sm text-gray-700 dark:text-gray-300 bg-white dark:bg-gray-700 border border-gray-300 dark:border-gray-600 rounded-lg hover:bg-gray-50 dark:hover:bg-gray-600 transition-colors disabled:opacity-50"
            >
              キャンセル
            </button>
            <button
              type="submit"
              disabled={isSubmitting}
              className="px-4 py-2 text-sm text-white bg-blue-600 hover:bg-blue-700 rounded-lg transition-colors disabled:opacity-50"
            >
              {isSubmitting ? '保存中...' : editing ? '保存' : '作成'}
            </button>
          </div>
        </form>
      </div>
    </div>
  )
}
