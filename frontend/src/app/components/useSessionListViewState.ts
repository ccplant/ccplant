'use client'

import { Session } from '../../types/agentapi'
import { CreatingSession, describeSessionStatus } from './sessionListPresentation'
import { useSessionListData } from './useSessionListData'
import { useSessionListOperations } from './useSessionListOperations'
import { useSessionListViewPreferences } from './useSessionListViewPreferences'

interface TagFilter { [key: string]: string[] }

export interface SessionListViewProps {
  tagFilters: TagFilter
  onSessionsUpdate: () => void
  creatingSessions?: CreatingSession[]
}

export function useSessionListViewState({ tagFilters, onSessionsUpdate, creatingSessions = [] }: SessionListViewProps) {
  const data = useSessionListData()
  const preferences = useSessionListViewPreferences(data.sessions, tagFilters)
  const operations = useSessionListOperations({
    data, paginatedSessions: preferences.paginatedSessions, onSessionsUpdate,
    closeAnnotationMenu: () => preferences.setOpenAnnotationMenuId(null),
  })
  const getAgentStatusForSession = (session: Session) => describeSessionStatus(session, data.sessionAgentStatus)
  return {
    ...data,
    ...preferences,
    ...operations,
    creatingSessions,
    getAgentStatusForSession,
  }
}

export type SessionListState = ReturnType<typeof useSessionListViewState>
