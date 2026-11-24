'use client'

import { useCallback, useEffect, useState } from 'react'
import { AgentAPIProxyClient, AgentAPIProxyError } from '../../lib/agentapi-proxy-client'
import { SandboxPolicy, UpdateSandboxPolicyRequest } from '../../types/sandbox_policy'

export function useSandboxDomainsDialog(sessionId: string, agentAPI: AgentAPIProxyClient, isOpen: boolean) {
  const domains = useSandboxDomainData(sessionId, agentAPI, isOpen)
  const selection = useSandboxDomainSelection(agentAPI, domains.policies, domains.setError)
  const [activeTab, setActiveTab] = useState<'denied' | 'allowed'>('denied')
  return {
    ...domains,
    ...selection,
    activeTab,
    setActiveTab,
    currentDomains: activeTab === 'denied' ? domains.denied : domains.allowed,
  }
}

function useSandboxDomainData(sessionId: string, agentAPI: AgentAPIProxyClient, isOpen: boolean) {
  const [isLoading, setIsLoading] = useState(false)
  const [allowed, setAllowed] = useState<string[]>([])
  const [denied, setDenied] = useState<string[]>([])
  const [error, setError] = useState<string | null>(null)
  const [policies, setPolicies] = useState<SandboxPolicy[]>([])
  const fetchDomains = useCallback(async () => {
    setIsLoading(true); setError(null)
    try {
      const result = await agentAPI.getSessionSandboxDomains(sessionId)
      setAllowed(result.allowed ?? []); setDenied(result.denied ?? [])
    } catch (reason) { setError(domainLoadError(reason)) }
    finally { setIsLoading(false) }
  }, [agentAPI, sessionId])
  const fetchPolicies = useCallback(async () => {
    try { setPolicies((await agentAPI.getSandboxPolicies()).sandbox_policies ?? []) }
    catch (reason) { console.debug('Sandbox policies are unavailable for this optional flow:', reason) }
  }, [agentAPI])
  useEffect(() => { if (isOpen) { void fetchDomains(); void fetchPolicies() } }, [isOpen, fetchDomains, fetchPolicies])
  return { isLoading, allowed, denied, error, setError, policies, fetchDomains }
}

function domainLoadError(reason: unknown): string {
  if (reason instanceof AgentAPIProxyError && reason.status === 503) return 'このセッションにはネットワークフィルター（サンドボックス）が設定されていません'
  if (reason instanceof AgentAPIProxyError && reason.status === 501) return 'このセッションタイプではサンドボックスドメインの取得はサポートされていません'
  return 'ドメインリストの取得に失敗しました'
}

function useSandboxDomainSelection(agentAPI: AgentAPIProxyClient, policies: SandboxPolicy[], setError: (value: string | null) => void) {
  const [selectedPolicyId, setSelectedPolicyId] = useState('')
  const [selectedDomains, setSelectedDomains] = useState<Set<string>>(new Set())
  const [addSuccess, setAddSuccess] = useState<string | null>(null)
  const [isAdding, setIsAdding] = useState(false)
  useEffect(() => { if (!selectedPolicyId && policies.length > 0) setSelectedPolicyId(policies[0].id) }, [policies, selectedPolicyId])
  const toggleDomain = (domain: string) => setSelectedDomains((previous) => toggleSetValue(previous, domain))
  const addToPolicy = async () => {
    if (!selectedPolicyId || selectedDomains.size === 0) return
    setIsAdding(true); setAddSuccess(null); setError(null)
    try {
      const policy = policies.find((candidate) => candidate.id === selectedPolicyId)
      if (!policy) throw new Error('ポリシーが見つかりません')
      const selected = Array.from(selectedDomains)
      await agentAPI.updateSandboxPolicy(selectedPolicyId, policyUpdate(policy, selected))
      setAddSuccess(`${selected.length} 件のドメインをポリシー「${policy.name}」に追加しました`)
      setSelectedDomains(new Set())
    } catch (reason) { setError(reason instanceof AgentAPIProxyError ? reason.message : 'ポリシーの更新に失敗しました') }
    finally { setIsAdding(false) }
  }
  return { selectedPolicyId, setSelectedPolicyId, selectedDomains, addSuccess, isAdding, toggleDomain, addToPolicy }
}

function toggleSetValue(previous: Set<string>, value: string): Set<string> {
  const next = new Set(previous)
  if (next.has(value)) next.delete(value); else next.add(value)
  return next
}

function policyUpdate(policy: SandboxPolicy, selected: string[]): UpdateSandboxPolicyRequest {
  const isAllowlist = (policy.allowed_domains?.length ?? 0) > 0 || (policy.denied_domains?.length ?? 0) === 0
  if (isAllowlist) return { allowed_domains: [...(policy.allowed_domains ?? []), ...selected.filter((domain) => !policy.allowed_domains?.includes(domain))] }
  return { denied_domains: (policy.denied_domains ?? []).filter((domain) => !selected.includes(domain)) }
}

export type SandboxDomainsDialogState = ReturnType<typeof useSandboxDomainsDialog>
