'use client'

import { Boxes, Plus, RefreshCw, Trash2, UserPlus, Users, X } from 'lucide-react'
import { ESMRegistrationToken, SettingsPageHeader, SettingsSubsection } from '@/components/settings'
import { ExternalSessionManagerList } from '@/components/settings/ExternalSessionManagerList'
import { PoolSupplierControls } from '@/components/settings/PoolSupplierControls'
import { BindingSettingsEditor, PoolSettingsEditor, SupplierSettingsEditor } from '@/components/settings/PoolResourceEditors'
import { heartbeatOnline, usePoolsSection } from './usePoolsSection'

const card = 'rounded-xl border border-gray-200 bg-white p-4 shadow-sm dark:border-gray-800 dark:bg-gray-900'

export function PoolsSection({ showHeader = true }: { showHeader?: boolean }) {
  const {
    activeSessions, activeTab, assignManager, assignManagerID, assigningPool, busyAction,
    createPool, deletePool, error, loading, managerByID, managers, newPoolName,
    onlineManagers, patchBinding, patchPool, patchSupplier, regenerateEsmToken,
    regeneratingEsmId, reload, revealedTokens, runtimes, scopeId, scopeKind,
    setActiveTab, setAssignManagerID, setAssigningPool, setNewPoolName,
    setShowCreatePool, setShowRegistration, showCreatePool, showRegistration, tabs,
    togglePool, togglePoolUsage, totalCapacity, unassignManager, update,
  } = usePoolsSection()

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

        {runtimes.map((runtime) => {
          const { pool, suppliers, bindings, scopeBinding } = runtime
          const canManage = (scopeBinding.role === 'manage' || scopeBinding.role === 'manage_and_use') && scopeBinding.enabled
          const canUse = (scopeBinding.role === 'use' || scopeBinding.role === 'manage_and_use') && scopeBinding.enabled
          const idle = suppliers.reduce((sum, supplier) => sum + (supplier.idle_runners ?? 0), 0)
          const runners = suppliers.reduce((sum, supplier) => sum + (supplier.total_runners ?? 0), 0)
          return <section key={pool.name} className="rounded-xl border border-gray-200 bg-white p-4 shadow-sm dark:border-gray-800 dark:bg-gray-900"><div className="flex flex-wrap items-start justify-between gap-3"><div><div className="flex flex-wrap items-center gap-2"><h3 className="font-semibold text-gray-950 dark:text-white">{pool.name}</h3><span className={`rounded-full px-2 py-1 text-xs font-medium ${canUse && pool.enabled ? 'bg-emerald-100 text-emerald-700 dark:bg-emerald-950 dark:text-emerald-300' : 'bg-gray-100 text-gray-600 dark:bg-gray-800 dark:text-gray-300'}`}>{canUse && pool.enabled ? '使用中' : '停止中'}</span><span className="text-xs text-gray-500">{scopeBinding.role}</span></div><div className="mt-2 flex flex-wrap gap-1">{Object.entries(pool.labels ?? {}).map(([key, value]) => <code key={key} className="rounded bg-gray-100 px-1.5 py-0.5 text-[11px] dark:bg-gray-800">{key}={value}</code>)}{Object.keys(pool.labels ?? {}).length === 0 && <span className="text-xs text-gray-400">Labelsなし</span>}</div></div><div className="flex flex-wrap gap-1.5">{canManage && <PoolSettingsEditor pool={pool} onSave={(patch) => patchPool(pool, patch)} />}{scopeBinding.role === 'use' && <button type="button" onClick={() => void togglePoolUsage(runtime)} disabled={busyAction === `usage:${pool.name}` || !pool.enabled} className="rounded-md border border-blue-300 px-2.5 py-1.5 text-xs text-blue-700 disabled:opacity-50 dark:border-blue-800 dark:text-blue-300">{scopeBinding.enabled ? '使用停止' : '使用'}</button>}{canManage && <button type="button" onClick={() => void togglePool(pool)} className="rounded-md border border-gray-300 px-2.5 py-1.5 text-xs text-gray-700 dark:border-gray-700 dark:text-gray-200">{pool.enabled ? '停止' : '有効化'}</button>}{canManage && <button type="button" aria-label={`${pool.name}を削除`} onClick={() => void deletePool(pool)} disabled={busyAction === `delete:${pool.name}`} className="rounded-md border border-red-200 p-1.5 text-red-600 disabled:opacity-50 dark:border-red-900 dark:text-red-400"><Trash2 className="h-3.5 w-3.5" /></button>}</div></div>{canManage && <dl className="mt-4 grid grid-cols-2 gap-2 sm:grid-cols-4">{[['Suppliers', suppliers.length], ['Runners', runners], ['Idle', idle], ['Bindings', bindings.length]].map(([label, value]) => <div key={label} className="rounded-lg bg-gray-50 px-3 py-2 dark:bg-gray-950"><dt className="text-xs text-gray-500">{label}</dt><dd className="mt-1 font-semibold tabular-nums text-gray-950 dark:text-white">{value}</dd></div>)}</dl>}</section>
        })}

      </div>}

      {activeTab === 'assignments' && <div className="space-y-4">
        <div><h2 className="text-base font-semibold text-gray-950 dark:text-white">割り当て設定</h2><p className="mt-1 text-sm text-gray-500">ManagerをPoolへ割り当て、供給の有効・無効を管理します。</p></div>
        {runtimes.length === 0 && <div className={`${card} py-8 text-center text-sm text-gray-500`}>先にPoolを作成してください。</div>}
        {runtimes.filter(({ scopeBinding }) => (scopeBinding.role === 'manage' || scopeBinding.role === 'manage_and_use') && scopeBinding.enabled).map(({ pool, suppliers, bindings }) => <section key={pool.name} className="overflow-hidden rounded-xl border border-gray-200 bg-white dark:border-gray-800 dark:bg-gray-900">
          <div className="flex flex-wrap items-center justify-between gap-3 border-b border-gray-200 px-4 py-3 dark:border-gray-800"><h3 className="break-all font-semibold text-gray-950 dark:text-white">{pool.name}</h3><button type="button" onClick={() => { setAssigningPool(assigningPool === pool.name ? null : pool.name); setAssignManagerID('') }} className="inline-flex items-center gap-1 rounded-md border border-blue-300 px-2.5 py-1.5 text-xs font-medium text-blue-700 dark:border-blue-800 dark:text-blue-300"><UserPlus className="h-3.5 w-3.5" /> Managerを割り当て</button></div>
          {assigningPool === pool.name && <div className="flex flex-col gap-2 border-b border-gray-200 bg-blue-50/50 p-3 sm:flex-row dark:border-gray-800 dark:bg-blue-950/20"><select value={assignManagerID} onChange={(event) => setAssignManagerID(event.target.value)} className="min-w-0 flex-1 rounded-md border border-gray-300 bg-white px-3 py-2 text-sm dark:border-gray-700 dark:bg-gray-950 dark:text-white"><option value="">Managerを選択</option>{managers.filter((manager) => manager.id && !suppliers.some((supplier) => supplier.manager_id === manager.id)).map((manager) => <option key={manager.id} value={manager.id}>{manager.name}</option>)}</select><button type="button" onClick={() => void assignManager(pool.name)} disabled={!assignManagerID || busyAction === `assign:${pool.name}`} className="rounded-md bg-blue-600 px-4 py-2 text-sm font-medium text-white disabled:opacity-50">割り当て</button></div>}
          <div className="grid gap-4 p-4 lg:grid-cols-2"><div><h4 className="mb-2 text-xs font-semibold uppercase tracking-wide text-gray-500">Suppliers</h4><div className="space-y-2">{suppliers.length === 0 && <div className="rounded-lg border border-dashed border-gray-300 p-4 text-center text-sm text-gray-500 dark:border-gray-700">Managerは割り当てられていません。</div>}{suppliers.map((supplier) => <div key={supplier.manager_id} className="rounded-lg border border-gray-200 p-3 dark:border-gray-800"><div className="flex items-start justify-between gap-3"><div><p className="text-sm font-medium text-gray-900 dark:text-white">{managerByID.get(supplier.manager_id)?.name ?? supplier.manager_id}</p><p className="mt-1 text-xs text-gray-500">min idle {supplier.min_idle ?? 0} · idle {supplier.idle_runners ?? 0}/{supplier.total_runners ?? 0} · max {supplier.max_runners || '∞'}</p></div><PoolSupplierControls supplier={supplier} onPatch={(patch) => { void patchSupplier(supplier, patch).catch(() => undefined) }} /></div><div className="mt-3 flex justify-end gap-2"><SupplierSettingsEditor supplier={supplier} onSave={(patch) => patchSupplier(supplier, patch)} /><button type="button" onClick={() => void unassignManager(supplier)} className="text-xs text-red-600 dark:text-red-400">解除</button></div></div>)}</div></div><div><h4 className="mb-2 text-xs font-semibold uppercase tracking-wide text-gray-500">Bindings</h4><div className="space-y-2">{bindings.length === 0 && <div className="rounded-lg border border-dashed border-gray-300 p-4 text-center text-sm text-gray-500 dark:border-gray-700">Bindingはありません。</div>}{bindings.map((binding) => <div key={binding.id} className="rounded-lg border border-gray-200 p-3 dark:border-gray-800"><div className="flex items-start justify-between gap-3"><div><p className="inline-flex items-center gap-1 text-sm font-medium text-gray-900 dark:text-white"><Users className="h-3.5 w-3.5" />{binding.subject_type}: {binding.subject_id || 'everyone'}</p><p className="mt-1 text-xs text-gray-500">{binding.roles?.join(', ') || binding.role} · priority {binding.priority ?? 0} · max {binding.max_concurrent || '∞'} · {binding.enabled ? '有効' : '無効'}</p></div><BindingSettingsEditor binding={binding} onSave={(patch) => patchBinding(binding, patch)} /></div></div>)}</div></div></div>
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
