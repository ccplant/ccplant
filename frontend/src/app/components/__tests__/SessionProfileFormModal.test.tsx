import { render, screen, fireEvent, waitFor, cleanup } from '@testing-library/react'
import { afterEach, describe, expect, it, vi } from 'vitest'
import SessionProfileFormModal from '../SessionProfileFormModal'

const mocks = vi.hoisted(() => ({
  scope: () => ({ scope: 'user' }),
  update: vi.fn().mockResolvedValue(undefined),
  client: {
    getSandboxPolicies: vi.fn().mockResolvedValue({ sandbox_policies: [] }),
    getAvailableSessionPools: vi.fn().mockResolvedValue([]),
  },
}))
vi.mock('../../../contexts/TeamScopeContext', () => ({ useTeamScope: () => ({ getScopeParams: mocks.scope }) }))
vi.mock('../../../lib/agentapi-proxy-client', () => ({ createAgentAPIProxyClientFromStorage: () => ({ ...mocks.client, updateSessionProfile: mocks.update }) }))
vi.mock('../../../components/settings/MCPServerSettings', () => ({ MCPServerSettings: () => null }))
afterEach(() => { cleanup(); vi.clearAllMocks() })

describe('SessionProfileFormModal authentication', () => {
  it('loads selections, saves changes, and restores inheritance', async () => {
    render(<SessionProfileFormModal isOpen onClose={vi.fn()} onSuccess={vi.fn()} editingProfile={{ id: 'profile', name: 'Test', created_at: '', updated_at: '', config: { params: { codex_auth_mode: 'openai_compatible', claude_auth_mode: 'bedrock' } } }} />)
    expect(screen.getByLabelText('Codex の認証方法')).toHaveValue('openai_compatible')
    expect(screen.getByLabelText('Claude Code の認証方法')).toHaveValue('bedrock')
    fireEvent.change(screen.getByLabelText('Codex の認証方法'), { target: { value: 'auth_json' } })
    fireEvent.change(screen.getByLabelText('Claude Code の認証方法'), { target: { value: 'anthropic_compatible' } })
    fireEvent.submit(screen.getByLabelText('Codex の認証方法').closest('form')!)
    await waitFor(() => expect(mocks.update).toHaveBeenCalledTimes(1))
    expect(mocks.update.mock.calls[0][1].config.params).toMatchObject({ codex_auth_mode: 'auth_json', claude_auth_mode: 'anthropic_compatible' })
    fireEvent.change(screen.getByLabelText('Codex の認証方法'), { target: { value: '' } })
    fireEvent.change(screen.getByLabelText('Claude Code の認証方法'), { target: { value: '' } })
    fireEvent.submit(screen.getByLabelText('Codex の認証方法').closest('form')!)
    await waitFor(() => expect(mocks.update).toHaveBeenCalledTimes(2))
    expect(mocks.update.mock.calls[1][1].config.params).not.toHaveProperty('codex_auth_mode')
    expect(mocks.update.mock.calls[1][1].config.params).not.toHaveProperty('claude_auth_mode')
  })
})

it('preserves a stored profile key on edit and explicitly removes the override', async () => {
  render(<SessionProfileFormModal isOpen onClose={vi.fn()} onSuccess={vi.fn()} editingProfile={{ id: 'profile', name: 'Test', created_at: '', updated_at: '', config: { params: { codex_auth_mode: 'openai_compatible' }, codex_connection: { mode: 'openai_compatible', base_url: 'https://old.example/v1', authentication: 'api_key', has_api_key: true } } }} />)
  expect(screen.getByLabelText('Codex API キー')).toHaveValue('')
  fireEvent.change(screen.getByLabelText('Codex Base URL'), { target: { value: 'https://new.example/v1' } })
  fireEvent.submit(screen.getByLabelText('Codex Base URL').closest('form')!)
  await waitFor(() => expect(mocks.update).toHaveBeenCalledTimes(1))
  expect(mocks.update.mock.calls[0][1].config.codex_connection).toMatchObject({ base_url: 'https://new.example/v1' })
  expect(mocks.update.mock.calls[0][1].config.codex_connection).not.toHaveProperty('api_key')
  fireEvent.change(screen.getByLabelText('Codex API キー'), { target: { value: 'replacement-key' } })
  fireEvent.submit(screen.getByLabelText('Codex Base URL').closest('form')!)
  await waitFor(() => expect(mocks.update).toHaveBeenCalledTimes(2))
  expect(mocks.update.mock.calls[1][1].config.codex_connection.api_key).toBe('replacement-key')
  fireEvent.click(screen.getByLabelText('このプロファイル専用の接続先・API キーを使う'))
  fireEvent.submit(screen.getByLabelText('Codex の認証方法').closest('form')!)
  await waitFor(() => expect(mocks.update).toHaveBeenCalledTimes(3))
  expect(mocks.update.mock.calls[2][1].config.codex_connection).toBeNull()
})
