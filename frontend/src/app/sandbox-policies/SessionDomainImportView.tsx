'use client'
import { SessionDomainsHeader } from './SessionDomainsHeader'
import { SessionDomainsResults } from './SessionDomainsResults'
import { SessionDomainsTabs } from './SessionDomainsTabs'
import type { SessionDomainImportState } from './useSessionDomainImport'
export function SessionDomainImportView(context: SessionDomainImportState) {
  const {
    onClose,
    policy,
    domains,
    loading,
    selectedDomains,
    activeTab,
    isSaving,
    error,
    success,
    handleAddToPolicy,
    currentDomains,
    ignoredDomainsForTab,
  } = context
  return (
    <div className="space-y-5">
      <SessionDomainsHeader {...context} />

      <div className="grid gap-5 lg:grid-cols-[220px_minmax(0,1fr)]">
        <aside className="lg:sticky lg:top-20 lg:self-start">
          <div className="rounded-lg border border-gray-200 bg-white p-2 dark:border-gray-700 dark:bg-gray-800">
            <SessionDomainsTabs {...context} />
          </div>
        </aside>

        <section className="min-w-0 rounded-lg border border-gray-200 bg-white dark:border-gray-700 dark:bg-gray-800">
          <div className="space-y-4 p-5 md:p-6">
            {error && (
              <div className="p-3 bg-red-50 dark:bg-red-900/20 border border-red-200 dark:border-red-800 rounded-lg">
                <p className="text-sm text-red-700 dark:text-red-400">{error}</p>
              </div>
            )}
            {success && (
              <div className="p-3 bg-green-50 dark:bg-green-900/20 border border-green-200 dark:border-green-800 rounded-lg">
                <p className="text-sm text-green-700 dark:text-green-400">{success}</p>
              </div>
            )}

            {loading && (
              <div className="flex items-center justify-center py-12 gap-2 text-gray-400">
                <svg className="animate-spin w-5 h-5" fill="none" viewBox="0 0 24 24">
                  <circle className="opacity-25" cx="12" cy="12" r="10" stroke="currentColor" strokeWidth="4" />
                  <path className="opacity-75" fill="currentColor" d="M4 12a8 8 0 018-8v8H4z" />
                </svg>
                <span className="text-sm">セッションのアクセスログを収集中...</span>
              </div>
            )}

            {!loading && domains && domains.allowed.length === 0 && domains.denied.length === 0 && (
              <p className="text-sm text-gray-400 text-center py-10">
                まだドメインが収集されていません。このポリシーを使用したセッションが起動されると自動的に収集されます。
              </p>
            )}

            {!loading && domains && (domains.allowed.length > 0 || domains.denied.length > 0) && (
              <>
                <div>
                  <h2 className="text-base font-semibold text-gray-900 dark:text-white">
                    {activeTab === 'denied' ? 'ブロック済みドメイン' : '許可済みドメイン'}
                  </h2>
                  <p className="mt-1 text-sm text-gray-500 dark:text-gray-400">
                    まだポリシーに反映していない検出ドメインを確認できます。
                  </p>
                </div>

                {currentDomains.length === 0 && ignoredDomainsForTab.length === 0 ? (
                  <p className="text-sm text-gray-400 text-center py-8">
                    {activeTab === 'denied' ? 'ブロックされたドメインはありません' : '許可されたドメインはありません'}
                  </p>
                ) : (
                  <SessionDomainsResults {...context} />
                )}

                {selectedDomains.size > 0 && (
                  <div className="border-t border-gray-200 pt-4 dark:border-gray-700">
                    <button
                      onClick={handleAddToPolicy}
                      disabled={isSaving}
                      className="w-full px-4 py-2 text-sm bg-blue-600 hover:bg-blue-700 disabled:bg-gray-300 dark:disabled:bg-gray-600 text-white rounded-lg transition-colors disabled:cursor-not-allowed"
                    >
                      {isSaving ? '追加中...' : `選択した ${selectedDomains.size} 件をポリシーに追加`}
                    </button>
                    <p className="text-xs text-gray-400 mt-1 text-center">
                      {(policy.allowed_domains?.length ?? 0) > 0
                        ? '許可リストに追加されます'
                        : '拒否リストから除外されます'}
                    </p>
                  </div>
                )}
              </>
            )}
          </div>

          <div className="flex items-center justify-end px-5 py-4 border-t border-gray-200 dark:border-gray-700">
            <button
              onClick={onClose}
              className="w-full px-4 py-2 text-sm text-gray-700 dark:text-gray-300 bg-white dark:bg-gray-700 border border-gray-300 dark:border-gray-600 rounded-lg hover:bg-gray-50 dark:hover:bg-gray-600 transition-colors sm:w-auto"
            >
              一覧へ戻る
            </button>
          </div>
        </section>
      </div>
    </div>
  )
}
