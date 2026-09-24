import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react'
import { afterEach, describe, expect, it, vi } from 'vitest'
import ScheduleFormModal from '../ScheduleFormModal'
import WebhookFormModal from '../WebhookFormModal'
import type { Schedule } from '../../../types/schedule'
import type { Webhook } from '../../../types/webhook'

const mocks = vi.hoisted(() => ({
  scope: vi.fn(() => ({ scope: 'user' })),
  updateSchedule: vi.fn().mockResolvedValue({}),
  updateWebhook: vi.fn().mockResolvedValue({}),
}))

vi.mock('../../../contexts/TeamScopeContext', () => ({
  useTeamScope: () => ({ getScopeParams: mocks.scope }),
}))
vi.mock('../../../lib/agentapi-proxy-client', () => ({
  createAgentAPIProxyClientFromStorage: () => mocks,
  AgentAPIProxyError: class AgentAPIProxyError extends Error {},
}))
vi.mock('../SessionProfileSelect', () => ({
  default: () => <div data-testid="session-profile-select" />,
}))

afterEach(() => {
  cleanup()
  vi.clearAllMocks()
})

describe('trigger model forms', () => {
  it('edits and saves a model for a schedule trigger', async () => {
    const schedule: Schedule = {
      id: 'schedule-1',
      name: 'Daily review',
      status: 'active',
      cron_expr: '0 9 * * *',
      timezone: 'UTC',
      session_config: { params: { message: 'Review', model: 'old-model' } },
      created_at: '2026-01-01T00:00:00Z',
      updated_at: '2026-01-01T00:00:00Z',
    }

    render(<ScheduleFormModal isOpen onClose={vi.fn()} onSuccess={vi.fn()} editingSchedule={schedule} />)
    const input = screen.getByLabelText('モデル')
    expect(input).toHaveValue('old-model')
    fireEvent.change(input, { target: { value: 'gpt-trigger' } })
    fireEvent.click(screen.getByRole('button', { name: '更新' }))

    await waitFor(() => expect(mocks.updateSchedule).toHaveBeenCalled())
    expect(mocks.updateSchedule.mock.calls[0][1].session_config.params.model).toBe('gpt-trigger')
  })

  it('edits and saves a model independently for each webhook trigger', async () => {
    const webhook: Webhook = {
      id: 'webhook-1',
      name: 'PR review',
      status: 'active',
      type: 'custom',
      triggers: [{
        id: 'trigger-1',
        name: 'opened',
        conditions: {},
        session_config: { params: { model: 'old-model' } },
      }],
      created_at: '2026-01-01T00:00:00Z',
      updated_at: '2026-01-01T00:00:00Z',
    }

    render(<WebhookFormModal isOpen onClose={vi.fn()} onSuccess={vi.fn()} editingWebhook={webhook} />)
    const input = screen.getByLabelText('モデル')
    expect(input).toHaveValue('old-model')
    fireEvent.change(input, { target: { value: 'claude-trigger' } })
    fireEvent.click(screen.getByRole('button', { name: '更新' }))

    await waitFor(() => expect(mocks.updateWebhook).toHaveBeenCalled())
    expect(mocks.updateWebhook.mock.calls[0][1].triggers[0].session_config.params.model).toBe('claude-trigger')
  })
})
