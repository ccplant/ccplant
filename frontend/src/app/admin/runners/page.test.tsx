/**
 * @vitest-environment happy-dom
 */
import { fireEvent, render, screen, waitFor } from '@testing-library/react'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import AdminRunnersPage from './page'

const listAdminSessionRunners = vi.fn()
const getAdminSessionRunnerLogs = vi.fn()

vi.mock('@/lib/agentapi-proxy-client', () => ({
  createCurrentDeploymentAgentAPIProxyClient: () => ({
    listAdminSessionRunners,
    getAdminSessionRunnerLogs,
  }),
}))

describe('AdminRunnersPage', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    listAdminSessionRunners.mockResolvedValue([
      { id: 'pooled-runner', manager_id: 'manager-a', manager_name: 'Manager A', pool: 'linux', from_pool: true, status: 'running', session_id: 'session-a', online: true },
      { id: 'direct-session', manager_id: 'manager-a', manager_name: 'Manager A', from_pool: false, status: 'running', session_id: 'direct-session', online: true },
    ])
    getAdminSessionRunnerLogs.mockResolvedValue({ lines: ['runner output'], source: 'pod/agentapi' })
  })

  it('shows pool origin, linked sessions, and runner logs', async () => {
    render(<AdminRunnersPage />)

    expect(await screen.findByText('pooled-runner')).toBeInTheDocument()
    expect(screen.getByText('Pool: linux')).toBeInTheDocument()
    expect(screen.getByText('Pool から作成されていません')).toBeInTheDocument()
    expect(screen.getByText('session-a')).toBeInTheDocument()

    fireEvent.click(screen.getAllByRole('button', { name: /ログ/ })[1])
    await waitFor(() => expect(getAdminSessionRunnerLogs).toHaveBeenCalledWith('direct-session', 'manager-a'))
    expect(await screen.findByText('runner output')).toBeInTheDocument()
  })
})
