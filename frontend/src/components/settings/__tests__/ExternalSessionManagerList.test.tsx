import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { ExternalSessionManagerList } from '../ExternalSessionManagerList'

const mocks = vi.hoisted(() => ({
  status: vi.fn(), logs: vi.fn(), restart: vi.fn(), upgrade: vi.fn(),
}))
vi.mock('@/lib/agentapi-proxy-client', () => ({
  createAgentAPIProxyClientFromStorage: () => ({
    getExternalSessionManagerOperationalStatus: mocks.status,
    getExternalSessionManagerLogs: mocks.logs,
    restartExternalSessionManager: mocks.restart,
    upgradeExternalSessionManager: mocks.upgrade,
  }),
}))

beforeEach(() => {
  mocks.status.mockResolvedValue({ status: 'online', version: 'v1.20.0', active_sessions: 2, uptime_seconds: 120, capabilities: [] })
  mocks.logs.mockResolvedValue({ lines: ['manager ready'], source: 'daemon' })
  vi.stubGlobal('confirm', vi.fn(() => true))
})
afterEach(() => { cleanup(); vi.clearAllMocks(); vi.unstubAllGlobals() })

describe('ExternalSessionManagerList acceptance contract', () => {
  it('edits configuration and loads deterministic operational state', async () => {
    const onChange = vi.fn()
    render(<ExternalSessionManagerList
      managers={[{ id: 'manager-fixed', name: 'Deterministic manager', has_connection_token: true }]}
      onChange={onChange} revealedTokens={{}} onRegenerate={vi.fn()}
      regeneratingEsmId={null} scope="user"
    />)

    fireEvent.click(screen.getByRole('button', { name: '編集' }))
    fireEvent.change(screen.getByDisplayValue('Deterministic manager'), { target: { value: 'Renamed manager' } })
    fireEvent.click(screen.getByRole('button', { name: '更新' }))
    expect(onChange).toHaveBeenCalledWith([{ id: 'manager-fixed', name: 'Renamed manager', has_connection_token: true }])

    fireEvent.click(screen.getByRole('button', { name: '運用管理' }))
    await waitFor(() => {
      expect(mocks.status).toHaveBeenCalledWith('manager-fixed', 'user', undefined)
      expect(mocks.logs).toHaveBeenCalledWith('manager-fixed', 'user', undefined)
    })
    expect(await screen.findByText(/接続中 · v1.20.0/)).toBeInTheDocument()
    expect(screen.getByText('manager ready')).toBeInTheDocument()

    fireEvent.click(screen.getByRole('button', { name: '削除' }))
    expect(onChange).toHaveBeenLastCalledWith([])
  })
})
