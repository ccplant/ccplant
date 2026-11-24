'use client'

import { useRouter } from 'next/navigation'
import { useCallback, useEffect, useRef, useState } from 'react'

import { useTeamScope } from '../../contexts/TeamScopeContext'
import { createAgentAPIProxyClientFromStorage, ProxySessionStatusEvent } from '../../lib/agentapi-proxy-client'
import { Session, SessionStatus } from '../../types/agentapi'
import { useSessionsStatusStream } from '../hooks/useSessionsStatusStream'

export function useSessionListSidebar(currentSessionId: string, isVisible: boolean) {
  const router = useRouter()
  const { selectedTeam, isLoading: scopeLoading } = useTeamScope()
  const [client] = useState(() => createAgentAPIProxyClientFromStorage())
  const [sessions, setSessions] = useState<Session[]>([])
  const sessionsRef = useRef<Session[]>([])
  const requestedScopeRef = useRef<string | null>(null)
  requestedScopeRef.current = scopeLoading ? null : selectedTeam ? `team:${selectedTeam}` : 'user'
  const [loading, setLoading] = useState(true)
  const [deletingIds, setDeletingIds] = useState<Set<string>>(new Set())

  const fetchSessions = useCallback(async () => {
    if (scopeLoading) return
    const requestedScope = selectedTeam ? `team:${selectedTeam}` : 'user'
    try {
      const scopeParams: { scope: 'user' | 'team'; team_id?: string } = selectedTeam
        ? { scope: 'team', team_id: selectedTeam }
        : { scope: 'user' }
      const response = await client.search({ limit: 100, ...scopeParams })
      const sorted = (response.sessions || []).sort(
        (first, second) => new Date(second.updated_at || second.started_at).getTime() - new Date(first.updated_at || first.started_at).getTime()
      )
      if (requestedScopeRef.current !== requestedScope) return
      sessionsRef.current = sorted
      setSessions(sorted)
    } catch (error) {
      if (requestedScopeRef.current === requestedScope) {
        console.error('[SessionListSidebar] Failed to fetch sessions:', error)
      }
    } finally {
      if (requestedScopeRef.current === requestedScope) setLoading(false)
    }
  }, [client, scopeLoading, selectedTeam])

  const handleStatus = useCallback((event: ProxySessionStatusEvent) => {
    const current = sessionsRef.current.find((session) => session.session_id === event.session_id)
    const statusChanged = current !== undefined && current.status !== event.status
    if (current) {
      sessionsRef.current = sessionsRef.current.map((session) =>
        session.session_id === event.session_id ? { ...session, status: event.status as SessionStatus } : session
      )
    }
    setSessions((previous) => previous.map((session) =>
      session.session_id === event.session_id ? { ...session, status: event.status as SessionStatus } : session
    ))
    if (statusChanged && ['active', 'stopped', 'error', 'timeout'].includes(event.status)) {
      void fetchSessions()
    }
  }, [fetchSessions])

  useSessionsStatusStream({ client, onStatusChange: handleStatus, enabled: isVisible })

  useEffect(() => {
    void fetchSessions()
    if (!isVisible) return
    const interval = setInterval(fetchSessions, 30000)
    return () => clearInterval(interval)
  }, [fetchSessions, isVisible])

  const handleDelete = useCallback(async (event: React.MouseEvent, sessionId: string) => {
    event.stopPropagation()
    if (!window.confirm('このセッションを削除しますか？')) return
    setDeletingIds((previous) => new Set(previous).add(sessionId))
    try {
      await createAgentAPIProxyClientFromStorage().delete(sessionId)
      setSessions((previous) => previous.filter((session) => session.session_id !== sessionId))
      if (sessionId === currentSessionId) router.push('/chats')
    } catch (error) {
      console.error('[SessionListSidebar] Failed to delete session:', error)
    } finally {
      setDeletingIds((previous) => {
        const next = new Set(previous)
        next.delete(sessionId)
        return next
      })
    }
  }, [currentSessionId, router])

  return { deletingIds, handleDelete, loading, sessions }
}
