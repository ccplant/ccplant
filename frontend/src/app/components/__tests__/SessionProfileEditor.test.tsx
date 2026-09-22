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

describe('SessionProfileEditor legacy pool settings', () => {
  it('shows a legacy pool and removes it when automatic selection is saved', async () => {
    render(<SessionProfileEditor section="pool" onClose={vi.fn()} onSuccess={vi.fn()} editingProfile={{ id: 'profile', name: 'Legacy', created_at: '', updated_at: '', config: { params: { pool: 'managed', auth_proxy: true } } }} />)
    const select = screen.getByLabelText('Session Runner Pool')
    expect(select).toHaveValue('managed')
    fireEvent.change(select, { target: { value: '' } })
    fireEvent.submit(select.closest('form')!)
    await waitFor(() => expect(mocks.update).toHaveBeenCalledTimes(1))
    const config = mocks.update.mock.calls[0][1].config
    expect(config).not.toHaveProperty('pool')
    expect(config.params).not.toHaveProperty('pool')
    expect(config.params.auth_proxy).toBe(true)
  })

  it.each([
    { config: { params: { pool: 'managed' } }, expected: 'managed' },
    { config: { pool: 'fly-dev', params: { pool: 'managed' } }, expected: 'fly-dev' },
  ])('saves the effective pool in the canonical field: $expected', async ({ config, expected }) => {
    render(<SessionProfileEditor section="pool" onClose={vi.fn()} onSuccess={vi.fn()} editingProfile={{ id: 'profile', name: 'Legacy', created_at: '', updated_at: '', config }} />)
    const select = screen.getByLabelText('Session Runner Pool')
    expect(select).toHaveValue(expected)
    fireEvent.submit(select.closest('form')!)
    await waitFor(() => expect(mocks.update).toHaveBeenCalledTimes(1))
    expect(mocks.update.mock.calls[0][1].config.pool).toBe(expected)
    expect(mocks.update.mock.calls[0][1].config.params).not.toHaveProperty('pool')
  })
})

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

  it('saves independent default models for each authentication method', async () => {
    render(<SessionProfileEditor section="models" onClose={vi.fn()} onSuccess={vi.fn()} editingProfile={{ id: 'profile', name: 'Models', created_at: '', updated_at: '', config: { params: { codex_default_models: { auth_json: 'account-model' }, claude_default_models: { bedrock: 'bedrock-model' } } } }} />)
    expect(screen.getByLabelText('Codex / auth.json のデフォルトモデル')).toHaveValue('account-model')
    expect(screen.getByLabelText('Claude / Bedrock のデフォルトモデル')).toHaveValue('bedrock-model')
    fireEvent.change(screen.getByLabelText('Codex / OpenAI 互換 API のデフォルトモデル'), { target: { value: 'gateway-model' } })
    fireEvent.change(screen.getByLabelText('Claude / OAuth のデフォルトモデル'), { target: { value: 'oauth-model' } })
    fireEvent.submit(screen.getByLabelText('Codex / auth.json のデフォルトモデル').closest('form')!)
    await waitFor(() => expect(mocks.update).toHaveBeenCalledTimes(1))
    expect(mocks.update.mock.calls[0][1].config.params).toMatchObject({
      codex_default_models: { auth_json: 'account-model', openai_compatible: 'gateway-model' },
      claude_default_models: { oauth: 'oauth-model', bedrock: 'bedrock-model' },
    })
  })
})

describe('SessionProfileEditor repository selector', () => {
  it('creates a profile with a repository selector', async () => {
    render(<SessionProfileEditor createScope={{ scope: 'user' }} onClose={vi.fn()} onSuccess={vi.fn()} />)
    fireEvent.change(screen.getByPlaceholderText('例: my-profile'), { target: { value: 'Repo profile' } })
    fireEvent.change(screen.getByLabelText('リポジトリ自動選択'), { target: { value: 'org/repo' } })
    fireEvent.click(screen.getByRole('button', { name: '作成' }))
    await waitFor(() => expect(mocks.create).toHaveBeenCalledTimes(1))
    expect(mocks.create.mock.calls[0][0].selector_tags).toEqual({ repository: 'org/repo' })
  })

  it('normalizes the legacy repo selector and preserves other selectors', async () => {
    render(<SessionProfileEditor onClose={vi.fn()} onSuccess={vi.fn()} editingProfile={{
      id: 'profile',
      name: 'Repo profile',
      created_at: '',
      updated_at: '',
      selector_tags: { repo: 'legacy/repo', env: 'dev' },
    }} />)
    expect(screen.getByLabelText('リポジトリ自動選択')).toHaveValue('legacy/repo')
    fireEvent.click(screen.getByRole('button', { name: '保存' }))
    await waitFor(() => expect(mocks.update).toHaveBeenCalledTimes(1))
    expect(mocks.update.mock.calls[0][1].selector_tags).toEqual({ env: 'dev', repository: 'legacy/repo' })
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
  fireEvent.change(screen.getByLabelText('Codex モデル ID'), { target: { value: 'gpt-test' } })
  fireEvent.change(screen.getByLabelText('Claude Code (Anthropic) モデル ID'), { target: { value: 'claude-test' } })
  rerender(<SessionProfileEditor {...props} section="basic" />)
  expect(screen.getByDisplayValue('Changed')).toBeInTheDocument()
  fireEvent.click(screen.getByRole('button', { name: '保存' }))
  await waitFor(() => expect(mocks.update).toHaveBeenCalledTimes(1))
  expect(mocks.update.mock.calls[0][1]).toMatchObject({ name: 'Changed', config: { reuse_session: true, initial_message_template: 'Hello', environment: { CODEX_MODEL: 'gpt-test', ANTHROPIC_MODEL: 'claude-test' }, params: { auth_proxy: true, credential_source: 'none' as const } } })
  fireEvent.change(screen.getByDisplayValue('Changed'), { target: { value: 'Discard this' } })
  fireEvent.click(screen.getByRole('button', { name: '破棄' }))
  expect(screen.getByDisplayValue('Original')).toBeInTheDocument()
  rerender(<SessionProfileEditor {...props} section="models" />)
  expect(screen.getByLabelText('Codex モデル ID')).toHaveValue('')
  expect(screen.getByLabelText('Claude Code (Anthropic) モデル ID')).toHaveValue('')
})

it('loads agent-specific models and keeps them out of the general environment editor', () => {
  const profile = { id: 'profile', name: 'Models', created_at: '', updated_at: '', config: { environment: { CODEX_MODEL: 'gpt-profile', ANTHROPIC_MODEL: 'claude-profile', KEEP_ME: 'yes' } } }
  const props = { editingProfile: profile, onClose: vi.fn(), onSuccess: vi.fn() }
  const { rerender } = render(<SessionProfileEditor {...props} section="models" />)
  expect(screen.getByLabelText('Codex モデル ID')).toHaveValue('gpt-profile')
  expect(screen.getByLabelText('Claude Code (Anthropic) モデル ID')).toHaveValue('claude-profile')
  rerender(<SessionProfileEditor {...props} section="environment" />)
  expect(screen.getByDisplayValue('KEEP_ME')).toBeInTheDocument()
  expect(screen.queryByDisplayValue('CODEX_MODEL')).not.toBeInTheDocument()
  expect(screen.queryByDisplayValue('ANTHROPIC_MODEL')).not.toBeInTheDocument()
})

it('does not prefill agent-specific fields from the legacy shared model', () => {
  render(<SessionProfileEditor section="models" onClose={vi.fn()} onSuccess={vi.fn()} editingProfile={{ id: 'profile', name: 'Legacy', created_at: '', updated_at: '', config: { params: { model: 'legacy-model' } } }} />)
  expect(screen.getByLabelText('Codex モデル ID')).toHaveValue('')
  expect(screen.getByLabelText('Claude Code (Anthropic) モデル ID')).toHaveValue('')
})

it('saves profile files registered in the files section', async () => {
  render(<SessionProfileEditor section="files" onClose={vi.fn()} onSuccess={vi.fn()} editingProfile={{
    id: 'profile', name: 'Files', created_at: '', updated_at: '',
    config: { files: [{ name: 'Existing', path: '/home/agentapi/keep.txt', content: 'keep' }] },
  }} />)
  fireEvent.click(screen.getByRole('button', { name: 'ファイルを追加' }))
  const nameInputs = screen.getAllByLabelText('表示名')
  const pathInputs = screen.getAllByLabelText('配置パス')
  const contentInputs = screen.getAllByLabelText('内容')
  const permissionInputs = screen.getAllByLabelText('パーミッション')
  fireEvent.change(nameInputs[1], { target: { value: 'SSH key' } })
  fireEvent.change(pathInputs[1], { target: { value: '/home/agentapi/.ssh/id_ed25519' } })
  fireEvent.change(contentInputs[1], { target: { value: 'private-key' } })
  fireEvent.change(permissionInputs[1], { target: { value: '0600' } })
  fireEvent.submit(pathInputs[1].closest('form')!)
  await waitFor(() => expect(mocks.update).toHaveBeenCalledTimes(1))
  expect(mocks.update.mock.calls[0][1].config.files).toEqual([
    { name: 'Existing', path: '/home/agentapi/keep.txt', content: 'keep', permissions: undefined },
    { name: 'SSH key', path: '/home/agentapi/.ssh/id_ed25519', content: 'private-key', permissions: '0600' },
  ])
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
