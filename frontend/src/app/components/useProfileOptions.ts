'use client'

import { useEffect } from 'react'

import { useTeamScope } from '../../contexts/TeamScopeContext'
import { createAgentAPIProxyClientFromStorage } from '../../lib/agentapi-proxy-client'

import type { ProfileEditorState } from './profileEditorState'

type ScopeParams = ReturnType<typeof useTeamScope>['getScopeParams']
export function useProfileOptions(
  state: ProfileEditorState,
  scope: 'user' | 'team' | undefined,
  teamId: string | undefined,
  getScopeParams: ScopeParams
) {
  const {
    setAvailablePools,
    setAvailableProfiles,
    setAvailableSecrets,
    setLinkedConnections,
    setSandboxPolicies,
    settingsTeamId,
  } = state
  useEffect(() => {
    const fetchConfigOptions = async () => {
      const client = createAgentAPIProxyClientFromStorage()
      try {
        const response = await client.getSandboxPolicies(
          scope ? { scope, ...(teamId ? { team_id: teamId } : {}) } : getScopeParams()
        )
        setSandboxPolicies(response.sandbox_policies || [])
      } catch {
        setSandboxPolicies([])
      }
      try {
        setAvailablePools(await client.getAvailableSessionPools())
      } catch {
        setAvailablePools([])
      }
      try {
        const response = await client.getSessionProfiles(
          scope ? { scope, ...(teamId ? { team_id: teamId } : {}) } : getScopeParams()
        )
        setAvailableProfiles(response.session_profiles || [])
      } catch {
        setAvailableProfiles([])
      }
    }
    fetchConfigOptions()
  }, [getScopeParams, scope, setAvailablePools, setAvailableProfiles, setSandboxPolicies, teamId])

  useEffect(() => {
    const loadSecrets = async () => {
      try {
        const client = createAgentAPIProxyClientFromStorage()
        const owner = settingsTeamId || (scope === 'team' ? teamId : (await client.getUserInfo()).principal_id)
        setAvailableSecrets(owner ? await client.listSettingsSecrets(owner) : [])
      } catch {
        setAvailableSecrets([])
      }
    }
    void loadSecrets()
  }, [scope, teamId, settingsTeamId, setAvailableSecrets])

  useEffect(() => {
    createAgentAPIProxyClientFromStorage()
      .listGitHubIdentities()
      .then((result) => setLinkedConnections(result.identities))
      .catch(() => setLinkedConnections([]))
  }, [setLinkedConnections])
}
