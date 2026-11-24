'use client'
import { useCallback, useEffect, useState } from 'react'
import { createAgentAPIProxyClientFromStorage } from '../../lib/agentapi-proxy-client'
import { SandboxPolicy } from '../../types/sandbox_policy'
export function useSandboxPolicies() {
  const [policies, setPolicies] = useState<SandboxPolicy[]>([])
  const [loading, setLoading] = useState(true)
  const [isFormOpen, setIsFormOpen] = useState(false)
  const [editing, setEditing] = useState<SandboxPolicy | null>(null)
  const [importTarget, setImportTarget] = useState<SandboxPolicy | null>(null)
  const [sidebarVisible, setSidebarVisible] = useState(false)

  const load = useCallback(async () => {
    setLoading(true)
    try {
      const client = createAgentAPIProxyClientFromStorage()
      const res = await client.getSandboxPolicies()
      setPolicies(res.sandbox_policies || [])
    } catch {
      setPolicies([])
    } finally {
      setLoading(false)
    }
  }, [])

  useEffect(() => {
    load()
  }, [load])

  const handleDelete = async (policy: SandboxPolicy) => {
    if (!confirm(`「${policy.name}」を削除しますか？`)) return
    try {
      const client = createAgentAPIProxyClientFromStorage()
      await client.deleteSandboxPolicy(policy.id)
      await load()
    } catch (err) {
      alert(err instanceof Error ? err.message : '削除に失敗しました')
    }
  }

  return {
    policies,
    loading,
    isFormOpen,
    setIsFormOpen,
    editing,
    setEditing,
    importTarget,
    setImportTarget,
    sidebarVisible,
    setSidebarVisible,
    load,
    handleDelete,
  }
}
export type SandboxPoliciesState = ReturnType<typeof useSandboxPolicies>
