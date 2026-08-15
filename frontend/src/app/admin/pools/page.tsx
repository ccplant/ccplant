'use client'

import { FormEvent, useCallback, useEffect, useState } from 'react'
import { createAgentAPIProxyClientFromStorage } from '@/lib/agentapi-proxy-client'
import { ClusterSessionManager, SessionPool, SessionPoolBinding } from '@/types/session_pool'

export default function SessionPoolsAdminPage() {
  const client = createAgentAPIProxyClientFromStorage()
  const [managers, setManagers] = useState<ClusterSessionManager[]>([])
  const [pools, setPools] = useState<SessionPool[]>([])
  const [bindings, setBindings] = useState<Record<string, SessionPoolBinding[]>>({})
  const [managerName, setManagerName] = useState('')
  const [managerID, setManagerID] = useState('')
  const [poolName, setPoolName] = useState('')
  const [subjectType, setSubjectType] = useState<'user' | 'team'>('team')
  const [subjectID, setSubjectID] = useState('')
  const [bindingPool, setBindingPool] = useState('')
  const [connectionToken, setConnectionToken] = useState('')
  const [error, setError] = useState('')

  const reload = useCallback(async () => {
    try {
      const [nextManagers, nextPools] = await Promise.all([client.listClusterSessionManagers(), client.listSessionPools()])
      setManagers(nextManagers); setPools(nextPools)
      if (!managerID && nextManagers[0]) setManagerID(nextManagers[0].id)
      const names = [...new Set(nextPools.map((pool) => pool.name))]
      if (!bindingPool && names[0]) setBindingPool(names[0])
      const rows = await Promise.all(names.map(async (name) => [name, await client.listSessionPoolBindings(name)] as const))
      setBindings(Object.fromEntries(rows)); setError('')
    } catch (reason) { setError(reason instanceof Error ? reason.message : '読み込みに失敗しました') }
  }, [bindingPool, client, managerID])

  useEffect(() => { void reload() }, []) // eslint-disable-line react-hooks/exhaustive-deps

  const addManager = async (event: FormEvent) => {
    event.preventDefault()
    try {
      const result = await client.createClusterSessionManager(managerName)
      setConnectionToken(result.connection_token); setManagerName(''); setManagerID(result.manager.id); await reload()
    } catch (reason) { setError(reason instanceof Error ? reason.message : '登録に失敗しました') }
  }
  const addPool = async (event: FormEvent) => {
    event.preventDefault()
    try { await client.createSessionPool(managerID, { name: poolName, min_idle: 1, max_runners: 10 }); setPoolName(''); setBindingPool(poolName); await reload() }
    catch (reason) { setError(reason instanceof Error ? reason.message : 'Pool作成に失敗しました') }
  }
  const addBinding = async (event: FormEvent) => {
    event.preventDefault()
    try { await client.createSessionPoolBinding(bindingPool, subjectType, subjectID); setSubjectID(''); await reload() }
    catch (reason) { setError(reason instanceof Error ? reason.message : 'Binding作成に失敗しました') }
  }

  const input = 'w-full rounded-md border border-gray-300 bg-white px-3 py-2 text-sm dark:border-gray-600 dark:bg-gray-900'
  return <div className="space-y-6">
    <div><h2 className="text-2xl font-bold">Session Pools</h2><p className="mt-1 text-sm text-gray-500">Managerはpoolの供給元です。利用権限は論理poolとuser/teamのPoolBindingで管理します。</p></div>
    {error && <div className="rounded-md bg-red-50 p-3 text-sm text-red-700">{error}</div>}
    {connectionToken && <div className="rounded-md border border-amber-300 bg-amber-50 p-4 text-sm"><strong>Connection token（再表示されません）</strong><code className="mt-2 block break-all">{connectionToken}</code></div>}
    <div className="grid gap-6 lg:grid-cols-3">
      <form onSubmit={addManager} className="space-y-3 rounded-lg border bg-white p-4 dark:bg-gray-800"><h3 className="font-semibold">1. Manager登録</h3><input required className={input} value={managerName} onChange={(e) => setManagerName(e.target.value)} placeholder="Tokyo Kubernetes" /><button className="rounded bg-blue-600 px-3 py-2 text-sm text-white">登録</button></form>
      <form onSubmit={addPool} className="space-y-3 rounded-lg border bg-white p-4 dark:bg-gray-800"><h3 className="font-semibold">2. Pool作成</h3><select required className={input} value={managerID} onChange={(e) => setManagerID(e.target.value)}>{managers.map((manager) => <option key={manager.id} value={manager.id}>{manager.name}</option>)}</select><input required className={input} value={poolName} onChange={(e) => setPoolName(e.target.value)} placeholder="linux-standard" /><button className="rounded bg-blue-600 px-3 py-2 text-sm text-white">作成</button></form>
      <form onSubmit={addBinding} className="space-y-3 rounded-lg border bg-white p-4 dark:bg-gray-800"><h3 className="font-semibold">3. PoolBinding</h3><select required className={input} value={bindingPool} onChange={(e) => setBindingPool(e.target.value)}>{[...new Set(pools.map((pool) => pool.name))].map((name) => <option key={name}>{name}</option>)}</select><select className={input} value={subjectType} onChange={(e) => setSubjectType(e.target.value as 'user' | 'team')}><option value="team">Team</option><option value="user">User</option></select><input required className={input} value={subjectID} onChange={(e) => setSubjectID(e.target.value)} placeholder={subjectType === 'team' ? 'org/team' : 'user-id'} /><button className="rounded bg-blue-600 px-3 py-2 text-sm text-white">Bind</button></form>
    </div>
    <div className="overflow-x-auto rounded-lg border bg-white dark:bg-gray-800"><table className="w-full text-left text-sm"><thead><tr className="border-b"><th className="p-3">Pool</th><th>Manager</th><th>Idle / Max</th><th>Bindings</th></tr></thead><tbody>{pools.map((pool) => <tr key={`${pool.manager_id}:${pool.name}`} className="border-b last:border-0"><td className="p-3 font-medium">{pool.name}</td><td>{managers.find((item) => item.id === pool.manager_id)?.name || pool.manager_id}</td><td>{pool.idle_runners || 0} / {pool.max_runners || '∞'}</td><td>{(bindings[pool.name] || []).map((binding) => <span key={binding.id} className="mr-2 inline-flex rounded bg-gray-100 px-2 py-1 text-xs">{binding.subject_type}: {binding.subject_id}<button className="ml-2 text-red-600" onClick={() => void client.deleteSessionPoolBinding(pool.name, binding.id).then(reload)}>×</button></span>)}</td></tr>)}</tbody></table></div>
  </div>
}
