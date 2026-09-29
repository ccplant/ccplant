'use client'

import { FormEvent, ReactNode, useCallback, useEffect, useMemo, useRef, useState } from 'react'
import { MoreHorizontal, RefreshCw, Terminal } from 'lucide-react'
import { createCurrentDeploymentAgentAPIProxyClient } from '@/lib/agentapi-proxy-client'
import {
  ClusterSessionManager,
  LogicalSessionPool,
  SessionPoolBinding,
  SessionPoolLogs,
  SessionPoolManagerStatus,
  SessionPoolSupplier,
} from '@/types/session_pool'
import {
  ItemList,
  ItemListEmpty,
  ItemListRow,
  RowAction,
  SettingsPageHeader,
  SettingsSubsection,
  StatusBadge,
} from '@/components/settings'
import { BindingSettingsEditor, PoolSettingsEditor, SupplierSettingsEditor } from '@/components/settings/PoolResourceEditors'

const menuItemClass = 'block w-full px-3 py-2 text-left text-sm text-gray-700 hover:bg-gray-100 dark:text-gray-200 dark:hover:bg-gray-800'

function RowActionsMenu({ label, children }: { label: string; children: ReactNode }) {
  const [open, setOpen] = useState(false)
  const containerRef = useRef<HTMLDivElement>(null)

  useEffect(() => {
    if (!open) return
    const close = (event: MouseEvent) => {
      if (!containerRef.current?.contains(event.target as Node)) setOpen(false)
    }
    document.addEventListener('mousedown', close)
    return () => document.removeEventListener('mousedown', close)
  }, [open])

  return <div ref={containerRef} className="relative inline-flex">
    <button type="button" aria-label={label} aria-haspopup="menu" aria-expanded={open} onClick={() => setOpen((current) => !current)} className="rounded-md p-1.5 text-gray-500 hover:bg-gray-100 hover:text-gray-900 dark:hover:bg-gray-800 dark:hover:text-white"><MoreHorizontal className="h-4 w-4" /></button>
    {open && <div role="menu" className="absolute right-0 top-full z-20 mt-1 w-32 rounded-lg border border-gray-200 bg-white py-1 shadow-lg dark:border-gray-700 dark:bg-gray-900">{children}</div>}
  </div>
}

function LogPanel({ target, result, onClose }: { target: string; result: SessionPoolLogs | null; onClose: () => void }) {
  return <div className="mt-3 rounded-lg border border-gray-700 bg-gray-950 p-3 text-gray-100">
    <div className="mb-2 flex items-center justify-between gap-3">
      <div className="min-w-0">
        <p className="truncate font-mono text-xs">{target}</p>
        {result?.source && <p className="truncate font-mono text-[10px] text-gray-500">{result.source}</p>}
      </div>
      <button type="button" onClick={onClose} className="text-xs text-gray-400 hover:text-white">閉じる</button>
    </div>
    <pre className="max-h-80 overflow-auto whitespace-pre-wrap rounded bg-black p-2 font-mono text-[11px] leading-4 text-gray-300">{result ? (result.lines.length ? result.lines.join('\n') : 'ログはありません') : 'ログを読み込み中...'}</pre>
  </div>
}

export default function SessionPoolsAdminPage() {
  const client = useMemo(() => createCurrentDeploymentAgentAPIProxyClient(), [])
  const [managers, setManagers] = useState<ClusterSessionManager[]>([])
  const [pools, setPools] = useState<LogicalSessionPool[]>([])
  const [suppliers, setSuppliers] = useState<SessionPoolSupplier[]>([])
  const [bindings, setBindings] = useState<Record<string, SessionPoolBinding[]>>({})
  const [runtimeStatuses, setRuntimeStatuses] = useState<SessionPoolManagerStatus[]>([])
  const [loading, setLoading] = useState(true)
  const [logTarget, setLogTarget] = useState<string | null>(null)
  const [logResult, setLogResult] = useState<SessionPoolLogs | null>(null)
  const [managerName, setManagerName] = useState('')
  const [managerID, setManagerID] = useState('')
  const [poolName, setPoolName] = useState('')
  const [supplierPool, setSupplierPool] = useState('')
  const [minIdle, setMinIdle] = useState(1)
  const [maxRunners, setMaxRunners] = useState(10)
  const [subjectType, setSubjectType] = useState<'user' | 'team' | 'all'>('team')
  const [subjectID, setSubjectID] = useState('')
  const [bindingRoles, setBindingRoles] = useState<Array<'use' | 'manage'>>(['use'])
  const [bindingPool, setBindingPool] = useState('')
  const [bindingPriority, setBindingPriority] = useState(0)
  const [maxConcurrent, setMaxConcurrent] = useState(0)
  const [connectionToken, setConnectionToken] = useState('')
  const [error, setError] = useState('')

  const reload = useCallback(async () => {
    setLoading(true)
    try {
      const [nextManagers, nextPools, statusResult] = await Promise.all([
        client.listClusterSessionManagers(),
        client.listSessionPools(),
        client.getSessionPoolStatus().catch(() => ({ session_pools: [], session_managers: [] })),
      ])
      const nextSuppliers = (await Promise.all(
        nextManagers.map((manager) => client.listSessionPoolSuppliers(manager.id)),
      )).flat()
      const nextBindings = await Promise.all(
        nextPools.map(async (pool) => [pool.name, await client.listSessionPoolBindings(pool.name)] as const),
      )
      setManagers(nextManagers)
      setPools(nextPools)
      setSuppliers(nextSuppliers)
      setBindings(Object.fromEntries(nextBindings))
      setRuntimeStatuses(statusResult.session_managers)
      setManagerID((current) => current || nextManagers[0]?.id || '')
      setSupplierPool((current) => current || nextPools[0]?.name || '')
      setBindingPool((current) => current || nextPools[0]?.name || '')
      setError('')
    } catch (reason) {
      setError(reason instanceof Error ? reason.message : '読み込みに失敗しました')
    } finally {
      setLoading(false)
    }
  }, [client])

  useEffect(() => { void reload() }, [reload])

  const showLogs = async (kind: 'manager' | 'runner', id: string) => {
    const target = `${kind}:${id}`
    setLogTarget(target)
    setLogResult(null)
    setError('')
    try {
      setLogResult(kind === 'manager'
        ? await client.getSessionPoolManagerLogs(id)
        : await client.getSessionPoolRunnerLogs(id))
    } catch (reason) {
      setError(reason instanceof Error ? reason.message : 'ログの取得に失敗しました')
    }
  }

  const addManager = async (event: FormEvent) => {
    event.preventDefault()
    try {
      const result = await client.createClusterSessionManager(managerName)
      setConnectionToken(result.registration_token)
      setManagerName('')
      setManagerID(result.manager.id)
      await reload()
    } catch (reason) {
      setError(reason instanceof Error ? reason.message : 'Manager登録に失敗しました')
    }
  }

  const addLogicalPool = async (event: FormEvent) => {
    event.preventDefault()
    try {
      const pool = await client.createSessionPool({ name: poolName })
      setPoolName('')
      setSupplierPool(pool.name)
      setBindingPool(pool.name)
      await reload()
    } catch (reason) {
      setError(reason instanceof Error ? reason.message : 'Logical Pool作成に失敗しました')
    }
  }

  const addSupplier = async (event: FormEvent) => {
    event.preventDefault()
    try {
      await client.createSessionPoolSupplier(managerID, {
        pool: supplierPool,
        min_idle: minIdle,
        max_runners: maxRunners,
      })
      await reload()
    } catch (reason) {
      setError(reason instanceof Error ? reason.message : 'Pool Supplier追加に失敗しました')
    }
  }

  const addBinding = async (event: FormEvent) => {
    event.preventDefault()
    try {
      await client.createSessionPoolBinding(bindingPool, subjectType, subjectID, bindingRoles, bindingPriority, maxConcurrent)
      setSubjectID('')
      await reload()
    } catch (reason) {
      setError(reason instanceof Error ? reason.message : 'Pool Binding作成に失敗しました')
    }
  }

  const removeBinding = async (pool: string, bindingID: string) => {
    try {
      await client.deleteSessionPoolBinding(pool, bindingID)
      await reload()
    } catch (reason) {
      setError(reason instanceof Error ? reason.message : 'Pool Binding削除に失敗しました')
    }
  }

  const removeManager = async (manager: ClusterSessionManager) => {
    if (!window.confirm(`Session Manager「${manager.name}」を削除しますか？関連するSupplierと待機Runnerも削除されます。`)) return
    try {
      await client.deleteClusterSessionManager(manager.id)
      await reload()
    } catch (reason) {
      setError(reason instanceof Error ? reason.message : 'Session Manager削除に失敗しました')
    }
  }

  const removePool = async (pool: LogicalSessionPool) => {
    if (!window.confirm(`Logical Pool「${pool.name}」を削除しますか？Supplier、Bindingなどの関連リソースも削除されます。`)) return
    try {
      await client.deleteSessionPool(pool.name)
      await reload()
    } catch (reason) {
      setError(reason instanceof Error ? reason.message : 'Logical Pool削除に失敗しました')
    }
  }

  const removeSupplier = async (supplier: SessionPoolSupplier) => {
    const manager = managers.find((item) => item.id === supplier.manager_id)
    if (!window.confirm(`${manager?.name || supplier.manager_id} の「${supplier.pool}」Supplierを削除しますか？待機Runnerも削除されます。`)) return
    try {
      await client.deleteSessionPoolSupplier(supplier.manager_id, supplier.pool)
      await reload()
    } catch (reason) {
      setError(reason instanceof Error ? reason.message : 'Pool Supplier削除に失敗しました')
    }
  }

  const togglePool = async (pool: LogicalSessionPool) => {
    try {
      await client.patchManagedSessionPool(pool.name, { enabled: !pool.enabled })
      await reload()
    } catch (reason) {
      setError(reason instanceof Error ? reason.message : 'Logical Pool更新に失敗しました')
    }
  }

  const patchPool = async (pool: LogicalSessionPool, patch: { enabled?: boolean; labels?: Record<string, string> }) => {
    try {
      await client.patchManagedSessionPool(pool.name, patch)
      await reload()
    } catch (reason) {
      setError(reason instanceof Error ? reason.message : 'Logical Pool更新に失敗しました')
      throw reason
    }
  }

  const patchSupplier = async (supplier: SessionPoolSupplier, patch: { enabled?: boolean; draining?: boolean; min_idle?: number; max_runners?: number }) => {
    try {
      await client.patchManagedSessionPoolSupplier(supplier.pool, supplier.manager_id, patch)
      await reload()
    } catch (reason) {
      setError(reason instanceof Error ? reason.message : 'Pool Supplier更新に失敗しました')
      throw reason
    }
  }

  const patchBinding = async (binding: SessionPoolBinding, patch: { roles?: Array<'use' | 'manage'>; enabled?: boolean; priority?: number; max_concurrent?: number }) => {
    try {
      await client.patchManagedSessionPoolBinding(binding.pool, binding.id, patch)
      await reload()
    } catch (reason) {
      setError(reason instanceof Error ? reason.message : 'Pool Binding更新に失敗しました')
      throw reason
    }
  }

  const input =
    'w-full rounded-md border border-gray-300 bg-white px-3 py-2 text-sm text-gray-900 focus:border-transparent focus:outline-none focus:ring-2 focus:ring-blue-500 dark:border-gray-700 dark:bg-gray-800 dark:text-white'
  const stepCard = 'space-y-3 rounded-lg border border-gray-200 p-4 dark:border-gray-700'
  const stepButton =
    'w-fit rounded-md bg-blue-600 px-3 py-1.5 text-sm font-semibold text-white transition-colors hover:bg-blue-700 disabled:cursor-not-allowed disabled:opacity-50'
  const stepNumber =
    'flex h-6 w-6 flex-shrink-0 items-center justify-center rounded-md bg-blue-50 text-xs font-bold tabular-nums text-blue-600 dark:bg-blue-900/40 dark:text-blue-300'

  return (
    <>
      <SettingsPageHeader
        title="Session Pools"
        description="Logical Pool、Manager ごとの供給設定、user / team の利用権限をクラスタ全体で管理します。"
        action={
          <button type="button" onClick={() => void reload()} disabled={loading} className="inline-flex items-center gap-1.5 rounded-md border border-gray-300 px-3 py-2 text-sm text-gray-700 hover:bg-gray-50 disabled:opacity-50 dark:border-gray-700 dark:text-gray-200 dark:hover:bg-gray-800">
            <RefreshCw className={`h-4 w-4 ${loading ? 'animate-spin' : ''}`} /> 更新
          </button>
        }
      />

      {error && (
        <div role="alert" className="mb-5 rounded-lg border border-red-200 bg-red-50 p-4 dark:border-red-800 dark:bg-red-900/20">
          <p className="text-sm text-red-600 dark:text-red-400">{error}</p>
        </div>
      )}

      {connectionToken && (
        <div className="mb-5 rounded-lg border border-amber-300 bg-amber-50 p-4 dark:border-amber-700 dark:bg-amber-900/20">
          <p className="text-sm font-semibold text-amber-900 dark:text-amber-200">
            Registration token は今だけ表示されます
          </p>
          <code className="mt-2 block break-all font-mono text-xs text-amber-900 dark:text-amber-200">
            {connectionToken}
          </code>
          <button
            type="button"
            className="mt-2 text-xs text-amber-900 underline dark:text-amber-200"
            onClick={() => setConnectionToken('')}
          >
            閉じる
          </button>
        </div>
      )}

      <SettingsSubsection title="稼働状況" description="Manager の制御チャネル、Runner、ログをライブで確認します">
        <div className="grid gap-3 sm:grid-cols-3">
          {[
            ['Manager online', `${runtimeStatuses.filter((item) => item.online).length}/${runtimeStatuses.length}`],
            ['Running runners', runtimeStatuses.reduce((sum, item) => sum + (item.status?.running_runners || 0), 0)],
            ['Used runners', runtimeStatuses.reduce((sum, item) => sum + (item.status?.used_runners || 0), 0)],
          ].map(([label, value]) => (
            <div key={label} className="rounded-lg border border-gray-200 bg-white p-4 dark:border-gray-800 dark:bg-gray-900">
              <p className="text-xs text-gray-500 dark:text-gray-400">{label}</p>
              <p className="mt-1 text-2xl font-semibold tabular-nums text-gray-950 dark:text-white">{value}</p>
            </div>
          ))}
        </div>
      </SettingsSubsection>

      <SettingsSubsection
        title="セットアップ"
        description="Manager から Binding まで、上から順に登録します"
      >
        <div className="grid gap-4 md:grid-cols-2">
          <form onSubmit={addManager} className={stepCard}>
            <div className="flex items-center gap-2">
              <span className={stepNumber}>1</span>
              <h4 className="text-sm font-semibold text-gray-900 dark:text-white">Session Manager</h4>
            </div>
            <p className="text-xs text-gray-500 dark:text-gray-400">Pool を供給する実行環境を登録します。</p>
            <input required className={input} value={managerName} onChange={(event) => setManagerName(event.target.value)} placeholder="Tokyo Kubernetes" />
            <button className={stepButton}>Manager を登録</button>
          </form>

          <form onSubmit={addLogicalPool} className={stepCard}>
            <div className="flex items-center gap-2">
              <span className={stepNumber}>2</span>
              <h4 className="text-sm font-semibold text-gray-900 dark:text-white">Logical Pool</h4>
            </div>
            <p className="text-xs text-gray-500 dark:text-gray-400">認可やセッション要求が参照するクラスタ共通の Pool です。</p>
            <input required className={input} value={poolName} onChange={(event) => setPoolName(event.target.value)} placeholder="linux-standard" />
            <button className={stepButton}>Logical Pool を作成</button>
          </form>

          <form onSubmit={addSupplier} className={stepCard}>
            <div className="flex items-center gap-2">
              <span className={stepNumber}>3</span>
              <h4 className="text-sm font-semibold text-gray-900 dark:text-white">Pool Supplier</h4>
            </div>
            <p className="text-xs text-gray-500 dark:text-gray-400">Manager が供給する Logical Pool とキャパシティを設定します。</p>
            <select required className={input} value={managerID} onChange={(event) => setManagerID(event.target.value)}>
              <option value="" disabled>Manager を選択</option>
              {managers.map((manager) => <option key={manager.id} value={manager.id}>{manager.name}</option>)}
            </select>
            <select required className={input} value={supplierPool} onChange={(event) => setSupplierPool(event.target.value)}>
              <option value="" disabled>Logical Pool を選択</option>
              {pools.map((pool) => <option key={pool.name} value={pool.name}>{pool.name}</option>)}
            </select>
            <div className="grid grid-cols-2 gap-3">
              <label className="text-xs text-gray-500 dark:text-gray-400">Min idle<input required min={0} type="number" className={input} value={minIdle} onChange={(event) => setMinIdle(Number(event.target.value))} /></label>
              <label className="text-xs text-gray-500 dark:text-gray-400">Max runners<input required min={0} type="number" className={input} value={maxRunners} onChange={(event) => setMaxRunners(Number(event.target.value))} /></label>
            </div>
            <button disabled={!managerID || !supplierPool} className={stepButton}>Supplier を追加</button>
          </form>

          <form onSubmit={addBinding} className={stepCard}>
            <div className="flex items-center gap-2">
              <span className={stepNumber}>4</span>
              <h4 className="text-sm font-semibold text-gray-900 dark:text-white">Pool Binding</h4>
            </div>
            <p className="text-xs text-gray-500 dark:text-gray-400">Logical Pool を利用できる user、team、またはクラスタ内の全員を指定します。</p>
            <select required className={input} value={bindingPool} onChange={(event) => setBindingPool(event.target.value)}>
              <option value="" disabled>Logical Pool を選択</option>
              {pools.map((pool) => <option key={pool.name} value={pool.name}>{pool.name}</option>)}
            </select>
            <select className={input} value={subjectType} onChange={(event) => {
              const nextType = event.target.value as 'user' | 'team' | 'all'
              setSubjectType(nextType)
              if (nextType === 'all') {
                setSubjectID('')
                setBindingRoles(['use'])
              }
            }}>
              <option value="team">Team</option>
              <option value="user">User</option>
              <option value="all">All users and teams</option>
            </select>
            <fieldset className="flex gap-4 rounded-lg border border-gray-300 px-3 py-2 dark:border-gray-700">
              <legend className="px-1 text-xs text-gray-500 dark:text-gray-400">Roles</legend>
              {(['use', 'manage'] as const).map((role) => <label key={role} className="flex items-center gap-2 text-sm text-gray-700 dark:text-gray-300">
                <input
                  type="checkbox"
                  checked={bindingRoles.includes(role)}
                  disabled={role === 'manage' && subjectType === 'all'}
                  onChange={(event) => setBindingRoles((current) => event.target.checked
                    ? [...current, role]
                    : current.filter((item) => item !== role))}
                />
                {role === 'use' ? 'Use' : 'Manage'}
              </label>)}
            </fieldset>
            <input required={subjectType !== 'all'} disabled={subjectType === 'all'} className={input} value={subjectID} onChange={(event) => setSubjectID(event.target.value)} placeholder={subjectType === 'all' ? 'Subject ID は不要です' : subjectType === 'team' ? 'org/team' : 'user-id'} />
            <div className="grid grid-cols-2 gap-3">
              <label className="text-xs text-gray-500 dark:text-gray-400">Priority<input type="number" className={input} value={bindingPriority} onChange={(event) => setBindingPriority(Number(event.target.value))} /></label>
              <label className="text-xs text-gray-500 dark:text-gray-400">Max concurrent<input min={0} type="number" className={input} value={maxConcurrent} onChange={(event) => setMaxConcurrent(Number(event.target.value))} /></label>
            </div>
            <button disabled={!bindingPool || bindingRoles.length === 0} className={stepButton}>Binding を追加</button>
          </form>
        </div>
      </SettingsSubsection>

      <SettingsSubsection title="Session Managers">
        <ItemList>
          {managers.length === 0 && <ItemListEmpty>Session Manager はまだありません</ItemListEmpty>}
          {managers.map((manager) => (
            (() => {
              const runtime = runtimeStatuses.find((item) => item.manager.id === manager.id)
              const runnerIDs = runtime?.status?.running_runner_ids || []
              return <ItemListRow
                key={manager.id}
                name={manager.name}
                meta={`${manager.id}${runtime?.status?.version ? ` · ${runtime.status.version}` : ''}`}
                badges={<>
                  <StatusBadge tone={runtime?.online ? 'green' : 'amber'}>{runtime ? (runtime.online ? 'Online' : 'Offline') : '未割り当て'}</StatusBadge>
                  {runtime?.pools.map((pool) => <StatusBadge key={pool} tone="blue">{pool}</StatusBadge>)}
                  {runtime?.status && <StatusBadge tone="violet">{runtime.status.used_runners || 0}/{runtime.status.running_runners || 0} used</StatusBadge>}
                </>}
                actions={<>
                  <RowAction onClick={() => void showLogs('manager', manager.id)} disabled={!runtime?.online}>ログ</RowAction>
                  <RowAction tone="danger" onClick={() => void removeManager(manager)} title={`${manager.name}を削除`}>削除</RowAction>
                </>}
              >
                {runtime?.error && <p className="mt-2 text-xs text-amber-600 dark:text-amber-400">{runtime.error}</p>}
                {runnerIDs.length > 0 && <div className="mt-3 flex flex-wrap items-center gap-2">
                  <span className="text-xs font-semibold text-gray-500 dark:text-gray-400">Runners</span>
                  {runnerIDs.map((runnerID) => <button key={runnerID} type="button" onClick={() => void showLogs('runner', runnerID)} className="inline-flex items-center gap-1 rounded-md bg-gray-100 px-2 py-1 font-mono text-xs text-gray-700 hover:bg-gray-200 dark:bg-gray-800 dark:text-gray-200 dark:hover:bg-gray-700"><Terminal className="h-3 w-3" />{runnerID}</button>)}
                </div>}
                {logTarget === `manager:${manager.id}` && <LogPanel target={manager.name} result={logResult} onClose={() => { setLogTarget(null); setLogResult(null) }} />}
                {runnerIDs.some((runnerID) => logTarget === `runner:${runnerID}`) && <LogPanel target={logTarget?.slice('runner:'.length) || ''} result={logResult} onClose={() => { setLogTarget(null); setLogResult(null) }} />}
              </ItemListRow>
            })()
          ))}
        </ItemList>
      </SettingsSubsection>

      <SettingsSubsection title="Logical Pools" description="Pool ごとの供給元と利用権限">
        {pools.length === 0 && <ItemList><ItemListEmpty>Logical Pool はまだありません</ItemListEmpty></ItemList>}
        {pools.length > 0 && <div className="space-y-4">
          {pools.map((pool) => {
            const poolSuppliers = suppliers.filter((supplier) => supplier.pool === pool.name)
            const poolBindings = bindings[pool.name] || []
            return (
              <section key={pool.name} className="rounded-2xl border border-gray-200 bg-white p-5 shadow-sm dark:border-gray-800 dark:bg-gray-900">
                <div className="flex flex-col gap-4 border-b border-gray-100 pb-4 dark:border-gray-800 sm:flex-row sm:items-start sm:justify-between">
                  <div className="min-w-0 space-y-3">
                    <div className="flex flex-wrap items-center gap-2">
                      <h3 className="text-lg font-semibold text-gray-950 dark:text-white">{pool.name}</h3>
                      <StatusBadge tone={pool.enabled ? 'green' : 'amber'}>{pool.enabled ? 'Enabled' : 'Disabled'}</StatusBadge>
                    </div>
                    <div className="flex flex-wrap gap-1.5">{Object.entries(pool.labels ?? {}).map(([key, value]) => <code key={key} className="rounded-md bg-gray-100 px-2 py-1 text-[11px] text-gray-700 dark:bg-gray-800 dark:text-gray-300">{key}={value}</code>)}{Object.keys(pool.labels ?? {}).length === 0 && <span className="text-xs text-gray-400">Labelsなし</span>}</div>
                  </div>
                  <div className="flex shrink-0 flex-wrap items-center gap-3"><PoolSettingsEditor pool={pool} onSave={(patch) => patchPool(pool, patch)} /><button type="button" onClick={() => void togglePool(pool)} className="text-xs text-blue-700 dark:text-blue-300">{pool.enabled ? '停止' : '有効化'}</button><button type="button" onClick={() => void removePool(pool)} className="text-xs text-red-600 dark:text-red-400">削除</button></div>
                </div>

                <div className="mt-5 grid gap-5 lg:grid-cols-2">
                  <div>
                    <div className="mb-3 flex items-center justify-between"><h4 className="text-sm font-semibold text-gray-800 dark:text-gray-200">Suppliers</h4><span className="text-xs text-gray-400">{poolSuppliers.length}件</span></div>
                    {poolSuppliers.length === 0 && <div className="rounded-xl border border-dashed border-gray-200 px-4 py-6 text-center text-sm text-gray-400 dark:border-gray-700">未設定</div>}
                    {poolSuppliers.length > 0 && <div className="overflow-visible rounded-xl border border-gray-200 dark:border-gray-700"><table className="w-full table-fixed text-left text-sm"><thead className="bg-gray-50 text-[11px] uppercase tracking-wide text-gray-500 dark:bg-gray-950/60"><tr><th className="px-3 py-2 font-medium">Manager</th><th className="w-20 px-3 py-2 font-medium">状態</th><th className="w-28 px-3 py-2 font-medium">Capacity</th><th className="w-10 px-2 py-2"><span className="sr-only">操作</span></th></tr></thead><tbody className="divide-y divide-gray-200 dark:divide-gray-800">
                    {poolSuppliers.map((supplier) => (
                      <tr key={supplier.manager_id}><td className="max-w-48 px-3 py-3 font-medium text-gray-900 dark:text-white"><span className="break-words">{managers.find((manager) => manager.id === supplier.manager_id)?.name || supplier.manager_id}</span></td><td className="px-3 py-3"><StatusBadge tone={supplier.enabled && !supplier.draining ? 'green' : 'amber'}>{supplier.enabled && !supplier.draining ? '有効' : '無効'}</StatusBadge></td><td className="whitespace-nowrap px-3 py-3 text-xs text-gray-500">{supplier.min_idle ?? 0} min / {supplier.max_runners || '∞'} max</td><td className="px-2 py-2 text-right"><RowActionsMenu label={`${supplier.pool}のSupplier操作`}><SupplierSettingsEditor supplier={supplier} onSave={(patch) => patchSupplier(supplier, patch)} triggerClassName={menuItemClass} /><button type="button" className={menuItemClass} onClick={() => void patchSupplier(supplier, { enabled: !(supplier.enabled && !supplier.draining), draining: false }).catch(() => undefined)}>{supplier.enabled && !supplier.draining ? '無効化' : '有効化'}</button><button type="button" className={`${menuItemClass} text-red-600 dark:text-red-400`} onClick={() => void removeSupplier(supplier)}>削除</button></RowActionsMenu></td></tr>
                    ))}</tbody></table></div>}
                  </div>

                  <div>
                    <div className="mb-3 flex items-center justify-between"><h4 className="text-sm font-semibold text-gray-800 dark:text-gray-200">Bindings</h4><span className="text-xs text-gray-400">{poolBindings.length}件</span></div>
                    {poolBindings.length === 0 && <div className="rounded-xl border border-dashed border-gray-200 px-4 py-6 text-center text-sm text-gray-400 dark:border-gray-700">未設定</div>}
                    {poolBindings.length > 0 && <div className="overflow-visible rounded-xl border border-gray-200 dark:border-gray-700"><table className="w-full table-fixed text-left text-sm"><thead className="bg-gray-50 text-[11px] uppercase tracking-wide text-gray-500 dark:bg-gray-950/60"><tr><th className="px-3 py-2 font-medium">Subject</th><th className="w-24 px-3 py-2 font-medium">Roles</th><th className="w-20 px-3 py-2 font-medium">Limit</th><th className="w-10 px-2 py-2"><span className="sr-only">操作</span></th></tr></thead><tbody className="divide-y divide-gray-200 dark:divide-gray-800">
                      {poolBindings.map((binding) => (
                        <tr key={binding.id}><td className="max-w-48 px-3 py-3 font-medium text-gray-900 dark:text-white"><span className="break-words">{binding.subject_type}: {binding.subject_id || 'everyone'}</span></td><td className="px-3 py-3 text-xs text-gray-600 dark:text-gray-300">{binding.roles?.join(', ') || binding.role}</td><td className="whitespace-nowrap px-3 py-3 text-xs text-gray-500">{binding.max_concurrent ? `${binding.max_concurrent} max` : '無制限'}</td><td className="px-2 py-2 text-right"><RowActionsMenu label={`${binding.subject_id || 'everyone'}のBinding操作`}><BindingSettingsEditor binding={binding} onSave={(patch) => patchBinding(binding, patch)} triggerClassName={menuItemClass} /><button type="button" className={`${menuItemClass} text-red-600 dark:text-red-400`} onClick={() => void removeBinding(pool.name, binding.id)}>削除</button></RowActionsMenu></td></tr>
                      ))}
                    </tbody></table></div>}
                  </div>
                </div>
              </section>
            )
          })}
        </div>}
      </SettingsSubsection>
    </>
  )
}
