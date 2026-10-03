import { describe, expect, it, vi } from 'vitest'
import { waitForControllerReady } from '../controller-agent-runtime'

describe('waitForControllerReady', () => {
  it('waits until the controller session becomes stable', async () => {
    const getStatus = vi.fn()
      .mockResolvedValueOnce({ status: 'running' })
      .mockResolvedValueOnce({ status: 'stable' })
    const delay = vi.fn().mockResolvedValue(undefined)

    await expect(waitForControllerReady(getStatus, { delay })).resolves.toBeUndefined()
    expect(getStatus).toHaveBeenCalledTimes(2)
    expect(delay).toHaveBeenCalledWith(2_000)
  })

  it('surfaces a controller runtime error', async () => {
    await expect(waitForControllerReady(
      async () => ({ status: 'error', message: 'runtime failed' }),
      { delay: async () => undefined },
    )).rejects.toThrow('runtime failed')
  })
})
