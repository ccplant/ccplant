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
