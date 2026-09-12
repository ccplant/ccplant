import { renderHook, waitFor } from '@testing-library/react'
import { describe, expect, it, vi } from 'vitest'

import { AgentAPIProxyClient } from '../../lib/agentapi-proxy-client'
import { useSessionList } from '../useSessionList'

describe('useSessionList', () => {
  it('does not request personal sessions while the team scope is initializing', async () => {
    const search = vi.fn().mockResolvedValue({ sessions: [] })
    const client = { search } as unknown as AgentAPIProxyClient
    const { rerender } = renderHook(
      ({ ready }) => useSessionList(
        ready ? { scope: 'team', team_id: 'acme/platform' } : null,
        client,
      ),
      { initialProps: { ready: false } },
    )

    await Promise.resolve()
    expect(search).not.toHaveBeenCalled()

    rerender({ ready: true })
    await waitFor(() => expect(search).toHaveBeenCalledTimes(1))
    expect(search).toHaveBeenCalledWith({ scope: 'team', team_id: 'acme/platform' })
  })
})
