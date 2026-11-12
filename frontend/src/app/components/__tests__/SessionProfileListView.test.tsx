import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import SessionProfileListView from '../SessionProfileListView'

const mocks = vi.hoisted(() => ({
  getScopeParams: vi.fn(() => ({ scope: 'user' as const })),
  getSessionProfiles: vi.fn(),
  getUserInfo: vi.fn(),
  getSettings: vi.fn(),
  saveSettings: vi.fn(),
}))

vi.mock('../../../contexts/TeamScopeContext', () => ({
  useTeamScope: () => ({ getScopeParams: mocks.getScopeParams, isTeamScope: false }),
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
})
