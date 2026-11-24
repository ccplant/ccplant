'use client'
import { useEffect, useState } from 'react'
import { useTeamScope } from '../../contexts/TeamScopeContext'
import { createAgentAPIProxyClientFromStorage } from '../../lib/agentapi-proxy-client'
import { CreateSandboxPolicyRequest, UpdateSandboxPolicyRequest } from '../../types/sandbox_policy'
import type { SandboxPolicySettingsPanelProps } from './SandboxPanelTypes'
export function useSandboxPolicySettings({ onClose, onSuccess, editing }: SandboxPolicySettingsPanelProps) {
  const { getScopeParams } = useTeamScope()
  const [activeTab, setActiveTab] = useState<'basic' | 'domains' | 'evaluation'>('basic')
  const [name, setName] = useState('')
  const [description, setDescription] = useState('')
  const [mode, setMode] = useState<'allowlist' | 'denylist'>('allowlist')
  const [domains, setDomains] = useState('')
  const [countMode, setCountMode] = useState(false)
  const [isSubmitting, setIsSubmitting] = useState(false)
  const [error, setError] = useState<string | null>(null)

  useEffect(() => {
    if (editing) {
      setName(editing.name)
      setDescription(editing.description ?? '')
      if (editing.allowed_domains && editing.allowed_domains.length > 0) {
        setMode('allowlist')
        setDomains(editing.allowed_domains.join('\n'))
      } else if (editing.denied_domains && editing.denied_domains.length > 0) {
        setMode('denylist')
        setDomains(editing.denied_domains.join('\n'))
      } else {
        setMode('allowlist')
        setDomains('')
      }
      setCountMode(editing.count_mode ?? false)
    } else {
      setName('')
      setDescription('')
      setMode('allowlist')
      setDomains('')
      setCountMode(false)
    }
    setError(null)
    setActiveTab('basic')
  }, [editing])

  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault()
    if (!name.trim()) {
      setError('名前は必須です')
      return
    }
    setIsSubmitting(true)
    try {
      const client = createAgentAPIProxyClientFromStorage()
      const domainList = domains
        .split('\n')
        .map((d) => d.trim())
        .filter(Boolean)
      if (editing) {
        const data: UpdateSandboxPolicyRequest = {
          name: name.trim(),
          description: description.trim() || undefined,
          ...(mode === 'allowlist'
            ? { allowed_domains: domainList, denied_domains: [] }
            : { denied_domains: domainList, allowed_domains: [] }),
          count_mode: countMode,
        }
        await client.updateSandboxPolicy(editing.id, data)
      } else {
        const scopeParams = getScopeParams()
        const data: CreateSandboxPolicyRequest = {
          name: name.trim(),
          description: description.trim() || undefined,
          ...(mode === 'allowlist' ? { allowed_domains: domainList } : { denied_domains: domainList }),
          count_mode: countMode,
          scope: (scopeParams as { scope?: 'user' | 'team' }).scope ?? 'user',
          ...((scopeParams as { team_id?: string }).team_id
            ? { team_id: (scopeParams as { team_id?: string }).team_id }
            : {}),
        }
        await client.createSandboxPolicy(data)
      }
      onSuccess()
    } catch (err) {
      setError(err instanceof Error ? err.message : '保存に失敗しました')
    } finally {
      setIsSubmitting(false)
    }
  }

  const tabs = [
    {
      id: 'basic' as const,
      label: '基本設定',
      description: '名前と説明',
    },
    {
      id: 'domains' as const,
      label: 'ドメイン',
      description: mode === 'allowlist' ? '許可リスト' : '拒否リスト',
    },
    {
      id: 'evaluation' as const,
      label: '評価',
      description: countMode ? 'カウントのみ' : '通常適用',
    },
  ]

  return {
    onClose,
    editing,
    activeTab,
    setActiveTab,
    name,
    setName,
    description,
    setDescription,
    mode,
    setMode,
    domains,
    setDomains,
    countMode,
    setCountMode,
    isSubmitting,
    error,
    handleSubmit,
    tabs,
  }
}
export type SandboxPolicySettingsState = ReturnType<typeof useSandboxPolicySettings>
