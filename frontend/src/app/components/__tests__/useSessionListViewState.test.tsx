import { act, cleanup, fireEvent, render, renderHook, screen, waitFor } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import type { Session } from '../../../types/agentapi'
import SessionListView from '../SessionListView'
import { useSessionListViewState } from '../useSessionListViewState'

const mocks = vi.hoisted(() => ({
  search: vi.fn(),
  push: vi.fn(),
  scope: { selectedTeam: 'team-a' as string | null, isLoading: false },
  status: undefined as undefined | ((event: { session_id: string; status: string; timestamp: string }) => void),
}))
vi.mock('next/navigation', () => ({ useRouter: () => ({ push: mocks.push }) }))
vi.mock('../../../contexts/TeamScopeContext', () => ({ useTeamScope: () => mocks.scope }))
vi.mock('../../../lib/agentapi-proxy-client', () => ({
  createAgentAPIProxyClientFromStorage: () => ({ search: mocks.search }),
  AgentAPIProxyError: class extends Error {},
}))
vi.mock('../../../lib/acp-server-client', () => ({ createACPServerClientFromStorage: () => ({}) }))
vi.mock('../../../types/settings', () => ({ getACPServerEnabled: () => false }))
vi.mock('../../hooks/useSessionsStatusStream', () => ({
  useSessionsStatusStream: (options: { onStatusChange: typeof mocks.status }) => {
    mocks.status = options.onStatusChange
  },
}))

const session = (id: string): Session => ({
  session_id: id,
  user_id: 'user-1',
  status: 'active',
  started_at: '2026-11-23T08:00:00Z',
  metadata: { description: `Session ${id}` },
})
const props = () => ({ tagFilters: {}, onSessionsUpdate: vi.fn() })

describe('session list view state', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    mocks.scope = { selectedTeam: 'team-a', isLoading: false }
    mocks.search.mockResolvedValue({ sessions: [] })
    localStorage.clear()
  })
  afterEach(cleanup)

  it('waits for team initialization before requesting sessions', async () => {
    mocks.scope.isLoading = true
    const { rerender, result } = renderHook(() => useSessionListViewState(props()))
    await act(async () => Promise.resolve())
    expect(mocks.search).not.toHaveBeenCalled()
    mocks.scope.isLoading = false
    rerender()
    await waitFor(() => expect(result.current.loading).toBe(false))
    expect(mocks.search).toHaveBeenCalledWith({ scope: 'team', team_id: 'team-a', limit: 1000 })
  })

  it('ignores stale scope responses and applies live status updates', async () => {
    let resolveA!: (value: { sessions: Session[] }) => void
    let resolveB!: (value: { sessions: Session[] }) => void
    mocks.search.mockImplementationOnce(
      () =>
        new Promise((resolve) => {
          resolveA = resolve
        })
    )
    mocks.search.mockImplementationOnce(
      () =>
        new Promise((resolve) => {
          resolveB = resolve
        })
    )
    const { result, rerender } = renderHook(() => useSessionListViewState(props()))
    mocks.scope.selectedTeam = 'team-b'
    rerender()
    await act(async () => resolveB({ sessions: [session('b')] }))
    expect(result.current.paginatedSessions.map((item) => item.session_id)).toEqual(['b'])
    await act(async () => resolveA({ sessions: [session('a')] }))
    expect(result.current.paginatedSessions.map((item) => item.session_id)).toEqual(['b'])
    act(() => mocks.status?.({ session_id: 'b', status: 'running', timestamp: '2026-11-23T09:00:00Z' }))
    expect(result.current.paginatedSessions[0].status).toBe('running')
    expect(result.current.getAgentStatusForSession(result.current.paginatedSessions[0]).text).toBe('Running')
  })

  it('renders an extracted row and navigates to its chat', async () => {
    mocks.search.mockResolvedValue({ sessions: [session('visible')] })
    render(<SessionListView {...props()} />)
    await screen.findByText('Session visible')
    fireEvent.click(screen.getByRole('button', { name: 'チャット' }))
    expect(mocks.push).toHaveBeenCalledWith('/sessions/visible')
  })
})
