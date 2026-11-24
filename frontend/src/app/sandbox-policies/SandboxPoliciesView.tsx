'use client'
import NavigationTabs from '../components/NavigationTabs'
import TagFilterSidebar from '../components/TagFilterSidebar'
import TopBar from '../components/TopBar'
import { PolicyCard } from './PolicyCard'
import { SandboxPolicySettingsPanel } from './SandboxPolicySettingsPanel'
import { SessionDomainImportPanel } from './SessionDomainImportPanel'
import type { SandboxPoliciesState } from './useSandboxPolicies'

export function SandboxPoliciesView(context: SandboxPoliciesState) {
  const {
    policies,
    loading,
    isFormOpen,
    setIsFormOpen,
    editing,
    setEditing,
    importTarget,
    setImportTarget,
    sidebarVisible,
    setSidebarVisible,
    load,
    handleDelete,
  } = context
  return (
    <main className="min-h-dvh bg-gray-50 dark:bg-gray-900">
      <TopBar
        title="Sandbox Policies"
        showSettingsButton={true}
        showFilterButton={true}
        filterButtonText="ナビゲーション"
        onFilterToggle={() => setSidebarVisible(!sidebarVisible)}
      >
        <div className="md:hidden">
          <NavigationTabs />
        </div>
      </TopBar>

      <div className="flex min-w-0">
        <TagFilterSidebar
          onFiltersChange={() => {}}
          currentFilters={{}}
          isVisible={sidebarVisible}
          onToggleVisibility={() => setSidebarVisible(!sidebarVisible)}
        />

        <div className="min-w-0 flex-1 px-4 md:px-6 lg:px-8 pt-6 md:pt-8 pb-6 md:pb-8">
          {importTarget ? (
            <SessionDomainImportPanel onClose={() => setImportTarget(null)} policy={importTarget} onImported={load} />
          ) : isFormOpen ? (
            <SandboxPolicySettingsPanel
              onClose={() => {
                setIsFormOpen(false)
                setEditing(null)
              }}
              onSuccess={() => {
                setIsFormOpen(false)
                setEditing(null)
                load()
              }}
              editing={editing}
            />
          ) : (
            <>
              <div className="mb-6 flex items-start justify-between gap-3">
                <div className="min-w-0">
                  <h1 className="text-xl font-bold text-gray-900 dark:text-white">サンドボックスポリシー</h1>
                  <p className="text-sm text-gray-500 dark:text-gray-400 mt-1">
                    名前付きのネットワークフィルタールールセットを定義し、セッションプロファイルから参照できます。
                  </p>
                </div>
                <button
                  onClick={() => {
                    setEditing(null)
                    setIsFormOpen(true)
                  }}
                  className="hidden md:inline-flex shrink-0 items-center px-4 py-2 bg-blue-600 hover:bg-blue-700 text-white text-sm font-medium rounded-lg transition-colors"
                >
                  <svg className="w-4 h-4 mr-1.5" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                    <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M12 6v6m0 0v6m0-6h6m-6 0H6" />
                  </svg>
                  新しいポリシー
                </button>
              </div>

              {loading ? (
                <div className="text-center py-12 text-gray-400">読み込み中...</div>
              ) : policies.length === 0 ? (
                <div className="text-center py-16">
                  <div className="w-12 h-12 rounded-full bg-gray-100 dark:bg-gray-800 flex items-center justify-center mx-auto mb-4">
                    <svg className="w-6 h-6 text-gray-400" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                      <path
                        strokeLinecap="round"
                        strokeLinejoin="round"
                        strokeWidth={2}
                        d="M9 12l2 2 4-4m5.618-4.016A11.955 11.955 0 0112 2.944a11.955 11.955 0 01-8.618 3.04A12.02 12.02 0 003 9c0 5.591 3.824 10.29 9 11.622 5.176-1.332 9-6.03 9-11.622 0-1.042-.133-2.052-.382-3.016z"
                      />
                    </svg>
                  </div>
                  <p className="text-gray-500 dark:text-gray-400 mb-4">ポリシーがまだありません</p>
                  <button
                    onClick={() => {
                      setEditing(null)
                      setIsFormOpen(true)
                    }}
                    className="inline-flex items-center px-4 py-2 bg-blue-600 hover:bg-blue-700 text-white text-sm font-medium rounded-lg transition-colors"
                  >
                    最初のポリシーを作成
                  </button>
                </div>
              ) : (
                <div className="grid min-w-0 gap-4 sm:grid-cols-2">
                  {policies.map((policy) => (
                    <PolicyCard
                      key={policy.id}
                      policy={policy}
                      onEdit={() => {
                        setEditing(policy)
                        setIsFormOpen(true)
                      }}
                      onDelete={() => handleDelete(policy)}
                      onImportDomains={() => setImportTarget(policy)}
                    />
                  ))}
                </div>
              )}
            </>
          )}
        </div>
      </div>

      {/* Mobile FAB */}
      {!isFormOpen && !importTarget && (
        <button
          onClick={() => {
            setEditing(null)
            setIsFormOpen(true)
          }}
          className="md:hidden fixed bottom-6 right-6 w-14 h-14 bg-blue-600 hover:bg-blue-700 text-white rounded-full shadow-lg flex items-center justify-center z-50 transition-colors"
          aria-label="新しいポリシー"
        >
          <svg className="w-6 h-6" fill="none" stroke="currentColor" viewBox="0 0 24 24">
            <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M12 6v6m0 0v6m0-6h6m-6 0H6" />
          </svg>
        </button>
      )}
    </main>
  )
}
