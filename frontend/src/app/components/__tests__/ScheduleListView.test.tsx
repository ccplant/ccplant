import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import ScheduleListView from '../ScheduleListView'
import type { Schedule } from '../../../types/schedule'

const mocks = vi.hoisted(() => ({
  getScopeParams: vi.fn(() => ({ scope: 'user' })),
  getSchedules: vi.fn(),
  updateSchedule: vi.fn(),
  triggerSchedule: vi.fn(),
  deleteSchedule: vi.fn(),
}))

vi.mock('../../../contexts/TeamScopeContext', () => ({
  useTeamScope: () => ({ getScopeParams: mocks.getScopeParams }),
}))

vi.mock('../../../lib/agentapi-proxy-client', () => ({
  createAgentAPIProxyClientFromStorage: () => mocks,
}))

const schedule: Schedule = {
  id: 'schedule-fixed',
  name: 'Deterministic schedule',
  status: 'active',
  cron_expr: '0 9 * * *',
  timezone: 'UTC',
  next_execution_at: '2026-09-08T09:00:00Z',
  execution_count: 0,
  session_config: { params: { message: 'Run QA' } },
  created_at: '2026-09-07T09:00:00Z',
  updated_at: '2026-09-07T09:00:00Z',
}

beforeEach(() => {
  mocks.getSchedules.mockResolvedValue({ schedules: [schedule] })
  mocks.updateSchedule.mockResolvedValue({ ...schedule, status: 'paused' })
  mocks.triggerSchedule.mockResolvedValue({ session_id: 'session-fixed' })
  mocks.deleteSchedule.mockResolvedValue(undefined)
  vi.stubGlobal('alert', vi.fn())
  vi.stubGlobal('confirm', vi.fn(() => true))
})

afterEach(() => {
  cleanup()
  vi.clearAllMocks()
  vi.unstubAllGlobals()
})

describe('ScheduleListView acceptance contract', () => {
  it('lists, pauses, triggers, and deletes a schedule through the API client', async () => {
    render(<ScheduleListView statusFilter={null} onScheduleEdit={vi.fn()} />)

    expect(await screen.findByText('Deterministic schedule')).toBeInTheDocument()
    expect(mocks.getSchedules).toHaveBeenCalledWith({ scope: 'user' })

    fireEvent.click(screen.getByRole('button', { name: '一時停止' }))
    await waitFor(() => {
      expect(mocks.updateSchedule).toHaveBeenCalledWith('schedule-fixed', { status: 'paused' })
    })
    expect(screen.getByRole('button', { name: '再開' })).toBeInTheDocument()

    fireEvent.click(screen.getByRole('button', { name: '今すぐ実行' }))
    await waitFor(() => {
      expect(mocks.triggerSchedule).toHaveBeenCalledWith('schedule-fixed')
      expect(window.alert).toHaveBeenCalledWith(
        'スケジュールを実行しました。セッションID: session-fixed',
      )
    })

    fireEvent.click(screen.getByRole('button', { name: '削除' }))
    await waitFor(() => {
      expect(window.confirm).toHaveBeenCalledWith(
        'スケジュール「Deterministic schedule」を削除してもよろしいですか？',
      )
      expect(mocks.deleteSchedule).toHaveBeenCalledWith('schedule-fixed')
    })
    expect(screen.queryByText('Deterministic schedule')).not.toBeInTheDocument()
  })
})
