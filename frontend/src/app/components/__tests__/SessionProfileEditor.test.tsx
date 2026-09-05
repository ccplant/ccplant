import { render, screen, fireEvent, waitFor, cleanup } from '@testing-library/react'
import { afterEach, describe, expect, it, vi } from 'vitest'
import SessionProfileEditor from '../SessionProfileEditor'

const mocks = vi.hoisted(() => ({
  scope: () => ({ scope: 'user' }),
  create: vi.fn().mockResolvedValue(undefined),
  update: vi.fn().mockResolvedValue(undefined),
  client: {
    getSandboxPolicies: vi.fn().mockResolvedValue({ sandbox_policies: [] }),
    getAvailableSessionPools: vi.fn().mockResolvedValue([]),
  },
}))
vi.mock('next/navigation', () => ({ usePathname: () => '/session-profiles/profile', useSearchParams: () => new URLSearchParams() }))
vi.mock('../../../contexts/TeamScopeContext', () => ({ useTeamScope: () => ({ getScopeParams: mocks.scope, availableTeams: ['org/team', 'org/other'] }) }))
vi.mock('../../../lib/agentapi-proxy-client', () => ({ createAgentAPIProxyClientFromStorage: () => ({ ...mocks.client, updateSessionProfile: mocks.update, createSessionProfile: mocks.create }) }))
vi.mock('../../../components/settings/MCPServerSettings', () => ({ MCPServerSettings: () => null }))
afterEach(() => { cleanup(); vi.clearAllMocks() })

describe('SessionProfileEditor authentication', () => {
  it('loads selections, saves changes, and restores inheritance', async () => {
    render(<SessionProfileEditor section="authentication" onClose={vi.fn()} onSuccess={vi.fn()} editingProfile={{ id: 'profile', name: 'Test', created_at: '', updated_at: '', config: { params: { codex_auth_mode: 'openai_compatible', claude_auth_mode: 'bedrock' } } }} />)
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
  render(<SessionProfileEditor section="authentication" onClose={vi.fn()} onSuccess={vi.fn()} editingProfile={{ id: 'profile', name: 'Test', created_at: '', updated_at: '', config: { params: { codex_auth_mode: 'openai_compatible' }, codex_connection: { mode: 'openai_compatible', base_url: 'https://old.example/v1', authentication: 'api_key', has_api_key: true } } }} />)
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


it('saves and restores team settings inheritance', async () => {
  render(<SessionProfileEditor section="inheritance" onClose={vi.fn()} onSuccess={vi.fn()} editingProfile={{ id: 'profile', name: 'Test', created_at: '', updated_at: '', config: { settings_team_id: 'org/team' } }} />)
  expect(screen.getByLabelText('ベースにする設定')).toHaveValue('org/team')
  fireEvent.change(screen.getByLabelText('ベースにする設定'), { target: { value: 'org/other' } })
  fireEvent.submit(screen.getByLabelText('ベースにする設定').closest('form')!)
  await waitFor(() => expect(mocks.update).toHaveBeenCalledTimes(1))
  expect(mocks.update.mock.calls[0][1].config.settings_team_id).toBe('org/other')
  fireEvent.change(screen.getByLabelText('ベースにする設定'), { target: { value: '' } })
  fireEvent.submit(screen.getByLabelText('ベースにする設定').closest('form')!)
  await waitFor(() => expect(mocks.update).toHaveBeenCalledTimes(2))
  expect(mocks.update.mock.calls[1][1].config).not.toHaveProperty('settings_team_id')
})

it('retains edits between sections, discards all edits, and preserves API-only fields', async () => {
  const profile = { id: 'profile', name: 'Original', created_at: '', updated_at: '', config: { reuse_session: true, initial_message_template: 'Hello', params: { auth_proxy: true, credential_source: 'none' as const } } }
  const props = { editingProfile: profile, onClose: vi.fn(), onSuccess: vi.fn() }
  const { rerender } = render(<SessionProfileEditor {...props} section="basic" />)
  fireEvent.change(screen.getByDisplayValue('Original'), { target: { value: 'Changed' } })
  rerender(<SessionProfileEditor {...props} section="models" />)
  expect(screen.queryByDisplayValue('Changed')).not.toBeInTheDocument()
  fireEvent.change(screen.getByLabelText('Codex モデル ID'), { target: { value: 'test-model' } })
  rerender(<SessionProfileEditor {...props} section="basic" />)
  expect(screen.getByDisplayValue('Changed')).toBeInTheDocument()
  fireEvent.click(screen.getByRole('button', { name: '保存' }))
  await waitFor(() => expect(mocks.update).toHaveBeenCalledTimes(1))
  expect(mocks.update.mock.calls[0][1]).toMatchObject({ name: 'Changed', config: { reuse_session: true, initial_message_template: 'Hello', environment: { CODEX_MODEL: 'test-model' }, params: { auth_proxy: true, credential_source: 'none' as const } } })
  fireEvent.change(screen.getByDisplayValue('Changed'), { target: { value: 'Discard this' } })
  fireEvent.click(screen.getByRole('button', { name: '破棄' }))
  expect(screen.getByDisplayValue('Original')).toBeInTheDocument()
  rerender(<SessionProfileEditor {...props} section="models" />)
  expect(screen.getByLabelText('Codex モデル ID')).toHaveValue('')
})

it('creates in the scope from the URL and retains the draft after a save failure', async () => {
  const success = vi.fn()
  mocks.create.mockRejectedValueOnce(new Error('Save failed'))
  const { rerender } = render(<SessionProfileEditor createScope={{ scope: 'team', team_id: 'org/team' }} onClose={vi.fn()} onSuccess={success} />)
  fireEvent.change(screen.getByPlaceholderText('例: my-profile'), { target: { value: 'Team draft' } })
  fireEvent.click(screen.getByRole('button', { name: '作成' }))
  await waitFor(() => expect(screen.getByRole('alert')).toHaveTextContent('Save failed'))
  expect(success).not.toHaveBeenCalled()
  expect(screen.getByDisplayValue('Team draft')).toBeInTheDocument()
  rerender(<SessionProfileEditor createScope={{ scope: 'team', team_id: 'org/team' }} section="inheritance" onClose={vi.fn()} onSuccess={success} />)
  expect(screen.queryByRole('option', { name: 'チーム: org/other' })).not.toBeInTheDocument()
  expect(screen.getByLabelText('ベースにする設定')).toHaveValue('org/team')
  fireEvent.click(screen.getByRole('button', { name: '作成' }))
  await waitFor(() => expect(success).toHaveBeenCalledOnce())
  expect(mocks.create.mock.calls[1][0]).toMatchObject({ name: 'Team draft', scope: 'team', team_id: 'org/team', config: { settings_team_id: 'org/team' } })
})
