'use client'
import type { SessionDomainImportState } from './useSessionDomainImport'
export function SessionDomainsHeader(context: SessionDomainImportState) {
  const { onClose, policy, domains, loading, fetchDomains } = context
  return (
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
        <h1 className="text-xl font-bold text-gray-900 dark:text-white">検出ドメインを確認</h1>
        <p className="mt-1 break-words text-sm text-gray-500 dark:text-gray-400">
          ポリシー: {policy.name}
          {!loading && domains?.updated_at && ` / 最終更新: ${new Date(domains.updated_at).toLocaleString('ja-JP')}`}
        </p>
      </div>
      <button
        onClick={fetchDomains}
        disabled={loading}
        className="inline-flex w-full items-center justify-center rounded-lg border border-blue-200 bg-white px-4 py-2 text-sm font-medium text-blue-600 transition-colors hover:bg-blue-50 disabled:opacity-50 dark:border-blue-800 dark:bg-gray-800 dark:text-blue-400 dark:hover:bg-blue-900/20 sm:w-auto"
      >
        再取得
      </button>
    </div>
  )
}
