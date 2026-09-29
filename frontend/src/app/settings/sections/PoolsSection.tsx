'use client'

import { useCallback, useEffect, useMemo, useState } from 'react'
import { Boxes, Plus, RefreshCw, Trash2, UserPlus, Users, X } from 'lucide-react'
import { ESMRegistrationToken, SettingsPageHeader, SettingsSubsection } from '@/components/settings'
import { ExternalSessionManagerList } from '@/components/settings/ExternalSessionManagerList'
import { PoolSupplierControls } from '@/components/settings/PoolSupplierControls'
import { BindingSettingsEditor, PoolSettingsEditor, SupplierSettingsEditor } from '@/components/settings/PoolResourceEditors'
import { createCurrentDeploymentAgentAPIProxyClient } from '@/lib/agentapi-proxy-client'
import type { ExternalSessionManagerConfig } from '@/types/settings'
import type { LogicalSessionPool, SessionPoolBinding, SessionPoolSupplier } from '@/types/session_pool'
import { useSettingsScope } from '../SettingsScopeContext'

interface PoolRuntime {
  pool: LogicalSessionPool
  suppliers: SessionPoolSupplier[]
  bindings: SessionPoolBinding[]
  scopeBinding: SessionPoolBinding
}

type PoolTab = 'managers' | 'pools' | 'assignments' | 'status'

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
  const [showCreatePool, setShowCreatePool] = useState(false)
  const [newPoolName, setNewPoolName] = useState('')
  const [assigningPool, setAssigningPool] = useState<string | null>(null)
  const [assignManagerID, setAssignManagerID] = useState('')
  const [busyAction, setBusyAction] = useState<string | null>(null)
  const [activeTab, setActiveTab] = useState<PoolTab>('pools')

  const reload = useCallback(async () => {
    setLoading(true)
    setError(null)
    try {
      const pools = await client.listManagedSessionPools()
      const subjectType = scopeKind === 'personal' ? 'user' : 'team'
      const details = await Promise.all(pools.map(async (pool) => {
        const bindings = await client.listManagedSessionPoolBindings(pool.name)
        const exact = bindings.find((binding) => binding.subject_type === subjectType && binding.subject_id === scopeId)
        const scopeBinding = exact ?? bindings.find((binding) => binding.subject_type === 'all' && !binding.subject_id)
        if (!scopeBinding) return null
        const suppliers = (scopeBinding.role === 'manage' || scopeBinding.role === 'manage_and_use') && scopeBinding.enabled
          ? await client.listManagedSessionPoolSuppliers(pool.name)
          : []
        return { pool, suppliers, bindings, scopeBinding }
      }))
      setRuntimes(details.filter((runtime): runtime is PoolRuntime => runtime !== null))
    } catch (reason) {
      setError(reason instanceof Error ? reason.message : 'Pool情報の読み込みに失敗しました')
    } finally {
      setLoading(false)
    }
  }, [client, scopeId, scopeKind])

  useEffect(() => { void reload() }, [reload])

  const patchSupplier = async (supplier: SessionPoolSupplier, patch: { enabled?: boolean; draining?: boolean; min_idle?: number; max_runners?: number }) => {
    setError(null)
    try {
      await client.patchManagedSessionPoolSupplier(supplier.pool, supplier.manager_id, patch)
      await reload()
    } catch (reason) {
      setError(reason instanceof Error ? reason.message : 'Supplierの更新に失敗しました')
      throw reason
    }
  }

  const patchPool = async (pool: LogicalSessionPool, patch: { enabled?: boolean; labels?: Record<string, string> }) => {
    setError(null)
    try {
      await client.patchManagedSessionPool(pool.name, patch)
      await reload()
    } catch (reason) {
      setError(reason instanceof Error ? reason.message : 'Poolの更新に失敗しました')
      throw reason
    }
  }

  const patchBinding = async (binding: SessionPoolBinding, patch: { roles?: Array<'use' | 'manage'>; enabled?: boolean; priority?: number; max_concurrent?: number }) => {
    setError(null)
    try {
      await client.patchManagedSessionPoolBinding(binding.pool, binding.id, patch)
      await reload()
    } catch (reason) {
      setError(reason instanceof Error ? reason.message : 'Bindingの更新に失敗しました')
      throw reason
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

  const togglePoolUsage = async (runtime: PoolRuntime) => {
    const { pool, scopeBinding } = runtime
    setBusyAction(`usage:${pool.name}`)
    setError(null)
    try {
      if (scopeBinding.subject_type === 'all') {
        await client.createManagedSessionPoolBinding(
          pool.name,
          scopeKind === 'personal' ? 'user' : 'team',
          scopeId,
          'use',
          0,
          0,
          false,
        )
      } else {
        await client.patchManagedSessionPoolBinding(pool.name, scopeBinding.id, { enabled: !scopeBinding.enabled })
      }
      await reload()
    } catch (reason) {
      setError(reason instanceof Error ? reason.message : 'Pool利用設定の更新に失敗しました')
    } finally {
      setBusyAction(null)
    }
  }

  const createPool = async () => {
    const name = newPoolName.trim()
    if (!name) return
    setBusyAction('create-pool')
    setError(null)
    try {
      await client.createManagedSessionPool({ name, ...(scopeKind === 'team' ? { team_id: scopeId } : {}) })
      setNewPoolName('')
      setShowCreatePool(false)
      await reload()
    } catch (reason) {
      setError(reason instanceof Error ? reason.message : 'Poolの作成に失敗しました')
    } finally {
      setBusyAction(null)
    }
  }

  const deletePool = async (pool: LogicalSessionPool) => {
    if (!window.confirm(`Pool「${pool.name}」を削除しますか？\nSupplierとBindingも削除されます。`)) return
    setBusyAction(`delete:${pool.name}`)
    setError(null)
    try {
      await client.deleteManagedSessionPool(pool.name)
      await reload()
    } catch (reason) {
      setError(reason instanceof Error ? reason.message : 'Poolの削除に失敗しました')
    } finally {
      setBusyAction(null)
    }
  }

  const assignManager = async (poolName: string) => {
    if (!assignManagerID) return
    setBusyAction(`assign:${poolName}`)
    setError(null)
    try {
      await client.createManagedSessionPoolSupplier(poolName, assignManagerID, { enabled: true })
      setAssignManagerID('')
      setAssigningPool(null)
      await reload()
    } catch (reason) {
      setError(reason instanceof Error ? reason.message : 'Managerの割り当てに失敗しました')
    } finally {
      setBusyAction(null)
    }
  }

  const unassignManager = async (supplier: SessionPoolSupplier) => {
    const name = managerByID.get(supplier.manager_id)?.name ?? supplier.manager_id
    if (!window.confirm(`${name} を「${supplier.pool}」から割り当て解除しますか？`)) return
    setBusyAction(`unassign:${supplier.pool}:${supplier.manager_id}`)
    setError(null)
    try {
      await client.deleteManagedSessionPoolSupplier(supplier.pool, supplier.manager_id)
      await reload()
    } catch (reason) {
      setError(reason instanceof Error ? reason.message : 'Managerの割り当て解除に失敗しました')
    } finally {
      setBusyAction(null)
    }
  }

  const managerByID = new Map(managers.filter((manager) => manager.id).map((manager) => [manager.id!, manager]))
  const onlineManagers = managers.filter(heartbeatOnline).length
  const activeSessions = managers.reduce((sum, manager) => sum + (manager.active_sessions ?? 0), 0)
  const totalCapacity = runtimes.flatMap((runtime) => runtime.suppliers).reduce((sum, supplier) => sum + (supplier.max_runners ?? 0), 0)

  const tabs: { id: PoolTab; label: string }[] = [
    { id: 'managers', label: 'Manager' },
    { id: 'pools', label: 'Pool' },
    { id: 'assignments', label: '割り当て設定' },
    { id: 'status', label: '稼働状況' },
  ]

  return (
    <>
      {showHeader && (
        <SettingsPageHeader
          title="プール"
          description="セッションの実行先、供給元、利用権限、稼働状態をまとめて管理します。"
        />
      )}

      <div className="mb-5 border-b border-gray-200 dark:border-gray-800">
        <div className="flex overflow-x-auto" role="tablist" aria-label="Pool運用メニュー">
          {tabs.map((tab) => (
            <button key={tab.id} type="button" role="tab" aria-selected={activeTab === tab.id} onClick={() => setActiveTab(tab.id)} className={`shrink-0 border-b-2 px-4 py-3 text-sm font-medium transition-colors ${activeTab === tab.id ? 'border-blue-600 text-blue-600 dark:text-blue-400' : 'border-transparent text-gray-500 hover:text-gray-800 dark:text-gray-400 dark:hover:text-gray-200'}`}>
              {tab.label}
            </button>
          ))}
        </div>
      </div>

      <div className="mb-5 flex justify-end">
        <button type="button" onClick={() => void reload()} disabled={loading} className="inline-flex items-center gap-1.5 rounded-md border border-gray-300 px-3 py-2 text-sm text-gray-700 hover:bg-gray-50 disabled:opacity-50 dark:border-gray-700 dark:text-gray-200 dark:hover:bg-gray-800">
          <RefreshCw className={`h-4 w-4 ${loading ? 'animate-spin' : ''}`} /> 更新
        </button>
      </div>

      {error && <div role="alert" className="mb-5 rounded-lg border border-red-200 bg-red-50 p-3 text-sm text-red-700 dark:border-red-900 dark:bg-red-950/40 dark:text-red-300">{error}</div>}

      {activeTab === 'managers' && (
        <div className="space-y-5">
          <div className="flex items-center justify-between gap-3">
            <div><h2 className="text-base font-semibold text-gray-950 dark:text-white">Manager</h2><p className="mt-1 text-sm text-gray-500">登録、接続確認、ログ、再起動、バージョンアップを管理します。</p></div>
            <button type="button" onClick={() => setShowRegistration((value) => !value)} className="shrink-0 rounded-md bg-blue-600 px-3 py-2 text-sm font-medium text-white hover:bg-blue-700">{showRegistration ? '閉じる' : '追加'}</button>
          </div>
          {showRegistration && <SettingsSubsection title="Managerを登録" description="登録後、割り当て設定タブからPoolへ割り当てます"><ESMRegistrationToken scope={scopeKind === 'personal' ? 'user' : 'team'} teamId={scopeKind === 'team' ? scopeId : undefined} /></SettingsSubsection>}
          <ExternalSessionManagerList managers={managers} onChange={(changed) => update({ external_session_managers: changed })} revealedTokens={revealedTokens} onRegenerate={regenerateEsmToken} regeneratingEsmId={regeneratingEsmId} scope={scopeKind === 'personal' ? 'user' : 'team'} teamId={scopeKind === 'team' ? scopeId : undefined} />
        </div>
      )}

      {activeTab === 'pools' && <div className="space-y-4">
        <div className="flex items-center justify-between gap-3"><div><h2 className="text-base font-semibold text-gray-950 dark:text-white">Pool</h2><p className="mt-1 text-sm text-gray-500">実行先の論理グループを作成・停止・削除します。</p></div><button type="button" onClick={() => setShowCreatePool((value) => !value)} className="inline-flex shrink-0 items-center gap-1.5 rounded-md bg-blue-600 px-3 py-2 text-sm font-medium text-white hover:bg-blue-700"><Plus className="h-4 w-4" /> 作成</button></div>
      {showCreatePool && (
        <div className={`${card} mb-5`}>
          <div className="flex items-center justify-between gap-3">
            <div>
              <h2 className="text-sm font-semibold text-gray-950 dark:text-white">新しいPool</h2>
              <p className="mt-1 text-xs text-gray-500">英小文字、数字、ハイフンを使った名前を推奨します。</p>
            </div>
            <button type="button" onClick={() => setShowCreatePool(false)} aria-label="閉じる" className="rounded p-1 text-gray-500 hover:bg-gray-100 dark:hover:bg-gray-800"><X className="h-4 w-4" /></button>
          </div>
          <div className="mt-4 flex flex-col gap-2 sm:flex-row">
            <input value={newPoolName} onChange={(event) => setNewPoolName(event.target.value)} onKeyDown={(event) => { if (event.key === 'Enter') void createPool() }} placeholder="例: linux-builders" className="min-w-0 flex-1 rounded-md border border-gray-300 bg-white px-3 py-2 text-sm dark:border-gray-700 dark:bg-gray-950 dark:text-white" />
            <button type="button" onClick={() => void createPool()} disabled={!newPoolName.trim() || busyAction === 'create-pool'} className="rounded-md bg-blue-600 px-4 py-2 text-sm font-medium text-white disabled:opacity-50">{busyAction === 'create-pool' ? '作成中...' : '作成'}</button>
          </div>
        </div>
      )}
        {!loading && runtimes.length === 0 && (
          <div className={`${card} py-10 text-center`}>
            <Boxes className="mx-auto h-8 w-8 text-gray-400" />
            <p className="mt-3 text-sm font-medium text-gray-900 dark:text-white">利用可能なPoolはありません</p>
            <p className="mt-1 text-xs text-gray-500">「作成」から最初のPoolを作成してください。</p>
          </div>
        )}

        {runtimes.length > 0 && <div className="overflow-x-auto rounded-xl border border-gray-200 bg-white shadow-sm dark:border-gray-800 dark:bg-gray-900"><table className="min-w-[860px] w-full text-left text-sm"><thead className="bg-gray-50 text-xs font-semibold uppercase tracking-wide text-gray-500 dark:bg-gray-950 dark:text-gray-400"><tr><th className="px-4 py-3">Pool</th><th className="px-4 py-3">状態 / 権限</th><th className="px-4 py-3">Labels</th><th className="px-4 py-3 text-right">Suppliers</th><th className="px-4 py-3 text-right">Runners</th><th className="px-4 py-3 text-right">Bindings</th><th className="px-4 py-3 text-right">操作</th></tr></thead><tbody className="divide-y divide-gray-200 dark:divide-gray-800">{runtimes.map((runtime) => {
          const { pool, suppliers, bindings, scopeBinding } = runtime
          const canManage = (scopeBinding.role === 'manage' || scopeBinding.role === 'manage_and_use') && scopeBinding.enabled
          const canUse = (scopeBinding.role === 'use' || scopeBinding.role === 'manage_and_use') && scopeBinding.enabled
          const idle = suppliers.reduce((sum, supplier) => sum + (supplier.idle_runners ?? 0), 0)
          const runners = suppliers.reduce((sum, supplier) => sum + (supplier.total_runners ?? 0), 0)
          return <tr key={pool.name} className="align-middle hover:bg-gray-50/70 dark:hover:bg-gray-800/40"><td className="px-4 py-3 font-semibold text-gray-950 dark:text-white">{pool.name}</td><td className="px-4 py-3"><span className={`rounded-full px-2 py-1 text-xs font-medium ${canUse && pool.enabled ? 'bg-emerald-100 text-emerald-700 dark:bg-emerald-950 dark:text-emerald-300' : 'bg-gray-100 text-gray-600 dark:bg-gray-800 dark:text-gray-300'}`}>{canUse && pool.enabled ? '使用中' : '停止中'}</span><span className="ml-2 text-xs text-gray-500">{scopeBinding.role}</span></td><td className="max-w-52 px-4 py-3"><div className="flex flex-wrap gap-1">{Object.entries(pool.labels ?? {}).map(([key, value]) => <code key={key} className="rounded bg-gray-100 px-1.5 py-0.5 text-[11px] dark:bg-gray-800">{key}={value}</code>)}{Object.keys(pool.labels ?? {}).length === 0 && <span className="text-xs text-gray-400">—</span>}</div></td><td className="px-4 py-3 text-right tabular-nums">{canManage ? suppliers.length : '—'}</td><td className="px-4 py-3 text-right tabular-nums">{canManage ? <>{idle} idle / {runners}</> : '—'}</td><td className="px-4 py-3 text-right tabular-nums">{canManage ? bindings.length : '—'}</td><td className="px-4 py-3"><div className="flex justify-end gap-1.5">{canManage && <PoolSettingsEditor pool={pool} onSave={(patch) => patchPool(pool, patch)} />}{scopeBinding.role === 'use' && <button type="button" onClick={() => void togglePoolUsage(runtime)} disabled={busyAction === `usage:${pool.name}` || !pool.enabled} className="rounded-md border border-blue-300 px-2.5 py-1.5 text-xs text-blue-700 disabled:opacity-50 dark:border-blue-800 dark:text-blue-300">{scopeBinding.enabled ? '使用停止' : '使用'}</button>}{canManage && <button type="button" onClick={() => void togglePool(pool)} className="rounded-md border border-gray-300 px-2.5 py-1.5 text-xs text-gray-700 dark:border-gray-700 dark:text-gray-200">{pool.enabled ? '停止' : '有効化'}</button>}{canManage && <button type="button" aria-label={`${pool.name}を削除`} onClick={() => void deletePool(pool)} disabled={busyAction === `delete:${pool.name}`} className="rounded-md border border-red-200 p-1.5 text-red-600 disabled:opacity-50 dark:border-red-900 dark:text-red-400"><Trash2 className="h-3.5 w-3.5" /></button>}</div></td></tr>
        })}</tbody></table></div>}

      </div>}

      {activeTab === 'assignments' && <div className="space-y-4">
        <div><h2 className="text-base font-semibold text-gray-950 dark:text-white">割り当て設定</h2><p className="mt-1 text-sm text-gray-500">ManagerをPoolへ割り当て、供給の有効・無効を管理します。</p></div>
        {runtimes.length === 0 && <div className={`${card} py-8 text-center text-sm text-gray-500`}>先にPoolを作成してください。</div>}
        {runtimes.filter(({ scopeBinding }) => (scopeBinding.role === 'manage' || scopeBinding.role === 'manage_and_use') && scopeBinding.enabled).map(({ pool, suppliers, bindings }) => <section key={pool.name} className="overflow-hidden rounded-xl border border-gray-200 bg-white dark:border-gray-800 dark:bg-gray-900">
          <div className="flex flex-wrap items-center justify-between gap-3 border-b border-gray-200 px-4 py-3 dark:border-gray-800"><h3 className="break-all font-semibold text-gray-950 dark:text-white">{pool.name}</h3><button type="button" onClick={() => { setAssigningPool(assigningPool === pool.name ? null : pool.name); setAssignManagerID('') }} className="inline-flex items-center gap-1 rounded-md border border-blue-300 px-2.5 py-1.5 text-xs font-medium text-blue-700 dark:border-blue-800 dark:text-blue-300"><UserPlus className="h-3.5 w-3.5" /> Managerを割り当て</button></div>
          {assigningPool === pool.name && <div className="flex flex-col gap-2 border-b border-gray-200 bg-blue-50/50 p-3 sm:flex-row dark:border-gray-800 dark:bg-blue-950/20"><select value={assignManagerID} onChange={(event) => setAssignManagerID(event.target.value)} className="min-w-0 flex-1 rounded-md border border-gray-300 bg-white px-3 py-2 text-sm dark:border-gray-700 dark:bg-gray-950 dark:text-white"><option value="">Managerを選択</option>{managers.filter((manager) => manager.id && !suppliers.some((supplier) => supplier.manager_id === manager.id)).map((manager) => <option key={manager.id} value={manager.id}>{manager.name}</option>)}</select><button type="button" onClick={() => void assignManager(pool.name)} disabled={!assignManagerID || busyAction === `assign:${pool.name}`} className="rounded-md bg-blue-600 px-4 py-2 text-sm font-medium text-white disabled:opacity-50">割り当て</button></div>}
          <div className="overflow-x-auto"><table className="min-w-[760px] w-full text-left text-sm"><thead className="bg-gray-50 text-xs text-gray-500 dark:bg-gray-950"><tr><th className="px-4 py-2">Supplier / Manager</th><th className="px-4 py-2 text-right">Min idle</th><th className="px-4 py-2 text-right">Idle / Total</th><th className="px-4 py-2 text-right">Max runners</th><th className="px-4 py-2">状態</th><th className="px-4 py-2 text-right">操作</th></tr></thead><tbody className="divide-y divide-gray-200 dark:divide-gray-800">{suppliers.length === 0 && <tr><td colSpan={6} className="px-4 py-6 text-center text-gray-500">Managerは割り当てられていません。</td></tr>}{suppliers.map((supplier) => <tr key={supplier.manager_id}><td className="px-4 py-3 font-medium text-gray-900 dark:text-white">{managerByID.get(supplier.manager_id)?.name ?? supplier.manager_id}</td><td className="px-4 py-3 text-right tabular-nums">{supplier.min_idle ?? 0}</td><td className="px-4 py-3 text-right tabular-nums">{supplier.idle_runners ?? 0} / {supplier.total_runners ?? 0}</td><td className="px-4 py-3 text-right tabular-nums">{supplier.max_runners || '∞'}</td><td className="px-4 py-3"><PoolSupplierControls supplier={supplier} onPatch={(patch) => { void patchSupplier(supplier, patch).catch(() => undefined) }} /></td><td className="px-4 py-3"><div className="flex justify-end gap-2"><SupplierSettingsEditor supplier={supplier} onSave={(patch) => patchSupplier(supplier, patch)} /><button type="button" onClick={() => void unassignManager(supplier)} className="text-xs text-red-600 dark:text-red-400">解除</button></div></td></tr>)}</tbody></table></div>
          <div className="border-t border-gray-200 dark:border-gray-800"><div className="bg-gray-50 px-4 py-2 text-xs font-semibold text-gray-500 dark:bg-gray-950">Bindings</div><div className="overflow-x-auto"><table className="min-w-[680px] w-full text-left text-sm"><thead className="text-xs text-gray-500"><tr><th className="px-4 py-2">Subject</th><th className="px-4 py-2">Roles</th><th className="px-4 py-2 text-right">Priority</th><th className="px-4 py-2 text-right">Max concurrent</th><th className="px-4 py-2">状態</th><th className="px-4 py-2 text-right">操作</th></tr></thead><tbody className="divide-y divide-gray-200 dark:divide-gray-800">{bindings.length === 0 && <tr><td colSpan={6} className="px-4 py-5 text-center text-gray-500">Bindingはありません。</td></tr>}{bindings.map((binding) => <tr key={binding.id}><td className="px-4 py-3"><span className="inline-flex items-center gap-1"><Users className="h-3 w-3" />{binding.subject_type}: {binding.subject_id || 'everyone'}</span></td><td className="px-4 py-3">{binding.roles?.join(', ') || binding.role}</td><td className="px-4 py-3 text-right tabular-nums">{binding.priority ?? 0}</td><td className="px-4 py-3 text-right tabular-nums">{binding.max_concurrent || '∞'}</td><td className="px-4 py-3">{binding.enabled ? '有効' : '無効'}</td><td className="px-4 py-3 text-right"><BindingSettingsEditor binding={binding} onSave={(patch) => patchBinding(binding, patch)} /></td></tr>)}</tbody></table></div></div>
        </section>)}
      </div>}

      {activeTab === 'status' && <div className="space-y-5">
        <div><h2 className="text-base font-semibold text-gray-950 dark:text-white">稼働状況</h2><p className="mt-1 text-sm text-gray-500">Manager接続、セッション数、Runner容量を確認します。</p></div>
        <dl className="divide-y divide-gray-200 overflow-hidden rounded-xl border border-gray-200 bg-white dark:divide-gray-800 dark:border-gray-800 dark:bg-gray-900">{[['Pool', runtimes.length], ['Manager online', `${onlineManagers}/${managers.length}`], ['Active sessions', activeSessions], ['Runner capacity', totalCapacity || '∞']].map(([label, value]) => <div key={label} className="flex items-center justify-between px-4 py-3"><dt className="text-sm text-gray-500 dark:text-gray-400">{label}</dt><dd className="font-semibold tabular-nums text-gray-950 dark:text-white">{value}</dd></div>)}</dl>
        <div className={card}><h3 className="mb-3 text-sm font-semibold text-gray-950 dark:text-white">Manager接続</h3><div className="divide-y divide-gray-200 dark:divide-gray-800">{managers.map((manager) => <div key={manager.id || manager.name} className="flex items-center justify-between gap-3 py-3"><div className="min-w-0"><p className="truncate text-sm font-medium text-gray-900 dark:text-white">{manager.name}</p><p className="mt-0.5 text-xs text-gray-500">{manager.version || 'version unknown'} · {manager.active_sessions ?? 0} sessions</p></div><span className={`shrink-0 rounded-full px-2 py-1 text-xs font-medium ${heartbeatOnline(manager) ? 'bg-emerald-100 text-emerald-700 dark:bg-emerald-950 dark:text-emerald-300' : 'bg-amber-100 text-amber-700 dark:bg-amber-950 dark:text-amber-300'}`}>{heartbeatOnline(manager) ? 'Online' : 'Offline'}</span></div>)}</div></div>
      </div>}
    </>
  )
}
