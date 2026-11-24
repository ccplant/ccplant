'use client'

import { useCallback, useEffect, useRef, useState } from 'react'
import { useTeamScope } from '../../contexts/TeamScopeContext'
import { ACPServerClient, createACPServerClientFromStorage } from '../../lib/acp-server-client'
import { AgentAPIProxyClient, AgentAPIProxyError, createAgentAPIProxyClientFromStorage, ProxySessionStatusEvent } from '../../lib/agentapi-proxy-client'
import { AgentStatus, Session, SessionListParams } from '../../types/agentapi'
import { getACPServerEnabled } from '../../types/settings'
import { useSessionsStatusStream } from '../hooks/useSessionsStatusStream'
import { createAgentStatusFromSession, createAgentStatusMapFromSessions, mapACPSessionToSession } from './sessionListPresentation'

export function useSessionListData() {
  const { selectedTeam, isLoading: isTeamScopeLoading } = useTeamScope()
  const [agentAPI] = useState(() => createAgentAPIProxyClientFromStorage())
  const [agentAPIProxy] = useState(() => createAgentAPIProxyClientFromStorage())
  const [acpClient] = useState(() => createACPServerClientFromStorage())
  const [acpMode] = useState(() => getACPServerEnabled())
  const [sessions, setSessions] = useState<Session[]>([])
  const sessionsRef = useRef<Session[]>([])
  const requestedScopeRef = useRef<string | null>(null)
  requestedScopeRef.current = isTeamScopeLoading ? null : selectedTeam ? `team:${selectedTeam}` : 'user'
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)
  const [success, setSuccess] = useState<string | null>(null)
  const [sessionAgentStatus, setSessionAgentStatus] = useState<Record<string, AgentStatus>>({})
  const fetchSessions = useCallback(async (silent = false) => {
    if (isTeamScopeLoading) return
    const requestedScope = selectedTeam ? `team:${selectedTeam}` : 'user'
    try {
      if (!silent) setLoading(true)
      setError(null)
      const list = await loadSessionList({ acpMode, acpClient, agentAPI, selectedTeam })
      if (requestedScopeRef.current !== requestedScope) return
      sessionsRef.current = list
      setSessions(list); setSessionAgentStatus(createAgentStatusMapFromSessions(list))
    } catch (reason) {
      if (requestedScopeRef.current !== requestedScope) return
      setError(sessionLoadError(reason)); setSessions([]); setSessionAgentStatus({})
    } finally { if (requestedScopeRef.current === requestedScope) setLoading(false) }
  }, [acpMode, acpClient, agentAPI, isTeamScopeLoading, selectedTeam])
  const handleStatus = useCallback((event: ProxySessionStatusEvent) => {
    const current = sessionsRef.current.find((session) => session.session_id === event.session_id)
    const changed = current !== undefined && current.status !== event.status
    sessionsRef.current = updateSessionStatus(sessionsRef.current, event)
    setSessions((previous) => updateSessionStatus(previous, event))
    setSessionAgentStatus((previous) => ({ ...previous, [event.session_id]: createAgentStatusFromSession({ status: event.status as Session['status'], updated_at: event.timestamp }) }))
    if (changed && ['active', 'error', 'timeout'].includes(event.status)) void fetchSessions(true)
  }, [fetchSessions])
  useSessionsStatusStream({ client: agentAPIProxy, onStatusChange: handleStatus })
  useEffect(() => { void fetchSessions() }, [fetchSessions])
  return { agentAPI, acpClient, acpMode, sessions, setSessions, loading, error, setError, success, setSuccess, sessionAgentStatus, fetchSessions }
}

async function loadSessionList(input: { acpMode: boolean; acpClient: ACPServerClient; agentAPI: AgentAPIProxyClient; selectedTeam: string | null }): Promise<Session[]> {
  if (input.acpMode) return ((await input.acpClient.listSessions()).sessions || []).map(mapACPSessionToSession)
  const scope: { scope: 'user' | 'team'; team_id?: string } = input.selectedTeam ? { scope: 'team', team_id: input.selectedTeam } : { scope: 'user' }
  const params: SessionListParams = { limit: 1000, ...scope }
  return (await input.agentAPI.search!(params)).sessions || []
}

function updateSessionStatus(sessions: Session[], event: ProxySessionStatusEvent): Session[] {
  const index = sessions.findIndex((session) => session.session_id === event.session_id)
  if (index === -1) return sessions
  const updated = [...sessions]
  updated[index] = { ...updated[index], status: event.status as Session['status'] }
  return updated
}

function sessionLoadError(reason: unknown): string {
  if (reason instanceof AgentAPIProxyError) return `セッション一覧の取得に失敗しました: ${reason.message}`
  return 'セッション一覧の取得中に予期せぬエラーが発生しました'
}
