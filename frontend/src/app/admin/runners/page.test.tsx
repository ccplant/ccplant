/**
 * @vitest-environment happy-dom
 */
import { fireEvent, render, screen, waitFor } from '@testing-library/react'
import { afterAll, beforeEach, describe, expect, it, vi } from 'vitest'
import AdminRunnersPage from './page'

const listAdminSessionRunners = vi.fn()
const getAdminSessionRunnerLogs = vi.fn()
const deleteAdminSessionRunner = vi.fn()

vi.mock('@/lib/agentapi-proxy-client', () => ({
  createCurrentDeploymentAgentAPIProxyClient: () => ({
    listAdminSessionRunners,
    getAdminSessionRunnerLogs,
    deleteAdminSessionRunner,
  }),
}))

// The shared Vitest config disables module isolation to reduce CI memory usage.
// Do not let this page-specific mock leak into later component suites.
afterAll(() => {
  vi.unmock('@/lib/agentapi-proxy-client')
  vi.resetModules()
})

describe('AdminRunnersPage', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    listAdminSessionRunners.mockResolvedValue([
      { id: 'pooled-runner', manager_id: 'manager-a', manager_name: 'Manager A', pool: 'linux', from_pool: true, status: 'running', session_id: 'session-a', online: true },
      { id: 'direct-session', manager_id: 'manager-a', manager_name: 'Manager A', from_pool: false, status: 'running', session_id: 'direct-session', online: true },
    ])
    getAdminSessionRunnerLogs.mockResolvedValue({ lines: ['runner output'], source: 'pod/agentapi' })
    deleteAdminSessionRunner.mockResolvedValue(undefined)
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

  it('confirms and deletes the complete runner workload', async () => {
    vi.spyOn(window, 'confirm').mockReturnValue(true)
    render(<AdminRunnersPage />)

    expect(await screen.findByText('pooled-runner')).toBeInTheDocument()
    fireEvent.click(screen.getAllByRole('button', { name: '削除' })[0])
    await waitFor(() => expect(deleteAdminSessionRunner).toHaveBeenCalledWith('pooled-runner', 'manager-a', false))
    expect(window.confirm).toHaveBeenCalledWith(expect.stringContaining('関連する Secret'))
  })

  it('selects multiple runners and deletes them together', async () => {
    vi.spyOn(window, 'confirm').mockReturnValue(true)
    render(<AdminRunnersPage />)

    expect(await screen.findByText('pooled-runner')).toBeInTheDocument()
    fireEvent.click(screen.getByRole('checkbox', { name: 'すべて選択' }))
    expect(screen.getByText('2 件選択中')).toBeInTheDocument()
    fireEvent.click(screen.getByRole('button', { name: '選択した Runner を削除' }))

    await waitFor(() => expect(deleteAdminSessionRunner).toHaveBeenCalledTimes(2))
    expect(deleteAdminSessionRunner).toHaveBeenCalledWith('pooled-runner', 'manager-a', false)
    expect(deleteAdminSessionRunner).toHaveBeenCalledWith('direct-session', 'manager-a', false)
    expect(window.confirm).toHaveBeenCalledWith(expect.stringContaining('選択した 2 件'))
  })

  it('warns and force-cleans parent metadata for an offline runner', async () => {
    vi.spyOn(window, 'confirm').mockReturnValue(true)
    listAdminSessionRunners.mockResolvedValue([
      { id: 'offline-runner', manager_id: 'manager-a', manager_name: 'Manager A', pool: 'linux', from_pool: true, status: 'offline', online: false },
    ])
    render(<AdminRunnersPage />)

    fireEvent.click(await screen.findByRole('button', { name: '強制クリーンアップ' }))

    await waitFor(() => expect(deleteAdminSessionRunner).toHaveBeenCalledWith('offline-runner', 'manager-a', true))
    expect(window.confirm).toHaveBeenCalledWith(expect.stringContaining('親側の Runner、Session の紐づき、allocation メタデータだけ'))
    expect(window.confirm).toHaveBeenCalledWith(expect.stringContaining('Manager が復旧しないこと'))
  })
})
