import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import WebhookListView from '../WebhookListView'
import type { Webhook, WebhookStatus } from '../../../types/webhook'

interface WebhookCardContractProps {
  webhook: Webhook
  onTogglePause: (id: string, status: WebhookStatus) => void
  onRegenerateSecret: (id: string) => void
  onDelete: (id: string) => void
}

const mocks = vi.hoisted(() => ({
  scope: vi.fn(() => ({ scope: 'user' })),
  getWebhooks: vi.fn(),
  updateWebhook: vi.fn(),
  deleteWebhook: vi.fn(),
  regenerateWebhookSecret: vi.fn(),
}))

vi.mock('../../../contexts/TeamScopeContext', () => ({
  useTeamScope: () => ({ getScopeParams: mocks.scope }),
}))
vi.mock('../../../lib/agentapi-proxy-client', () => ({
  createAgentAPIProxyClientFromStorage: () => mocks,
}))
vi.mock('../WebhookCard', () => ({
  default: ({ webhook, onTogglePause, onRegenerateSecret, onDelete }: WebhookCardContractProps) => (
    <div>
      <span>{webhook.name}</span>
      <span>{webhook.status}</span>
      <button onClick={() => onTogglePause(webhook.id, webhook.status)}>toggle</button>
      <button onClick={() => onRegenerateSecret(webhook.id)}>regenerate</button>
      <button onClick={() => onDelete(webhook.id)}>delete</button>
    </div>
  ),
}))

const webhook = {
  id: 'webhook-fixed', name: 'Deterministic webhook', status: 'active', type: 'custom',
  triggers: [], created_at: '2026-09-07T09:00:00Z', updated_at: '2026-09-07T09:00:00Z',
} as const

beforeEach(() => {
  mocks.getWebhooks.mockResolvedValue({ webhooks: [webhook] })
  mocks.updateWebhook.mockResolvedValue({ ...webhook, status: 'paused' })
  mocks.deleteWebhook.mockResolvedValue(undefined)
  mocks.regenerateWebhookSecret.mockResolvedValue({ id: webhook.id, secret: 'rotated' })
})
afterEach(() => { cleanup(); vi.clearAllMocks() })

describe('WebhookListView acceptance contract', () => {
  it('lists, pauses, rotates, and deletes through the API client', async () => {
    render(<WebhookListView statusFilter={null} typeFilter={null} onWebhookEdit={vi.fn()} />)
    expect(await screen.findByText(webhook.name)).toBeInTheDocument()
    expect(mocks.getWebhooks).toHaveBeenCalledWith({ scope: 'user' })

    fireEvent.click(screen.getByRole('button', { name: 'toggle' }))
    await waitFor(() => expect(mocks.updateWebhook).toHaveBeenCalledWith(webhook.id, { status: 'paused' }))
    expect(screen.getByText('paused')).toBeInTheDocument()

    fireEvent.click(screen.getByRole('button', { name: 'regenerate' }))
    await waitFor(() => expect(mocks.regenerateWebhookSecret).toHaveBeenCalledWith(webhook.id))

    fireEvent.click(screen.getByRole('button', { name: 'delete' }))
    await waitFor(() => expect(mocks.deleteWebhook).toHaveBeenCalledWith(webhook.id))
    expect(screen.queryByText(webhook.name)).not.toBeInTheDocument()
  })
})
