'use client'

import { CheckCircle, Plug, RefreshCw } from 'lucide-react'
import NavigationTabs from '../components/NavigationTabs'
import { IntegrationScope, SciaIntegration } from '@/types/settings'
import { ScopeGroup } from './integrationScopes'

export function IntegrationsSidebar({
  integrations,
  selectedIntegrationID,
  isVisible,
  loading,
  onSelectIntegration,
  onRefresh,
  onToggleVisibility,
}: {
  integrations: SciaIntegration[]
  selectedIntegrationID: string | null
  isVisible: boolean
  loading: boolean
  onSelectIntegration: (integrationID: string) => void
  onRefresh: () => void
  onToggleVisibility: () => void
}) {
  return (
    <aside
      className={`
        fixed inset-y-0 left-0 z-30 flex w-80 flex-col border-r border-gray-200 bg-white transition-transform duration-300 ease-in-out dark:border-gray-700 dark:bg-gray-800
        md:relative md:inset-auto md:z-0 md:h-screen md:translate-x-0
        ${isVisible ? 'translate-x-0' : '-translate-x-full'}
      `}
    >
      <div className="flex-shrink-0 px-4 pt-4 pb-0">
        <NavigationTabs />
      </div>

      <div className="flex items-center justify-between gap-3 px-4 pb-3 pt-4">
        <div>
          <h2 className="text-lg font-semibold text-gray-900 dark:text-white">
            連携先一覧
          </h2>
          <p className="text-xs text-gray-500 dark:text-gray-400">
            連携先を選んでスコープを設定
          </p>
        </div>
        <div className="flex items-center gap-1">
          <button
            type="button"
            onClick={onRefresh}
            disabled={loading}
            aria-label="Refresh integrations"
            title="Refresh integrations"
            className="inline-flex h-9 w-9 items-center justify-center rounded-md border border-gray-200 bg-white text-gray-600 transition-colors hover:bg-gray-50 disabled:cursor-not-allowed disabled:opacity-60 dark:border-gray-700 dark:bg-gray-800 dark:text-gray-300 dark:hover:bg-gray-700"
          >
            <RefreshCw className={`h-4 w-4 ${loading ? 'animate-spin' : ''}`} />
          </button>
          <button
            type="button"
            onClick={onToggleVisibility}
            className="inline-flex h-9 w-9 items-center justify-center rounded-md text-gray-400 transition-colors hover:bg-gray-100 hover:text-gray-600 md:hidden dark:hover:bg-gray-700 dark:hover:text-gray-300"
            aria-label="Close integrations sidebar"
          >
            ×
          </button>
        </div>
      </div>

      <div className="flex-1 overflow-y-auto px-4 pb-4">
        <div className="space-y-2">
          {integrations.map((integration) => {
            const active = integration.id === selectedIntegrationID
            return (
              <button
                key={integration.id}
                type="button"
                onClick={() => onSelectIntegration(integration.id)}
                className={`w-full rounded-md border px-3 py-3 text-left transition-colors ${
                  active
                    ? 'border-blue-200 bg-blue-50 text-blue-800 dark:border-blue-800 dark:bg-blue-900/30 dark:text-blue-200'
                    : 'border-transparent text-gray-700 hover:border-gray-200 hover:bg-gray-50 dark:text-gray-300 dark:hover:border-gray-700 dark:hover:bg-gray-700/60'
                }`}
              >
                <div className="flex items-start gap-3">
                  {integration.icon_url ? (
                    // eslint-disable-next-line @next/next/no-img-element
                    <img src={integration.icon_url} alt="" className="mt-0.5 h-6 w-6 shrink-0" />
                  ) : (
                    <span className="mt-0.5 inline-flex h-6 w-6 shrink-0 items-center justify-center rounded bg-gray-100 text-gray-500 dark:bg-gray-700 dark:text-gray-300">
                      <Plug className="h-3.5 w-3.5" />
                    </span>
                  )}
                  <span className="min-w-0 flex-1">
                    <span className="flex items-center gap-2">
                      <span className="truncate text-sm font-medium">
                        {integration.name}
                      </span>
                      {integration.connected && (
                        <CheckCircle className="h-3.5 w-3.5 shrink-0 text-green-600 dark:text-green-400" />
                      )}
                    </span>
                    {integration.description && (
                      <span className={`mt-1 line-clamp-2 block text-xs ${active ? 'text-blue-700 dark:text-blue-300' : 'text-gray-500 dark:text-gray-400'}`}>
                        {integration.description}
                      </span>
                    )}
                  </span>
                </div>
              </button>
            )
          })}
        </div>
      </div>
    </aside>
  )
}

export function ScopeGroupCard({
  integration,
  group,
  selectedScopeID,
  allowed,
  onAllowedChange,
  onScopeChange,
}: {
  integration: SciaIntegration
  group: ScopeGroup
  selectedScopeID: string
  allowed: boolean
  onAllowedChange: (allowed: boolean) => void
  onScopeChange: (scopeID: string) => void
}) {
  return (
    <fieldset
      className={`rounded-lg border p-4 transition-colors ${
        allowed
          ? 'border-blue-200 bg-blue-50/50 dark:border-blue-900/70 dark:bg-blue-900/10'
          : 'border-gray-200 bg-white dark:border-gray-700 dark:bg-gray-800'
      }`}
    >
      <div className="flex items-start justify-between gap-4">
        <div className="min-w-0">
          <h4 className="text-sm font-semibold text-gray-900 dark:text-white">
            {group.name}
          </h4>
          {group.desc && (
            <p className="mt-1 text-sm text-gray-500 dark:text-gray-400">
              {group.desc}
            </p>
          )}
        </div>
        <button
          type="button"
          role="switch"
          aria-checked={allowed}
          onClick={() => onAllowedChange(!allowed)}
          className={`relative mt-0.5 inline-flex h-6 w-11 shrink-0 items-center rounded-full transition-colors focus:outline-none focus:ring-2 focus:ring-blue-500 focus:ring-offset-2 dark:focus:ring-offset-gray-800 ${
            allowed ? 'bg-blue-600' : 'bg-gray-300 dark:bg-gray-600'
          }`}
        >
          <span
            className={`inline-block h-5 w-5 rounded-full bg-white shadow transition-transform ${
              allowed ? 'translate-x-5' : 'translate-x-0.5'
            }`}
          />
        </button>
      </div>

      {allowed && (
        <div className="mt-4 border-t border-blue-100 pt-4 dark:border-blue-900/50">
          <div className="mb-2 text-xs font-medium text-gray-500 dark:text-gray-400">
            詳しいアクセス
          </div>
          <div className="space-y-2">
            {group.scopes.map((scope) => (
              <ScopeRadio
                key={scope.id}
                integrationID={integration.id}
                groupID={group.id}
                scope={scope}
                checked={selectedScopeID === scope.id}
                onChange={() => onScopeChange(scope.id)}
              />
            ))}
          </div>
        </div>
      )}
    </fieldset>
  )
}

function ScopeRadio({
  integrationID,
  groupID,
  scope,
  checked,
  onChange,
}: {
  integrationID: string
  groupID: string
  scope: IntegrationScope
  checked: boolean
  onChange: () => void
}) {
  return (
    <label
      className={`flex cursor-pointer items-start gap-2.5 rounded-md px-2 py-1.5 text-sm transition-colors ${
        checked
          ? 'bg-white text-gray-900 shadow-sm ring-1 ring-blue-100 dark:bg-gray-800 dark:text-white dark:ring-blue-900/60'
          : 'text-gray-600 hover:bg-white/70 dark:text-gray-300 dark:hover:bg-gray-800/70'
      }`}
    >
      <input
        type="radio"
        name={`${integrationID}-${groupID}`}
        value={scope.id}
        checked={checked}
        onChange={onChange}
        className="mt-0.5 h-3.5 w-3.5 border-gray-300 text-blue-600 focus:ring-blue-500"
      />
      <span className="min-w-0">
        <span className="block font-medium">
          {scope.name}
        </span>
        {scope.desc && (
          <span className="mt-0.5 block text-xs text-gray-500 dark:text-gray-400">
            {scope.desc}
          </span>
        )}
      </span>
    </label>
  )
}
