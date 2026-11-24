'use client'

import TopBar from '../components/TopBar'
import NavigationTabs from '../components/NavigationTabs'
import { IntegrationsSidebar } from './IntegrationComponents'
import { IntegrationDetail } from './IntegrationDetail'
import { IntegrationsPageModel } from './useIntegrationsPage'

export function IntegrationsPageView({ model }: { model: IntegrationsPageModel }) {
  const {
    error, integrations, isTeamScope, isTeamScopeLoading, loadIntegrations, loading,
    selectIntegration, selectedIntegration, selectedIntegrationID, setSidebarVisible,
    sidebarVisible, visibleIntegrations,
  } = model

  return (
    <main className="min-h-dvh bg-gray-50 dark:bg-gray-900">
      <TopBar
        title="Integrations"
        showFilterButton={!isTeamScope}
        filterButtonText="連携先"
        onFilterToggle={() => setSidebarVisible(!sidebarVisible)}
        showSettingsButton={true}
      >
        <div className="md:hidden">
          <NavigationTabs />
        </div>
      </TopBar>

      <div className="flex">
        {!isTeamScope && (
          <IntegrationsSidebar
            integrations={visibleIntegrations}
            selectedIntegrationID={selectedIntegrationID}
            isVisible={sidebarVisible}
            loading={loading}
            onSelectIntegration={selectIntegration}
            onRefresh={loadIntegrations}
            onToggleVisibility={() => setSidebarVisible(!sidebarVisible)}
          />
        )}

        <div className="flex-1 px-4 md:px-6 lg:px-8 pt-6 md:pt-8 pb-6 md:pb-8">
          <div className="mx-auto max-w-5xl">
            {isTeamScopeLoading || loading ? (
              <div className="flex items-center justify-center py-12">
                <div className="h-8 w-8 animate-spin rounded-full border-b-2 border-blue-600"></div>
              </div>
            ) : isTeamScope ? (
              <div className="rounded-lg border border-gray-200 bg-white p-6 text-sm text-gray-600 dark:border-gray-700 dark:bg-gray-800 dark:text-gray-300">
                チームでは利用できません。
              </div>
            ) : error ? (
              <div className="rounded-lg border border-red-200 bg-red-50 p-4 text-sm text-red-700 dark:border-red-800 dark:bg-red-900/20 dark:text-red-300">
                {error}
              </div>
            ) : !integrations?.enabled || visibleIntegrations.length === 0 ? (
              <div className="rounded-lg border border-gray-200 bg-white p-6 text-sm text-gray-600 dark:border-gray-700 dark:bg-gray-800 dark:text-gray-300">
                No SaaS integrations are available.
              </div>
            ) : selectedIntegration ? (
              <IntegrationDetail model={model} />
            ) : null}
          </div>
        </div>
      </div>
    </main>
  )
}
