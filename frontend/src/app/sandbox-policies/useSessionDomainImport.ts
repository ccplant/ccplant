'use client'
import { useCallback, useEffect, useState } from 'react'
import { AgentAPIProxyError, createAgentAPIProxyClientFromStorage } from '../../lib/agentapi-proxy-client'
import { UpdateSandboxPolicyRequest } from '../../types/sandbox_policy'
import type { SessionDomainImportPanelProps } from './SandboxPanelTypes'
export function useSessionDomainImport({ onClose, policy, onImported }: SessionDomainImportPanelProps) {
  const [domains, setDomains] = useState<{
    allowed: string[]
    denied: string[]
    ignored: string[]
    updated_at?: string
  } | null>(null)
  const [loading, setLoading] = useState(false)
  const [selectedDomains, setSelectedDomains] = useState<Set<string>>(new Set())
  // Track domains already registered in the policy (pre-existing + just added this session)
  const [registeredDomains, setRegisteredDomains] = useState<Set<string>>(new Set())
  // Track locally ignored domains (synced from server on fetch)
  const [ignoredDomains, setIgnoredDomains] = useState<Set<string>>(new Set())
  const [showHidden, setShowHidden] = useState(false)
  const [activeTab, setActiveTab] = useState<'denied' | 'allowed'>('denied')
  const [isSaving, setIsSaving] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const [success, setSuccess] = useState<string | null>(null)

  const fetchDomains = useCallback(async () => {
    setLoading(true)
    setDomains(null)
    setSelectedDomains(new Set())
    setError(null)
    setSuccess(null)
    try {
      const client = createAgentAPIProxyClientFromStorage()
      const result = await client.getSandboxPolicyDomains(policy.id)
      setDomains(result)
      setIgnoredDomains(new Set(result.ignored ?? []))
    } catch (err) {
      if (err instanceof AgentAPIProxyError && err.status === 501) {
        setError('ドメイン収集機能が利用できません')
      } else {
        setError('ドメインデータの取得に失敗しました')
      }
    } finally {
      setLoading(false)
    }
  }, [policy.id])

  useEffect(() => {
    setActiveTab('denied')
    setShowHidden(false)
    // Seed registered set from the policy's current domain list
    const isAllowlist = (policy.allowed_domains?.length ?? 0) > 0 || (policy.denied_domains?.length ?? 0) === 0
    setRegisteredDomains(new Set(isAllowlist ? (policy.allowed_domains ?? []) : (policy.denied_domains ?? [])))
    fetchDomains()
  }, [fetchDomains, policy.allowed_domains, policy.denied_domains])

  const toggleDomain = (domain: string) => {
    setSelectedDomains((prev) => {
      const next = new Set(prev)
      if (next.has(domain)) next.delete(domain)
      else next.add(domain)
      return next
    })
  }

  const handleAddToPolicy = async () => {
    if (selectedDomains.size === 0) return
    setIsSaving(true)
    setError(null)
    setSuccess(null)
    try {
      const client = createAgentAPIProxyClientFromStorage()
      const toAdd = Array.from(selectedDomains)
      const isAllowlist = (policy.allowed_domains?.length ?? 0) > 0 || (policy.denied_domains?.length ?? 0) === 0
      const update: UpdateSandboxPolicyRequest = isAllowlist
        ? { allowed_domains: [...new Set([...(policy.allowed_domains ?? []), ...toAdd])] }
        : { denied_domains: (policy.denied_domains ?? []).filter((d) => !toAdd.includes(d)) }
      await client.updateSandboxPolicy(policy.id, update)
      setSuccess(`${toAdd.length} 件のドメインをポリシーに追加しました`)
      setSelectedDomains(new Set())
      setRegisteredDomains((prev) => new Set([...prev, ...toAdd]))
      onImported()
    } catch (err) {
      setError(err instanceof AgentAPIProxyError ? err.message : 'ポリシーの更新に失敗しました')
    } finally {
      setIsSaving(false)
    }
  }

  const handleHideDomain = async (domain: string) => {
    const newIgnored = new Set(ignoredDomains)
    newIgnored.add(domain)
    setIgnoredDomains(newIgnored)
    setSelectedDomains((prev) => {
      const next = new Set(prev)
      next.delete(domain)
      return next
    })
    try {
      const client = createAgentAPIProxyClientFromStorage()
      await client.updateIgnoredSandboxPolicyDomains(policy.id, Array.from(newIgnored))
    } catch {
      setIgnoredDomains((prev) => {
        const reverted = new Set(prev)
        reverted.delete(domain)
        return reverted
      })
      setError('非表示リストの更新に失敗しました')
    }
  }

  const handleUnhideDomain = async (domain: string) => {
    const newIgnored = new Set(ignoredDomains)
    newIgnored.delete(domain)
    setIgnoredDomains(newIgnored)
    try {
      const client = createAgentAPIProxyClientFromStorage()
      await client.updateIgnoredSandboxPolicyDomains(policy.id, Array.from(newIgnored))
    } catch {
      setIgnoredDomains((prev) => new Set([...prev, domain]))
      setError('非表示リストの更新に失敗しました')
    }
  }

  const currentDomains = (activeTab === 'denied' ? (domains?.denied ?? []) : (domains?.allowed ?? [])).filter(
    (d) => !registeredDomains.has(d) && !ignoredDomains.has(d)
  )

  const ignoredDomainsForTab = (activeTab === 'denied' ? (domains?.denied ?? []) : (domains?.allowed ?? [])).filter(
    (d) => !registeredDomains.has(d) && ignoredDomains.has(d)
  )

  const tabs = [
    { id: 'denied' as const, label: 'ブロック済み', count: domains?.denied.length ?? 0, tone: 'red' },
    { id: 'allowed' as const, label: '許可済み', count: domains?.allowed.length ?? 0, tone: 'green' },
  ]

  return {
    onClose,
    policy,
    domains,
    loading,
    selectedDomains,
    showHidden,
    setShowHidden,
    activeTab,
    setActiveTab,
    isSaving,
    error,
    success,
    fetchDomains,
    toggleDomain,
    handleAddToPolicy,
    handleHideDomain,
    handleUnhideDomain,
    currentDomains,
    ignoredDomainsForTab,
    tabs,
  }
}
export type SessionDomainImportState = ReturnType<typeof useSessionDomainImport>
