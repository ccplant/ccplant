'use client'

import { FormEvent, useCallback, useEffect, useMemo, useState } from 'react'
import TopBar from '../components/TopBar'
import NavigationTabs from '../components/NavigationTabs'
import { useTeamScope } from '../../contexts/TeamScopeContext'
import { createCurrentDeploymentAgentAPIProxyClient } from '../../lib/agentapi-proxy-client'
import { LogicalSessionPool, SessionPoolBinding } from '../../types/session_pool'

const inputClass = 'rounded border border-gray-300 bg-white px-3 py-2 text-sm text-gray-900 dark:border-gray-600 dark:bg-gray-800 dark:text-white'

export default function SessionPoolsPage() {
  const client = useMemo(() => createCurrentDeploymentAgentAPIProxyClient(), [])
  const { selectedTeam } = useTeamScope()
  const [pools, setPools] = useState<LogicalSessionPool[]>([])
  const [bindings, setBindings] = useState<Record<string, SessionPoolBinding[]>>({})
  const [name, setName] = useState('')
  const [targetPool, setTargetPool] = useState('')
  const [subjectType, setSubjectType] = useState<'user' | 'team' | 'all'>('user')
  const [subjectID, setSubjectID] = useState('')
  const [role, setRole] = useState<'use' | 'manage'>('use')
  const [error, setError] = useState('')

  const reload = useCallback(async () => {
    try {
      const nextPools = await client.listManagedSessionPools()
      const nextBindings = await Promise.all(nextPools.map(async pool => [pool.name, await client.listManagedSessionPoolBindings(pool.name)] as const))
      setPools(nextPools)
      setBindings(Object.fromEntries(nextBindings))
      setTargetPool(current => current || nextPools[0]?.name || '')
      setError('')
    } catch (reason) {
      setError(reason instanceof Error ? reason.message : 'Poolの読み込みに失敗しました')
    }
  }, [client])

  useEffect(() => { void reload() }, [reload])

  const createPool = async (event: FormEvent) => {
    event.preventDefault()
    try {
      const pool = await client.createManagedSessionPool({ name, team_id: selectedTeam || undefined })
      setName('')
      setTargetPool(pool.name)
      await reload()
    } catch (reason) {
      setError(reason instanceof Error ? reason.message : 'Poolの作成に失敗しました')
    }
  }

  const createBinding = async (event: FormEvent) => {
    event.preventDefault()
    try {
      await client.createManagedSessionPoolBinding(targetPool, subjectType, subjectID, role)
      setSubjectID('')
      await reload()
    } catch (reason) {
      setError(reason instanceof Error ? reason.message : 'Bindingの作成に失敗しました')
    }
  }

  const removeBinding = async (pool: string, binding: SessionPoolBinding) => {
    if (!window.confirm('このBindingを削除しますか？')) return
    try {
      await client.deleteManagedSessionPoolBinding(pool, binding.id)
      await reload()
    } catch (reason) {
      setError(reason instanceof Error ? reason.message : 'Bindingの削除に失敗しました')
    }
  }

  const removePool = async (pool: LogicalSessionPool) => {
    if (!window.confirm(`Pool「${pool.name}」と関連リソースを削除しますか？`)) return
    try {
      await client.deleteManagedSessionPool(pool.name)
      await reload()
    } catch (reason) {
      setError(reason instanceof Error ? reason.message : 'Poolの削除に失敗しました')
    }
  }

  return (
    <main className="min-h-dvh bg-gray-50 dark:bg-gray-900">
      <TopBar title="Session Pools" subtitle="Poolの利用者と管理者をBindingで設定します。">
        <div className="md:hidden"><NavigationTabs /></div>
      </TopBar>
      <div className="mx-auto max-w-5xl space-y-5 p-4 md:p-8">
        <NavigationTabs className="hidden md:block" />
        {error && <div className="rounded border border-red-300 bg-red-50 p-3 text-sm text-red-700">{error}</div>}
        <div className="grid gap-4 md:grid-cols-2">
          <form onSubmit={createPool} className="space-y-3 rounded-lg bg-white p-5 shadow dark:bg-gray-800">
            <h2 className="font-semibold">Poolを作成</h2>
            <p className="text-xs text-gray-500">{selectedTeam ? `${selectedTeam} のmanage Bindingを自動作成します。` : 'あなたのmanage Bindingを自動作成します。'}</p>
            <input required className={`${inputClass} w-full`} value={name} onChange={event => setName(event.target.value)} placeholder="pool-name" />
            <button className="rounded bg-blue-600 px-3 py-2 text-sm text-white">作成</button>
          </form>
          <form onSubmit={createBinding} className="space-y-3 rounded-lg bg-white p-5 shadow dark:bg-gray-800">
            <h2 className="font-semibold">Bindingを追加</h2>
            <select required className={`${inputClass} w-full`} value={targetPool} onChange={event => setTargetPool(event.target.value)}>
              <option value="" disabled>Poolを選択</option>
              {pools.map(pool => <option key={pool.name}>{pool.name}</option>)}
            </select>
            <div className="grid grid-cols-2 gap-2">
              <select className={inputClass} value={subjectType} onChange={event => {
                const next = event.target.value as 'user' | 'team' | 'all'
                setSubjectType(next)
                if (next === 'all') { setSubjectID(''); setRole('use') }
              }}><option value="user">User</option><option value="team">Team</option><option value="all">All</option></select>
              <select className={inputClass} value={role} onChange={event => setRole(event.target.value as 'use' | 'manage')}><option value="use">Use</option><option value="manage" disabled={subjectType === 'all'}>Manage</option></select>
            </div>
            <input required={subjectType !== 'all'} disabled={subjectType === 'all'} className={`${inputClass} w-full disabled:opacity-50`} value={subjectID} onChange={event => setSubjectID(event.target.value)} placeholder={subjectType === 'team' ? 'org/team' : subjectType === 'user' ? 'user-id' : '不要'} />
            <button disabled={!targetPool} className="rounded bg-blue-600 px-3 py-2 text-sm text-white disabled:opacity-50">追加</button>
          </form>
        </div>
        <div className="space-y-3">
          {pools.map(pool => <section key={pool.name} className="rounded-lg bg-white p-5 shadow dark:bg-gray-800">
            <div className="flex items-center justify-between"><h2 className="font-semibold">{pool.name}</h2><button className="text-sm text-red-600" onClick={() => void removePool(pool)}>削除</button></div>
            <div className="mt-3 flex flex-wrap gap-2">{(bindings[pool.name] || []).map(binding => <span key={binding.id} className="rounded bg-gray-100 px-3 py-2 text-sm dark:bg-gray-700">{binding.subject_type}:{binding.subject_id || 'everyone'} ({binding.role || 'use'}) <button className="ml-2 text-red-600" onClick={() => void removeBinding(pool.name, binding)}>×</button></span>)}</div>
          </section>)}
          {pools.length === 0 && <p className="text-sm text-gray-500">管理できるPoolはありません。</p>}
        </div>
      </div>
    </main>
  )
}
