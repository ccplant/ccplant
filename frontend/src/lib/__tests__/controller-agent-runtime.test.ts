import { describe, expect, it, vi } from 'vitest'
import { sendControllerCommand, waitForControllerReady } from '../controller-agent-runtime'

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

describe('sendControllerCommand', () => {
  const restClient = () => ({
    getACPSessionInfo: vi.fn().mockResolvedValue(null),
    sendACPPrompt: vi.fn().mockResolvedValue(undefined),
    sendSessionMessage: vi.fn().mockResolvedValue({}),
  })

  it('uses the global ACP transport when enabled', async () => {
    const rest = restClient()
    const acp = { initialize: vi.fn().mockResolvedValue({}), sendPrompt: vi.fn().mockResolvedValue(undefined) }
    await sendControllerCommand({ sessionId: 'session-1', message: 'do work', globalACPEnabled: true, restClient: rest, acpServerClient: acp })
    expect(acp.initialize).toHaveBeenCalledOnce()
    expect(acp.sendPrompt).toHaveBeenCalledWith('session-1', [{ type: 'text', text: 'do work' }])
    expect(rest.sendSessionMessage).not.toHaveBeenCalled()
  })

  it('uses the per-session ACP bridge when detected', async () => {
    const rest = restClient()
    rest.getACPSessionInfo.mockResolvedValue({ sessionId: 'acp-1' })
    await sendControllerCommand({ sessionId: 'session-1', message: 'do work', globalACPEnabled: false, restClient: rest, promptId: 42 })
    expect(rest.sendACPPrompt).toHaveBeenCalledWith('session-1', 'acp-1', [{ type: 'text', text: 'do work' }], 42)
    expect(rest.sendSessionMessage).not.toHaveBeenCalled()
  })

  it('falls back to the AgentAPI message endpoint for regular sessions', async () => {
    const rest = restClient()
    await sendControllerCommand({ sessionId: 'session-1', message: 'do work', globalACPEnabled: false, restClient: rest })
    expect(rest.sendSessionMessage).toHaveBeenCalledWith('session-1', { content: 'do work', type: 'user' })
  })
})
