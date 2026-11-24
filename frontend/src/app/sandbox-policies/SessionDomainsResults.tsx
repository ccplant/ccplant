'use client'
import type { SessionDomainImportState } from './useSessionDomainImport'
export function SessionDomainsResults(context: SessionDomainImportState) {
  const {
    selectedDomains,
    showHidden,
    setShowHidden,
    activeTab,
    toggleDomain,
    handleHideDomain,
    handleUnhideDomain,
    currentDomains,
    ignoredDomainsForTab,
  } = context
  return (
    <div className="space-y-1 max-h-[55vh] overflow-y-auto">
      {currentDomains.map((domain) => (
        <div
          key={domain}
          className="flex items-center gap-2 px-2 py-1.5 rounded hover:bg-gray-50 dark:hover:bg-gray-700"
        >
          <input
            type="checkbox"
            checked={selectedDomains.has(domain)}
            onChange={() => toggleDomain(domain)}
            className="rounded border-gray-300 dark:border-gray-600 text-blue-600 shrink-0"
          />
          <span
            className={`min-w-0 flex-1 truncate font-mono text-sm ${
              activeTab === 'denied' ? 'text-red-600 dark:text-red-400' : 'text-green-600 dark:text-green-400'
            }`}
          >
            {domain}
          </span>
          <button
            onClick={() => handleHideDomain(domain)}
            title="このドメインを非表示にする"
            className="shrink-0 text-xs text-gray-400 hover:text-gray-600 dark:hover:text-gray-300 px-1.5 py-0.5 rounded border border-gray-200 dark:border-gray-600 hover:border-gray-400 dark:hover:border-gray-400 transition-colors"
          >
            非表示
          </button>
        </div>
      ))}
      {ignoredDomainsForTab.length > 0 && (
        <>
          <button
            onClick={() => setShowHidden((v) => !v)}
            className="w-full text-xs text-gray-400 hover:text-gray-600 dark:hover:text-gray-300 py-1.5 text-center transition-colors"
          >
            {showHidden ? '非表示ドメインを隠す' : `非表示のドメイン ${ignoredDomainsForTab.length} 件を表示`}
          </button>
          {showHidden &&
            ignoredDomainsForTab.map((domain) => (
              <div
                key={domain}
                className="flex items-center gap-2 px-2 py-1.5 rounded bg-gray-50 dark:bg-gray-700/50 opacity-60"
              >
                <span className="min-w-0 flex-1 truncate font-mono text-sm text-gray-400 dark:text-gray-500 line-through">
                  {domain}
                </span>
                <button
                  onClick={() => handleUnhideDomain(domain)}
                  title="このドメインを再表示する"
                  className="shrink-0 text-xs text-blue-500 hover:text-blue-700 dark:hover:text-blue-300 px-1.5 py-0.5 rounded border border-blue-200 dark:border-blue-700 hover:border-blue-400 transition-colors"
                >
                  再表示
                </button>
              </div>
            ))}
        </>
      )}
    </div>
  )
}
