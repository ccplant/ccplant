'use client'

import { useCallback, useEffect, useMemo, useState } from 'react'
import { Activity, Boxes, CircleGauge, RefreshCw, Server, Users } from 'lucide-react'
import { ESMRegistrationToken, SettingsPageHeader, SettingsSubsection } from '@/components/settings'
import { ExternalSessionManagerList } from '@/components/settings/ExternalSessionManagerList'
import { createCurrentDeploymentAgentAPIProxyClient } from '@/lib/agentapi-proxy-client'
import type { ExternalSessionManagerConfig } from '@/types/settings'
import type { LogicalSessionPool, SessionPoolBinding, SessionPoolSupplier } from '@/types/session_pool'
import { useSettingsScope } from '../SettingsScopeContext'

interface PoolRuntime {
  pool: LogicalSessionPool
  suppliers: SessionPoolSupplier[]
  bindings: SessionPoolBinding[]
}

const heartbeatOnline = (manager: ExternalSessionManagerConfig) =>
  Boolean(manager.last_heartbeat_at && Date.now() - new Date(manager.last_heartbeat_at).getTime() < 45_000)

const card = 'rounded-xl border border-gray-200 bg-white p-4 shadow-sm dark:border-gray-800 dark:bg-gray-900'

export function PoolsSection({ showHeader = true }: { showHeader?: boolean }) {
  const client = useMemo(() => createCurrentDeploymentAgentAPIProxyClient(), [])
  const {
    scopeKind, scopeId, settings, update, revealedTokens, regenerateEsmToken, regeneratingEsmId,
  } = useSettingsScope()
  const managers = settings.external_session_managers ?? []
  const [runtimes, setRuntimes] = useState<PoolRuntime[]>([])
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)
  const [showRegistration, setShowRegistration] = useState(false)

  const reload = useCallback(async () => {
    setLoading(true)
    setError(null)
    try {
      const pools = await client.listManagedSessionPools()
      const details = await Promise.all(pools.map(async (pool) => {
        const [suppliers, bindings] = await Promise.all([
          client.listManagedSessionPoolSuppliers(pool.name),
          client.listManagedSessionPoolBindings(pool.name),
        ])
        return { pool, suppliers, bindings }
      }))
      setRuntimes(details)
    } catch (reason) {
      setError(reason instanceof Error ? reason.message : 'Pool情報の読み込みに失敗しました')
    } finally {
      setLoading(false)
    }
  }, [client])

  useEffect(() => { void reload() }, [reload])

  const updateManager = (managerID: string, changed: ExternalSessionManagerConfig[]) => {
    const replacement = changed[0]
    update({
      external_session_managers: replacement
        ? managers.map((manager) => manager.id === managerID ? replacement : manager)
        : managers.filter((manager) => manager.id !== managerID),
    })
  }

  const patchSupplier = async (supplier: SessionPoolSupplier, patch: { enabled?: boolean; draining?: boolean }) => {
    setError(null)
    try {
      await client.patchManagedSessionPoolSupplier(supplier.pool, supplier.manager_id, patch)
      await reload()
    } catch (reason) {
      setError(reason instanceof Error ? reason.message : 'Supplierの更新に失敗しました')
    }
  }

  const togglePool = async (pool: LogicalSessionPool) => {
    setError(null)
    try {
      await client.patchManagedSessionPool(pool.name, { enabled: !pool.enabled })
      await reload()
    } catch (reason) {
      setError(reason instanceof Error ? reason.message : 'Poolの更新に失敗しました')
    }
  }

  const managerByID = new Map(managers.filter((manager) => manager.id).map((manager) => [manager.id!, manager]))
  const assigned = new Set(runtimes.flatMap((runtime) => runtime.suppliers.map((supplier) => supplier.manager_id)))
  const unassigned = managers.filter((manager) => !manager.id || !assigned.has(manager.id))
  const onlineManagers = managers.filter(heartbeatOnline).length
  const activeSessions = managers.reduce((sum, manager) => sum + (manager.active_sessions ?? 0), 0)
  const totalCapacity = runtimes.flatMap((runtime) => runtime.suppliers).reduce((sum, supplier) => sum + (supplier.max_runners ?? 0), 0)

  const metrics = [
    { label: 'Pools', value: runtimes.length, icon: Boxes },
    { label: 'Managers online', value: `${onlineManagers}/${managers.length}`, icon: Activity },
    { label: 'Active sessions', value: activeSessions, icon: CircleGauge },
    { label: 'Runner capacity', value: totalCapacity || '∞', icon: Server },
  ]

  return (
    <>
      {showHeader && (
        <SettingsPageHeader
          title="プール"
          description="セッションの実行先、供給元、利用権限、稼働状態をまとめて管理します。"
        />
      )}

      <div className="mb-6 grid gap-3 sm:grid-cols-2 xl:grid-cols-4">
        {metrics.map(({ label, value, icon: Icon }) => (
          <div key={label} className={card}>
            <div className="flex items-center justify-between text-gray-500 dark:text-gray-400">
              <span className="text-xs font-medium uppercase tracking-wide">{label}</span>
              <Icon className="h-4 w-4" />
            </div>
            <p className="mt-2 text-2xl font-semibold tabular-nums text-gray-950 dark:text-white">{value}</p>
          </div>
        ))}
      </div>

      <div className="mb-5 flex flex-wrap items-center justify-between gap-3">
        <p className="text-sm text-gray-500 dark:text-gray-400">
          Poolを開くとManagerのログ、再起動、upgradeまで操作できます。
        </p>
        <div className="flex gap-2">
          <button type="button" onClick={() => void reload()} disabled={loading} className="inline-flex items-center gap-1.5 rounded-md border border-gray-300 px-3 py-2 text-sm text-gray-700 hover:bg-gray-50 disabled:opacity-50 dark:border-gray-700 dark:text-gray-200 dark:hover:bg-gray-800">
            <RefreshCw className={`h-4 w-4 ${loading ? 'animate-spin' : ''}`} /> 更新
          </button>
          <button type="button" onClick={() => setShowRegistration((value) => !value)} className="rounded-md bg-blue-600 px-3 py-2 text-sm font-medium text-white hover:bg-blue-700">
            {showRegistration ? '登録を閉じる' : 'Managerを追加'}
          </button>
        </div>
      </div>

      {error && <div role="alert" className="mb-5 rounded-lg border border-red-200 bg-red-50 p-3 text-sm text-red-700 dark:border-red-900 dark:bg-red-950/40 dark:text-red-300">{error}</div>}

      {showRegistration && (
        <SettingsSubsection title="ManagerをPoolへ追加" description="登録完了時にPool、Supplier、Bindingが自動作成されます">
          <ESMRegistrationToken scope={scopeKind === 'personal' ? 'user' : 'team'} teamId={scopeKind === 'team' ? scopeId : undefined} />
        </SettingsSubsection>
      )}

      <div className="space-y-5">
        {!loading && runtimes.length === 0 && (
          <div className={`${card} py-10 text-center`}>
            <Boxes className="mx-auto h-8 w-8 text-gray-400" />
            <p className="mt-3 text-sm font-medium text-gray-900 dark:text-white">利用可能なPoolはありません</p>
            <p className="mt-1 text-xs text-gray-500">Managerを追加するとPoolが自動作成されます。</p>
          </div>
        )}

        {runtimes.map(({ pool, suppliers, bindings }) => {
          const poolManagers = suppliers.map((supplier) => managerByID.get(supplier.manager_id)).filter((manager): manager is ExternalSessionManagerConfig => Boolean(manager))
          const idle = suppliers.reduce((sum, supplier) => sum + (supplier.idle_runners ?? 0), 0)
          const runners = suppliers.reduce((sum, supplier) => sum + (supplier.total_runners ?? 0), 0)
          return (
            <section key={pool.name} className="overflow-hidden rounded-xl border border-gray-200 bg-white shadow-sm dark:border-gray-800 dark:bg-gray-900">
              <div className="border-b border-gray-200 p-5 dark:border-gray-800">
                <div className="flex flex-wrap items-start justify-between gap-3">
                  <div>
                    <div className="flex items-center gap-2">
                      <h2 className="text-lg font-semibold text-gray-950 dark:text-white">{pool.name}</h2>
                      <span className={`rounded-full px-2 py-0.5 text-xs font-medium ${pool.enabled ? 'bg-emerald-100 text-emerald-700 dark:bg-emerald-950 dark:text-emerald-300' : 'bg-gray-100 text-gray-600 dark:bg-gray-800 dark:text-gray-300'}`}>{pool.enabled ? 'Enabled' : 'Disabled'}</span>
                    </div>
                    <div className="mt-2 flex flex-wrap gap-x-4 gap-y-1 text-xs text-gray-500 dark:text-gray-400">
                      <span>{suppliers.length} suppliers</span><span>{runners} runners</span><span>{idle} idle</span><span>{bindings.length} bindings</span>
                    </div>
                  </div>
                  <div className="flex flex-wrap items-center gap-1.5">
                    {bindings.map((binding) => (
                      <span key={binding.id} className="inline-flex items-center gap-1 rounded-md bg-violet-50 px-2 py-1 text-xs text-violet-700 dark:bg-violet-950/50 dark:text-violet-300">
                        <Users className="h-3 w-3" /> {binding.subject_type}: {binding.subject_id || 'everyone'} · {binding.role}
                      </span>
                    ))}
                    <button
                      type="button"
                      onClick={() => void togglePool(pool)}
                      className="ml-1 rounded-md border border-gray-300 px-2.5 py-1 text-xs font-medium text-gray-700 hover:bg-gray-50 dark:border-gray-700 dark:text-gray-200 dark:hover:bg-gray-800"
                    >
                      {pool.enabled ? 'Poolを停止' : 'Poolを有効化'}
                    </button>
                  </div>
                </div>
              </div>

              <div className="p-5">
                {suppliers.map((supplier) => (
                  <div key={supplier.manager_id} className="mb-3 flex flex-wrap items-center justify-between gap-3 rounded-lg border border-gray-200 bg-gray-50 p-3 dark:border-gray-800 dark:bg-gray-950">
                    <div className="text-xs text-gray-600 dark:text-gray-300">
                      <span className="font-semibold">{managerByID.get(supplier.manager_id)?.name ?? supplier.manager_id}</span>
                      <span className="ml-3">idle {supplier.idle_runners ?? 0} / total {supplier.total_runners ?? 0} / max {supplier.max_runners || '∞'}</span>
                    </div>
                    <div className="flex gap-2">
                      <button type="button" onClick={() => void patchSupplier(supplier, { draining: !supplier.draining })} className="text-xs text-amber-700 hover:underline dark:text-amber-300">{supplier.draining ? 'Drain解除' : 'Drain'}</button>
                      <button type="button" onClick={() => void patchSupplier(supplier, { enabled: !supplier.enabled })} className="text-xs text-blue-700 hover:underline dark:text-blue-300">{supplier.enabled ? '供給停止' : '供給再開'}</button>
                    </div>
                  </div>
                ))}

                {poolManagers.length === 0 ? <p className="py-4 text-sm text-gray-500">このPoolに紐づく登録済みManagerはありません。</p> : poolManagers.map((manager) => (
                  <ExternalSessionManagerList key={manager.id} managers={[manager]} onChange={(changed) => updateManager(manager.id!, changed)} revealedTokens={revealedTokens} onRegenerate={regenerateEsmToken} regeneratingEsmId={regeneratingEsmId} scope={scopeKind === 'personal' ? 'user' : 'team'} teamId={scopeKind === 'team' ? scopeId : undefined} />
                ))}
              </div>
            </section>
          )
        })}

        {unassigned.length > 0 && (
          <SettingsSubsection title="未割り当てのManager" description="Supplier情報がない、または参照できないManager">
            <ExternalSessionManagerList managers={unassigned} onChange={(changed) => update({ external_session_managers: [...managers.filter((manager) => manager.id && assigned.has(manager.id)), ...changed] })} revealedTokens={revealedTokens} onRegenerate={regenerateEsmToken} regeneratingEsmId={regeneratingEsmId} scope={scopeKind === 'personal' ? 'user' : 'team'} teamId={scopeKind === 'team' ? scopeId : undefined} />
          </SettingsSubsection>
        )}
      </div>
    </>
  )
}
