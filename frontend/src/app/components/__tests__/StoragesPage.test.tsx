import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react'
import { afterEach, expect, it, vi } from 'vitest'
import StoragesPage from '../../storages/page'

const mocks = vi.hoisted(() => ({
  scope: () => ({ scope: 'team', team_id: 'org/team' }),
  getStorages: vi.fn().mockResolvedValue({ storages: [{ id: 'one', name: 'Shared data', state: 'ready', revision: 3, file_count: 2, updated_at: '2026-10-20T00:00:00Z', scope: 'team', team_id: 'org/team' }] }),
  createStorage: vi.fn().mockResolvedValue({}),
  updateStorage: vi.fn().mockResolvedValue({}),
  deleteStorage: vi.fn().mockResolvedValue(undefined),
}))
vi.mock('../../../contexts/TeamScopeContext', () => ({ useTeamScope: () => ({ getScopeParams: mocks.scope }) }))
vi.mock('../../../lib/agentapi-proxy-client', () => ({ createAgentAPIProxyClientFromStorage: () => mocks }))
vi.mock('../TopBar', () => ({ default: () => null }))
vi.mock('../NavigationTabs', () => ({ default: () => null }))
afterEach(() => { cleanup(); vi.clearAllMocks(); vi.restoreAllMocks() })

it('lists scoped storage, creates, edits, and deletes it', async () => {
  render(<StoragesPage />)
  await screen.findByText('Shared data')
  expect(mocks.getStorages).toHaveBeenCalledWith({ scope: 'team', team_id: 'org/team' })
  fireEvent.click(screen.getByRole('button', { name: '新しい Storage' }))
  fireEvent.change(screen.getByLabelText('名前'), { target: { value: 'New data' } })
  fireEvent.click(screen.getByRole('button', { name: '保存' }))
  await waitFor(() => expect(mocks.createStorage).toHaveBeenCalledWith({ name: 'New data', description: '', scope: 'team', team_id: 'org/team', kind: 'object_snapshot' }))
  await waitFor(() => expect(screen.queryByRole('dialog')).not.toBeInTheDocument())
  fireEvent.click(await screen.findByRole('button', { name: '編集' }))
  fireEvent.change(screen.getByLabelText('名前'), { target: { value: 'Renamed' } })
  fireEvent.click(screen.getByRole('button', { name: '保存' }))
  await waitFor(() => expect(mocks.updateStorage).toHaveBeenCalledWith('one', { name: 'Renamed', description: '' }))
  await waitFor(() => expect(screen.queryByRole('dialog')).not.toBeInTheDocument())
  vi.spyOn(window, 'confirm').mockReturnValueOnce(false).mockReturnValueOnce(true)
  fireEvent.click(await screen.findByRole('button', { name: '削除' }))
  expect(mocks.deleteStorage).not.toHaveBeenCalled()
  fireEvent.click(screen.getByRole('button', { name: '削除' }))
  await waitFor(() => expect(mocks.deleteStorage).toHaveBeenCalledWith('one'))
  await screen.findByText('Storage はまだありません')
})
