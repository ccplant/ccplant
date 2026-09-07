import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import SlackbotListView from '../SlackbotListView'
import type { SlackBot, SlackBotStatus } from '../../../types/slackbot'

interface SlackbotCardContractProps {
  slackbot: SlackBot
  onToggleStatus: (id: string, status: SlackBotStatus) => void
  onDelete: (id: string) => void
}

const mocks = vi.hoisted(() => ({
  scope: vi.fn(() => ({ scope: 'user' })),
  getSlackBots: vi.fn(), updateSlackBot: vi.fn(), deleteSlackBot: vi.fn(),
}))
vi.mock('../../../contexts/TeamScopeContext', () => ({ useTeamScope: () => ({ getScopeParams: mocks.scope }) }))
vi.mock('../../../lib/agentapi-proxy-client', () => ({ createAgentAPIProxyClientFromStorage: () => mocks }))
vi.mock('../SlackbotCard', () => ({
  default: ({ slackbot, onToggleStatus, onDelete }: SlackbotCardContractProps) => (
    <div>
      <span>{slackbot.name}</span><span>{slackbot.status}</span>
      <button onClick={() => onToggleStatus(slackbot.id, 'paused')}>pause</button>
      <button onClick={() => onDelete(slackbot.id)}>delete</button>
    </div>
  ),
}))

const bot = {
  id: 'slackbot-fixed', name: 'Deterministic Slackbot', status: 'active',
  created_at: '2026-09-07T09:00:00Z', updated_at: '2026-09-07T09:00:00Z',
} as const

beforeEach(() => {
  mocks.getSlackBots.mockResolvedValue({ slackbots: [bot] })
  mocks.updateSlackBot.mockResolvedValue({ ...bot, status: 'paused' })
  mocks.deleteSlackBot.mockResolvedValue(undefined)
})
afterEach(() => { cleanup(); vi.clearAllMocks() })

describe('SlackbotListView acceptance contract', () => {
  it('lists, pauses, and deletes through the API client', async () => {
    render(<SlackbotListView statusFilter={null} onSlackbotEdit={vi.fn()} />)
    expect(await screen.findByText(bot.name)).toBeInTheDocument()
    expect(mocks.getSlackBots).toHaveBeenCalledWith({ scope: 'user' })
    fireEvent.click(screen.getByRole('button', { name: 'pause' }))
    await waitFor(() => expect(mocks.updateSlackBot).toHaveBeenCalledWith(bot.id, { status: 'paused' }))
    expect(screen.getByText('paused')).toBeInTheDocument()
    fireEvent.click(screen.getByRole('button', { name: 'delete' }))
    await waitFor(() => expect(mocks.deleteSlackBot).toHaveBeenCalledWith(bot.id))
    expect(screen.queryByText(bot.name)).not.toBeInTheDocument()
  })
})
