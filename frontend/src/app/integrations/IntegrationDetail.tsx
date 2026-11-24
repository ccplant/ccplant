'use client'

import { CheckCircle, ExternalLink, Plug, Unlink } from 'lucide-react'
import { ScopeGroupCard } from './IntegrationComponents'
import { denyScopeValue, defaultScopeForGroup, scopeGroups } from './integrationScopes'
import { IntegrationsPageModel } from './useIntegrationsPage'

export function IntegrationDetail({ model }: { model: IntegrationsPageModel }) {
  const {
    connectingIntegrationID, revokeIntegration, revokingIntegrationID,
    selectedIntegration, selectedScopeGroups, setScopeGroup, setScopeGroupAllowed, startOAuth,
  } = model
  if (!selectedIntegration) return null
  return (
    <section className="space-y-6">
                    <div className="rounded-lg border border-gray-200 bg-white p-5 shadow-sm dark:border-gray-700 dark:bg-gray-800">
                      <div className="flex flex-col gap-4 sm:flex-row sm:items-start sm:justify-between">
                        <div className="min-w-0">
                          <div className="flex items-center gap-3">
                            {selectedIntegration.icon_url ? (
                              // eslint-disable-next-line @next/next/no-img-element
                              <img src={selectedIntegration.icon_url} alt="" className="h-8 w-8 shrink-0" />
                            ) : (
                              <span className="inline-flex h-8 w-8 shrink-0 items-center justify-center rounded-md bg-gray-100 text-gray-500 dark:bg-gray-700 dark:text-gray-300">
                                <Plug className="h-4 w-4" />
                              </span>
                            )}
                            <div className="min-w-0">
                              <h2 className="truncate text-xl font-semibold text-gray-900 dark:text-white">
                                {selectedIntegration.name}
                              </h2>
                              {selectedIntegration.connected && (
                                <span className="mt-1 inline-flex items-center gap-1 rounded-full bg-green-50 px-2 py-0.5 text-xs font-medium text-green-700 dark:bg-green-900/30 dark:text-green-300">
                                  <CheckCircle className="h-3.5 w-3.5" />
                                  Connected
                                </span>
                              )}
                            </div>
                          </div>
                          {selectedIntegration.description && (
                            <p className="mt-3 max-w-3xl text-sm text-gray-600 dark:text-gray-300">
                              {selectedIntegration.description}
                            </p>
                          )}
                        </div>
    
                        <div className="flex shrink-0 gap-2">
                          <button
                            type="button"
                            onClick={() => startOAuth(selectedIntegration)}
                            disabled={connectingIntegrationID === selectedIntegration.id || revokingIntegrationID === selectedIntegration.id}
                            className="inline-flex min-w-0 items-center justify-center gap-1.5 rounded-md bg-blue-600 px-3 py-2 text-sm font-medium text-white transition-colors hover:bg-blue-700 disabled:cursor-not-allowed disabled:opacity-60 focus:outline-none focus:ring-2 focus:ring-blue-500 focus:ring-offset-2 dark:focus:ring-offset-gray-800"
                          >
                            <ExternalLink className="h-4 w-4 shrink-0" />
                            <span className="truncate">
                              {connectingIntegrationID === selectedIntegration.id ? 'Connecting...' : selectedIntegration.connected ? 'Reconnect' : `Connect ${selectedIntegration.name}`}
                            </span>
                          </button>
                          {selectedIntegration.connected && (
                            <button
                              type="button"
                              onClick={() => revokeIntegration(selectedIntegration)}
                              disabled={revokingIntegrationID === selectedIntegration.id || connectingIntegrationID === selectedIntegration.id}
                              aria-label={`Revoke ${selectedIntegration.name}`}
                              title={`Revoke ${selectedIntegration.name}`}
                              className="inline-flex shrink-0 items-center justify-center gap-1.5 rounded-md border border-red-200 bg-white px-3 py-2 text-sm font-medium text-red-600 transition-colors hover:bg-red-50 disabled:cursor-not-allowed disabled:opacity-60 focus:outline-none focus:ring-2 focus:ring-red-500 focus:ring-offset-2 dark:border-red-900/70 dark:bg-gray-800 dark:text-red-300 dark:hover:bg-red-900/20 dark:focus:ring-offset-gray-800"
                            >
                              <Unlink className={`h-4 w-4 ${revokingIntegrationID === selectedIntegration.id ? 'animate-pulse' : ''}`} />
                              <span>{revokingIntegrationID === selectedIntegration.id ? 'Revoking...' : 'Revoke'}</span>
                            </button>
                          )}
                        </div>
                      </div>
                    </div>
    
                    <div>
                      <h3 className="text-base font-semibold text-gray-900 dark:text-white">
                        アクセスを許可するスコープ
                      </h3>
                      <div className="mt-4 grid gap-4 lg:grid-cols-2">
                        {scopeGroups(selectedIntegration).map((group) => {
                          const selectedScopeID = selectedScopeGroups[selectedIntegration.id]?.[group.id] ?? defaultScopeForGroup(group)
                          const allowed = selectedScopeID !== denyScopeValue
                          return (
                            <ScopeGroupCard
                              key={group.id}
                              integration={selectedIntegration}
                              group={group}
                              selectedScopeID={selectedScopeID}
                              allowed={allowed}
                              onAllowedChange={(nextAllowed) => setScopeGroupAllowed(selectedIntegration, group, nextAllowed)}
                              onScopeChange={(scopeID) => setScopeGroup(selectedIntegration, group.id, scopeID)}
                            />
                          )
                        })}
                      </div>
                    </div>
                  </section>
  )
}
