'use client'
import type { SessionDomainImportState } from './useSessionDomainImport'
export function SessionDomainsTabs(context: SessionDomainImportState) {
  const { setShowHidden, activeTab, setActiveTab, tabs } = context
  return (
    <nav className="grid grid-cols-2 gap-2 lg:grid-cols-1" aria-label="検出ドメイン">
      {tabs.map((tab) => (
        <button
          key={tab.id}
          type="button"
          onClick={() => {
            setActiveTab(tab.id)
            setShowHidden(false)
          }}
          className={`min-w-0 rounded-md px-3 py-2 text-left transition-colors ${
            activeTab === tab.id
              ? tab.tone === 'red'
                ? 'bg-red-50 text-red-700 dark:bg-red-900/30 dark:text-red-200'
                : 'bg-green-50 text-green-700 dark:bg-green-900/30 dark:text-green-200'
              : 'text-gray-600 hover:bg-gray-50 hover:text-gray-900 dark:text-gray-400 dark:hover:bg-gray-700 dark:hover:text-gray-100'
          }`}
        >
          <span className="block truncate text-sm font-medium">{tab.label}</span>
          <span className="mt-0.5 block text-xs opacity-75">{tab.count} 件</span>
        </button>
      ))}
    </nav>
  )
}
