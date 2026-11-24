'use client'

import { useCallback, useEffect, useMemo, useState } from 'react'

import { useTeamScope } from '@/contexts/TeamScopeContext'
import { useToast } from '@/contexts/ToastContext'
import { createAgentAPIProxyClientFromStorage } from '@/lib/agentapi-proxy-client'
import { SciaIntegration, SciaIntegrationsResponse } from '@/types/settings'

import {
  defaultScopeGroups, fallbackScopeForGroup, scopeIDsForSelectedGroups, type ScopeGroup,
} from './integrationScopes'

type SelectedScopeGroups = Record<string, Record<string, string>>

export function useIntegrationsPage() {
  const [integrations, setIntegrations] = useState<SciaIntegrationsResponse | null>(null)
  const [selectedScopeGroups, setSelectedScopeGroups] = useState<SelectedScopeGroups>({})
  const [selectedIntegrationID, setSelectedIntegrationID] = useState<string | null>(null)
  const [sidebarVisible, setSidebarVisible] = useState(false)
  const [loading, setLoading] = useState(true)
  const [connectingIntegrationID, setConnectingIntegrationID] = useState<string | null>(null)
  const [revokingIntegrationID, setRevokingIntegrationID] = useState<string | null>(null)
  const [error, setError] = useState<string | null>(null)
  const { showToast } = useToast()
  const { isTeamScope, isLoading: isTeamScopeLoading } = useTeamScope()

  const loadIntegrations = useCallback(async () => {
    setLoading(true)
    setError(null)
    try {
      const response = await createAgentAPIProxyClientFromStorage().getIntegrations()
      setIntegrations(response)
      setSelectedScopeGroups((current) => {
        const next = { ...current }
        for (const integration of response.integrations || []) {
          if (!next[integration.id]) next[integration.id] = defaultScopeGroups(integration)
        }
        return next
      })
    } catch (loadError) {
      console.error('Failed to load integrations:', loadError)
      setError('Integrations を読み込めませんでした')
    } finally {
      setLoading(false)
    }
  }, [])

  useEffect(() => {
    const params = new URLSearchParams(window.location.search)
    const provider = params.get('integration_connected')
    if (!provider) return
    showToast(`${provider} 連携が完了しました`, 'success')
    window.history.replaceState(null, '', '/integrations')
  }, [showToast])

  useEffect(() => {
    if (isTeamScopeLoading) return
    if (isTeamScope) {
      setLoading(false)
      setError(null)
      return
    }
    void loadIntegrations()
  }, [isTeamScope, isTeamScopeLoading, loadIntegrations])

  const visibleIntegrations = useMemo(
    () => (integrations?.integrations || []).filter((integration) => integration.released),
    [integrations],
  )

  useEffect(() => {
    if (!visibleIntegrations.length) return setSelectedIntegrationID(null)
    if (!selectedIntegrationID || !visibleIntegrations.some(({ id }) => id === selectedIntegrationID)) {
      setSelectedIntegrationID(visibleIntegrations[0].id)
    }
  }, [selectedIntegrationID, visibleIntegrations])

  const selectedIntegration = useMemo(
    () => visibleIntegrations.find(({ id }) => id === selectedIntegrationID) || null,
    [selectedIntegrationID, visibleIntegrations],
  )

  const selectIntegration = (integrationID: string) => {
    setSelectedIntegrationID(integrationID)
    setSidebarVisible(false)
  }

  const setScopeGroup = (integration: SciaIntegration, groupID: string, scopeID: string) => {
    setSelectedScopeGroups((current) => ({
      ...current,
      [integration.id]: { ...(current[integration.id] || {}), [groupID]: scopeID },
    }))
  }

  const setScopeGroupAllowed = (integration: SciaIntegration, group: ScopeGroup, allowed: boolean) => {
    setScopeGroup(integration, group.id, allowed ? fallbackScopeForGroup(group) : '__none__')
  }

  const startOAuth = async (integration: SciaIntegration) => {
    setConnectingIntegrationID(integration.id)
    try {
      const response = await createAgentAPIProxyClientFromStorage().createIntegrationAuthorizationURL(integration.id, {
        redirect_uri: `${window.location.origin}/api/oauth/${integration.provider}/callback`,
        scope_ids: scopeIDsForSelectedGroups(integration, selectedScopeGroups[integration.id] || defaultScopeGroups(integration)),
      })
      window.location.href = response.authorization_url
    } catch (oauthError) {
      console.error('Failed to create authorization URL:', oauthError)
      showToast('OAuth URL を作成できませんでした', 'error')
      setConnectingIntegrationID(null)
    }
  }

  const revokeIntegration = async (integration: SciaIntegration) => {
    if (!integration.connected || !window.confirm(`${integration.name} の連携を解除しますか？`)) return
    setRevokingIntegrationID(integration.id)
    try {
      await createAgentAPIProxyClientFromStorage().revokeIntegration(integration.id)
      showToast(`${integration.name} 連携を解除しました`, 'success')
      await loadIntegrations()
    } catch (revokeError) {
      console.error('Failed to revoke integration:', revokeError)
      showToast('連携を解除できませんでした', 'error')
    } finally {
      setRevokingIntegrationID(null)
    }
  }

  return {
    connectingIntegrationID, error, integrations, isTeamScope, isTeamScopeLoading,
    loadIntegrations, loading, revokeIntegration, revokingIntegrationID,
    selectIntegration, selectedIntegration, selectedIntegrationID, selectedScopeGroups,
    setScopeGroup, setScopeGroupAllowed, setSidebarVisible, sidebarVisible, startOAuth,
    visibleIntegrations,
  }
}

export type IntegrationsPageModel = ReturnType<typeof useIntegrationsPage>
