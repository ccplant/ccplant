import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import SessionProfileListView from '../SessionProfileListView'

const mocks = vi.hoisted(() => ({
  getScopeParams: vi.fn<() => { scope: 'user' | 'team'; team_id?: string }>(() => ({ scope: 'user' })),
  getSessionProfiles: vi.fn(),
  getUserInfo: vi.fn(),
  getSettings: vi.fn(),
  saveSettings: vi.fn(),
  transferResource: vi.fn(),
  isTeamScope: false,
}))

vi.mock('../../../contexts/TeamScopeContext', () => ({
  useTeamScope: () => ({ getScopeParams: mocks.getScopeParams, isTeamScope: mocks.isTeamScope }),
}))

vi.mock('../../../lib/agentapi-proxy-client', () => ({
  createAgentAPIProxyClientFromStorage: () => mocks,
}))

beforeEach(() => {
  mocks.getSessionProfiles.mockResolvedValue({
    session_profiles: [{
      id: 'team-profile', name: 'Shared Profile', description: 'Team defaults', scope: 'team',
      team_id: 'acme/platform', personal_enabled: false, created_at: '', updated_at: '', config: {},
    }],
  })
  mocks.getUserInfo.mockResolvedValue({ principal_id: 'alice', username: 'alice', teams: ['acme/platform'] })
  mocks.getSettings.mockResolvedValue({ enabled_team_session_profile_ids: [] })
  mocks.saveSettings.mockResolvedValue({ enabled_team_session_profile_ids: ['team-profile'] })
  mocks.transferResource.mockResolvedValue({ status: 'transferred' })
  mocks.isTeamScope = false
  vi.stubGlobal('alert', vi.fn())
})

afterEach(() => {
  cleanup()
  vi.clearAllMocks()
  vi.unstubAllGlobals()
})

describe('SessionProfileListView personal team profiles', () => {
  it('stores the opt-in in personal settings', async () => {
    render(<SessionProfileListView onProfileEdit={vi.fn()} />)

    const checkbox = await screen.findByRole('checkbox', { name: /Shared Profile/ })
    expect(mocks.getSessionProfiles).toHaveBeenCalledWith({
      scope: 'user', include_available_team_profiles: true,
    })
    fireEvent.click(checkbox)

    await waitFor(() => expect(mocks.saveSettings).toHaveBeenCalledWith('alice', {
      enabled_team_session_profile_ids: ['team-profile'],
    }))
    expect(checkbox).toBeChecked()
  })

  it('transfers a personal profile to a selected team after validation', async () => {
    mocks.getSessionProfiles
      .mockResolvedValueOnce({ session_profiles: [{ id: 'personal-profile', name: 'Personal', scope: 'user', created_at: '', updated_at: '', config: {} }] })
      .mockResolvedValueOnce({ session_profiles: [] })
    mocks.getUserInfo.mockResolvedValue({ principal_id: 'alice', username: 'alice', teams: ['acme/platform'] })

    render(<SessionProfileListView onProfileEdit={vi.fn()} />)
    fireEvent.click(await screen.findByRole('button', { name: '移管' }))
    fireEvent.change(screen.getByRole('combobox', { name: '移管先チーム' }), { target: { value: 'acme/platform' } })
    fireEvent.click(screen.getByRole('button', { name: '移管する' }))

    await waitFor(() => expect(mocks.transferResource).toHaveBeenNthCalledWith(1, {
      resource_type: 'session_profile', resource_id: 'personal-profile', target_scope: 'team', target_team_id: 'acme/platform', dry_run: true,
    }))
    expect(mocks.transferResource).toHaveBeenNthCalledWith(2, {
      resource_type: 'session_profile', resource_id: 'personal-profile', target_scope: 'team', target_team_id: 'acme/platform',
    })
  })

  it('lets an administrator transfer a team profile to personal scope', async () => {
    mocks.isTeamScope = true
    mocks.getScopeParams.mockReturnValue({ scope: 'team', team_id: 'acme/platform' })
    mocks.getSessionProfiles
      .mockResolvedValueOnce({ session_profiles: [{ id: 'team-profile', name: 'Shared', scope: 'team', team_id: 'acme/platform', created_at: '', updated_at: '', config: {} }] })
      .mockResolvedValueOnce({ session_profiles: [] })
    mocks.getUserInfo.mockResolvedValue({ principal_id: 'alice', username: 'alice', teams: ['acme/platform'], is_admin: true })

    render(<SessionProfileListView onProfileEdit={vi.fn()} />)
    fireEvent.click(await screen.findByRole('button', { name: '移管' }))
    fireEvent.click(screen.getByRole('button', { name: '移管する' }))

    await waitFor(() => expect(mocks.transferResource).toHaveBeenNthCalledWith(1, {
      resource_type: 'session_profile', resource_id: 'team-profile', target_scope: 'user', dry_run: true,
    }))
    expect(mocks.transferResource).toHaveBeenNthCalledWith(2, {
      resource_type: 'session_profile', resource_id: 'team-profile', target_scope: 'user',
    })
  })
})
