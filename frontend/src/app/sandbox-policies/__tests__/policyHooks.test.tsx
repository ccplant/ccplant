import { act, cleanup, renderHook, waitFor } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import type { SandboxPolicy } from '../../../types/sandbox_policy'
import { useSessionDomainImport } from '../useSessionDomainImport'
import { useSandboxPolicySettings } from '../useSandboxPolicySettings'

const mocks = vi.hoisted(() => ({ domains: vi.fn(), update: vi.fn(), ignore: vi.fn(), create: vi.fn() }))
vi.mock('../../../lib/agentapi-proxy-client', () => ({
  createAgentAPIProxyClientFromStorage: () => ({
    getSandboxPolicyDomains: mocks.domains,
    updateSandboxPolicy: mocks.update,
    updateIgnoredSandboxPolicyDomains: mocks.ignore,
    createSandboxPolicy: mocks.create,
  }),
  AgentAPIProxyError: class extends Error {},
}))
vi.mock('../../../contexts/TeamScopeContext', () => ({
  useTeamScope: () => ({ getScopeParams: () => ({ scope: 'team', team_id: 'team-1' }) }),
}))

const policy: SandboxPolicy = {
  id: 'policy-1',
  name: 'Policy',
  allowed_domains: ['base.test'],
  scope: 'team',
  owner_id: 'user-1',
  created_at: '2026-11-23T00:00:00Z',
  updated_at: '2026-11-23T00:00:00Z',
}

describe('sandbox policy hooks', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    mocks.domains.mockResolvedValue({ allowed: [], denied: ['new.test'], ignored: [] })
    mocks.update.mockResolvedValue({})
    mocks.ignore.mockResolvedValue({})
    mocks.create.mockResolvedValue({})
  })
  afterEach(cleanup)

  it('adds selected domains without dropping existing allowlist entries', async () => {
    const onImported = vi.fn()
    const { result } = renderHook(() => useSessionDomainImport({ policy, onClose: vi.fn(), onImported }))
    await waitFor(() => expect(result.current.loading).toBe(false))
    act(() => result.current.toggleDomain('new.test'))
    await act(async () => result.current.handleAddToPolicy())
    expect(mocks.update).toHaveBeenCalledWith('policy-1', { allowed_domains: ['base.test', 'new.test'] })
    expect(result.current.selectedDomains.size).toBe(0)
    expect(result.current.currentDomains).toEqual([])
    expect(onImported).toHaveBeenCalledOnce()
  })

  it('reverts a failed optimistic domain hide and reports the error', async () => {
    mocks.ignore.mockRejectedValueOnce(new Error('offline'))
    const { result } = renderHook(() => useSessionDomainImport({ policy, onClose: vi.fn(), onImported: vi.fn() }))
    await waitFor(() => expect(result.current.loading).toBe(false))
    await act(async () => result.current.handleHideDomain('new.test'))
    expect(result.current.currentDomains).toEqual(['new.test'])
    expect(result.current.error).toBe('非表示リストの更新に失敗しました')
  })

  it('clears the opposite domain mode when editing a policy', async () => {
    const onSuccess = vi.fn()
    const { result } = renderHook(() => useSandboxPolicySettings({ editing: policy, onClose: vi.fn(), onSuccess }))
    expect(result.current.domains).toBe('base.test')
    act(() => {
      result.current.setMode('denylist')
      result.current.setDomains(' blocked.test\n\n second.test ')
    })
    await act(async () => result.current.handleSubmit({ preventDefault: vi.fn() } as unknown as React.FormEvent))
    expect(mocks.update).toHaveBeenCalledWith(
      'policy-1',
      expect.objectContaining({ allowed_domains: [], denied_domains: ['blocked.test', 'second.test'] })
    )
    expect(onSuccess).toHaveBeenCalledOnce()
  })

  it('creates a policy with the selected team scope', async () => {
    const { result } = renderHook(() =>
      useSandboxPolicySettings({ editing: null, onClose: vi.fn(), onSuccess: vi.fn() })
    )
    act(() => {
      result.current.setName(' New policy ')
      result.current.setCountMode(true)
    })
    await act(async () => result.current.handleSubmit({ preventDefault: vi.fn() } as unknown as React.FormEvent))
    expect(mocks.create).toHaveBeenCalledWith(
      expect.objectContaining({ name: 'New policy', scope: 'team', team_id: 'team-1', count_mode: true })
    )
  })
})
