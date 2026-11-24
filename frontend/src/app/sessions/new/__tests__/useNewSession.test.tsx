import { act, cleanup, renderHook, waitFor } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { useNewSession } from '../useNewSession'

const mocks = vi.hoisted(() => ({ submit: vi.fn(), back: vi.fn(), push: vi.fn() }))
vi.mock('next/navigation', () => ({ useRouter: () => ({ back: mocks.back, push: mocks.push }) }))
vi.mock('../../../../contexts/TeamScopeContext', () => ({ useTeamScope: () => ({ selectedTeam: 'team-1' }) }))
vi.mock('../newSessionSubmission', () => ({ submitNewSession: mocks.submit }))
vi.mock('../../../../lib/agentapi-proxy-client', () => ({
  createAgentAPIProxyClientFromStorage: () => ({
    getAvailableSessionPools: async () => [{ name: 'fly-dev' }],
    listGitHubIdentities: async () => ({ identities: [] }),
  }),
}))
vi.mock('../../../../utils/messageTemplateManager', () => ({
  messageTemplateManager: { getTemplates: async () => [] },
}))
vi.mock('../../../../utils/recentMessagesManager', () => ({
  recentMessagesManager: { getRecentMessages: async () => [] },
}))

async function renderForm() {
  const hook = renderHook(() => useNewSession())
  await waitFor(() => expect(hook.result.current.availablePools).toHaveLength(1))
  return hook
}

describe('new session form state', () => {
  beforeEach(() => vi.clearAllMocks())
  afterEach(cleanup)

  it('composes options, registry edits and progress resets', async () => {
    const { result } = await renderForm()
    act(() => result.current.addDockerRegistry())
    act(() => result.current.updateDockerRegistry(0, 'server', 'registry.example'))
    expect(result.current.dockerRegistries[0].server).toBe('registry.example')
    act(() => result.current.removeDockerRegistry(0))
    expect(result.current.dockerRegistries).toEqual([])
    act(() => result.current.handleRetry())
    expect(result.current.isCreating).toBe(false)
    expect(result.current.creationProgress).toBeNull()
  })

  it('passes the selected team, checkout and pool in a submission snapshot', async () => {
    const { result } = await renderForm()
    act(() => {
      result.current.setInitialMessage('hello')
      result.current.selectFreeFormRepositorySuggestion('org/repo')
      result.current.handleCheckoutTargetChange('branch')
      result.current.setCheckoutBranch('topic')
      result.current.setSelectedManagerId('fly-dev')
    })
    const preventDefault = vi.fn()
    await act(async () => result.current.handleSubmit({ preventDefault } as unknown as React.FormEvent))
    expect(preventDefault).toHaveBeenCalledOnce()
    expect(mocks.submit).toHaveBeenCalledWith(
      expect.objectContaining({
        draft: expect.objectContaining({
          initialMessage: 'hello',
          selectedTeam: 'team-1',
          freeFormRepository: 'org/repo',
          checkoutTarget: 'branch',
          checkoutBranch: 'topic',
          selectedManagerId: 'fly-dev',
        }),
      })
    )
  })

  it('closes the template modal before navigating back on Escape', async () => {
    const { result } = await renderForm()
    act(() => result.current.setShowTemplateModal(true))
    act(() => document.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape' })))
    expect(result.current.showTemplateModal).toBe(false)
    expect(mocks.back).not.toHaveBeenCalled()
    act(() => document.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape' })))
    expect(mocks.back).toHaveBeenCalledOnce()
  })
})
