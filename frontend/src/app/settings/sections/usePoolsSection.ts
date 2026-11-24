'use client'

import { useCallback, useEffect, useMemo, useState } from 'react'
import { createCurrentDeploymentAgentAPIProxyClient } from '@/lib/agentapi-proxy-client'
import type { LogicalSessionPool, SessionPoolBinding, SessionPoolSupplier } from '@/types/session_pool'
import type { ExternalSessionManagerConfig } from '@/types/settings'
import { useSettingsScope } from '../SettingsScopeContext'

export interface PoolRuntime {
  pool: LogicalSessionPool
  suppliers: SessionPoolSupplier[]
  bindings: SessionPoolBinding[]
  scopeBinding: SessionPoolBinding
}

export type PoolTab = 'managers' | 'pools' | 'assignments' | 'status'

export const heartbeatOnline = (manager: ExternalSessionManagerConfig) =>
  Boolean(manager.last_heartbeat_at && Date.now() - new Date(manager.last_heartbeat_at).getTime() < 45_000)

export function usePoolsSection() {
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

  return {
    activeSessions, activeTab, assignManager, assignManagerID, assigningPool, busyAction,
    createPool, deletePool, error, loading, managerByID, managers, newPoolName,
    onlineManagers, patchBinding, patchPool, patchSupplier, regenerateEsmToken,
    regeneratingEsmId, reload, revealedTokens, runtimes, scopeId, scopeKind,
    setActiveTab, setAssignManagerID, setAssigningPool, setNewPoolName,
    setShowCreatePool, setShowRegistration, showCreatePool, showRegistration, tabs,
    togglePool, togglePoolUsage, totalCapacity, unassignManager, update,
  }
}
