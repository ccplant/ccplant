'use client'

import { useState, useEffect, useCallback, useMemo, type KeyboardEvent } from 'react'
import { useSearchParams, useRouter } from 'next/navigation'
import type { Session, SessionListParams } from '../../types/agentapi'
import { createAgentAPIProxyClientFromStorage, AgentAPIProxyError } from '../../lib/agentapi-proxy-client'
import { extractFilterGroups, applySessionFilters, parseFiltersFromURL, filtersToURLParams, getFilterValuesForSessionCreation, type SessionFilter } from '../../lib/filter-utils'
import { useTeamScope } from '../../contexts/TeamScopeContext'
import { useSessionList, mutateSessionList } from '../../hooks/useSessionList'
import { useSessionStatusSSE } from '../../hooks/useSessionStatusSSE'

export interface PageState { page: number; limit: number }
export type SortField = 'started_at' | 'updated_at'
export type SortOrder = 'asc' | 'desc'

function useDerivedSessions(sessions: Session[], filters: SessionFilter, sortBy: SortField, sortOrder: SortOrder, page: PageState) {
  const filterGroups = useMemo(() => extractFilterGroups(sessions), [sessions])
  const filtered = useMemo(() => applySessionFilters(sessions, filters), [sessions, filters])
  const sorted = useMemo(() => [...filtered].sort((left, right) => {
    const first = left[sortBy] || ''
    const second = right[sortBy] || ''
    if (first === second) return 0
    const order = first < second ? -1 : 1
    return sortOrder === 'asc' ? order : -order
  }), [filtered, sortBy, sortOrder])
  const paginated = useMemo(() => sorted.slice((page.page - 1) * page.limit, page.page * page.limit), [page, sorted])
  return { filterGroups, filteredSessions: sorted, paginatedSessions: paginated, totalPages: Math.ceil(sorted.length / page.limit) }
}

function useQuickStart(client: ReturnType<typeof createAgentAPIProxyClientFromStorage>, selectedTeam: string | null, filters: SessionFilter, setError: (value: string | null) => void) {
  const [message, setMessage] = useState('')
  const [creating, setCreating] = useState(false)
  const start = useCallback(async () => {
    if (!message.trim()) return
    setCreating(true); setError(null)
    try {
      const values = getFilterValuesForSessionCreation(filters)
      const scope = selectedTeam ? { scope: 'team' as const, team_id: selectedTeam } : { scope: 'user' as const }
      await client.start({ metadata: { ...values.metadata, description: message.trim() }, environment: values.environment, params: { message: message.trim() }, ...scope })
      setMessage(''); mutateSessionList()
    } catch (error) {
      setError(error instanceof AgentAPIProxyError ? `Failed to start session: ${error.message}` : 'An unexpected error occurred while starting session')
    } finally { setCreating(false) }
  }, [client, filters, message, selectedTeam, setError])
  const onKeyPress = useCallback((event: KeyboardEvent) => { if (event.key === 'Enter' && !event.shiftKey) { event.preventDefault(); void start() } }, [start])
  return { quickStartMessage: message, setQuickStartMessage: setMessage, isCreatingQuickSession: creating, handleQuickStart: start, handleQuickStartKeyPress: onKeyPress }
}

function useSessionSelection(client: ReturnType<typeof createAgentAPIProxyClientFromStorage>, visibleSessions: Session[], setError: (value: string | null) => void) {
  const [selectedSessions, setSelectedSessions] = useState<Set<string>>(new Set())
  const [deletingSession, setDeletingSession] = useState<string | null>(null)
  const [isBulkDeleting, setIsBulkDeleting] = useState(false)
  const toggleSessionSelection = useCallback((id: string) => setSelectedSessions(current => {
    const next = new Set(current); if (next.has(id)) next.delete(id); else next.add(id); return next
  }), [])
  const toggleSelectAll = useCallback(() => setSelectedSessions(current => current.size === visibleSessions.length && visibleSessions.length ? new Set() : new Set(visibleSessions.map(session => session.session_id))), [visibleSessions])
  const deleteSession = useCallback(async (id: string) => {
    if (!confirm('このセッションを削除してもよろしいですか？')) return
    setDeletingSession(id)
    try { await client.delete(id); mutateSessionList() } catch (error) { console.error('Failed to delete session:', error); setError('セッションの削除に失敗しました') } finally { setDeletingSession(null) }
  }, [client, setError])
  const deleteSelectedSessions = useCallback(async () => {
    if (!selectedSessions.size || !confirm(`${selectedSessions.size}件のセッションを削除してもよろしいですか？`)) return
    setIsBulkDeleting(true); setError(null)
    try { await client.deleteBatch([...selectedSessions]); setSelectedSessions(new Set()); mutateSessionList() } catch (error) { console.error('Failed to bulk delete sessions:', error); setError('セッションの一括削除に失敗しました') } finally { setIsBulkDeleting(false) }
  }, [client, selectedSessions, setError])
  return { selectedSessions, setSelectedSessions, deletingSession, isBulkDeleting, toggleSessionSelection, toggleSelectAll, deleteSession, deleteSelectedSessions }
}

export function useConversationList() {
  const searchParams = useSearchParams()
  const router = useRouter()
  const { selectedTeam, isLoading: isTeamScopeLoading } = useTeamScope()

  // Extract repository from query parameters
  const repositoryParam = searchParams.get('repository')

  // Create global API client
  const [agentAPI] = useState(() => createAgentAPIProxyClientFromStorage(repositoryParam || undefined))

  const [sessionFilters, setSessionFilters] = useState<SessionFilter>(() => parseFiltersFromURL(searchParams))
  const [pageState, setPageState] = useState<PageState>({
    page: 1,
    limit: 50 // Increased limit for better UX with SWR caching
  })
  const [showNewConversationModal, setShowNewConversationModal] = useState(false)
  const [isSelectionMode, setIsSelectionMode] = useState(false)
  const [sidebarVisible, setSidebarVisible] = useState(false)
  const [sortBy, setSortBy] = useState<SortField>(() => {
    if (typeof window !== 'undefined') {
      return (localStorage.getItem('conversationListSortBy') as SortField) || 'updated_at'
    }
    return 'updated_at'
  })
  const [sortOrder, setSortOrder] = useState<SortOrder>(() => {
    if (typeof window !== 'undefined') {
      return (localStorage.getItem('conversationListSortOrder') as SortOrder) || 'desc'
    }
    return 'desc'
  })

  // Compute scope parameters for SWR hook
  const scopeParams: SessionListParams = useMemo(() => {
    return selectedTeam
      ? { scope: 'team', team_id: selectedTeam, limit: 1000 }
      : { scope: 'user', limit: 1000 }
  }, [selectedTeam])

  // Use SWR hook for data fetching with caching
  const { sessions: allSessions, isLoading, isError, error: swrError, mutate } = useSessionList(isTeamScopeLoading ? null : scopeParams, agentAPI, {
    revalidateOnFocus: true,
    revalidateOnReconnect: true,
    refreshInterval: 30000, // Refresh every 30 seconds
  })

  // Local error state for user actions (create, delete, etc.)
  const [localError, setLocalError] = useState<string | null>(null)

  // Convert SWR error to display message
  const error = localError || (isError && swrError
    ? swrError instanceof AgentAPIProxyError
      ? `Failed to load sessions: ${swrError.message}`
      : 'An unexpected error occurred while loading sessions'
    : null)

  const { filterGroups, filteredSessions, paginatedSessions, totalPages } = useDerivedSessions(allSessions, sessionFilters, sortBy, sortOrder, pageState)

  // Loading state from SWR
  const loading = isTeamScopeLoading || isLoading

  // Refresh sessions using SWR mutate
  const fetchSessions = useCallback(() => {
    mutate()
  }, [mutate])

  // Subscribe to real-time session status updates via SSE
  useSessionStatusSSE({
    enabled: true,
    client: agentAPI,
    onStatusChange: (event) => {
      console.log('[ConversationList] Session status update:', event)
    },
    onConnectionChange: (connected) => {
      if (connected) {
        console.log('[ConversationList] SSE connected')
      } else {
        console.log('[ConversationList] SSE disconnected')
      }
    },
  })

  // Update URL when filters change
  const updateURL = useCallback((filters: SessionFilter) => {
    const params = filtersToURLParams(filters)
    const newURL = params.toString() ? `?${params.toString()}` : '/chats'
    router.replace(newURL, { scroll: false })
  }, [router])

  const handleFiltersChange = (newFilters: SessionFilter) => {
    setSessionFilters(newFilters)
    setPageState(prev => ({ ...prev, page: 1 })) // Reset to first page
    updateURL(newFilters)
  }

  const handlePageChange = (newPage: number) => {
    setPageState(prev => ({ ...prev, page: newPage }))
  }

  const handleNewConversationSuccess = () => {
    // Refresh the session list to show the new session
    mutateSessionList()
  }

  const quickStart = useQuickStart(agentAPI, selectedTeam, sessionFilters, setLocalError)
  const selection = useSessionSelection(agentAPI, paginatedSessions, setLocalError)
  const { quickStartMessage, setQuickStartMessage, isCreatingQuickSession, handleQuickStart, handleQuickStartKeyPress } = quickStart
  const { selectedSessions, setSelectedSessions, deletingSession, isBulkDeleting, toggleSessionSelection, toggleSelectAll, deleteSession, deleteSelectedSessions } = selection

  // Initialize filters from URL on mount
  useEffect(() => {
    const urlFilters = parseFiltersFromURL(searchParams)
    setSessionFilters(urlFilters)
  }, [searchParams])

  // ソート設定を localStorage に保存
  useEffect(() => {
    if (typeof window !== 'undefined') {
      localStorage.setItem('conversationListSortBy', sortBy)
    }
  }, [sortBy])

  useEffect(() => {
    if (typeof window !== 'undefined') {
      localStorage.setItem('conversationListSortOrder', sortOrder)
    }
  }, [sortOrder])


  return {
    repositoryParam, sessionFilters, pageState, showNewConversationModal, setShowNewConversationModal,
    quickStartMessage, setQuickStartMessage, isCreatingQuickSession, deletingSession,
    selectedSessions, setSelectedSessions, isBulkDeleting, isSelectionMode, setIsSelectionMode,
    sidebarVisible, setSidebarVisible, sortBy, setSortBy, sortOrder, setSortOrder,
    allSessions, error, filterGroups, filteredSessions, paginatedSessions, totalPages, loading,
    fetchSessions, handleFiltersChange, handlePageChange, handleNewConversationSuccess,
    handleQuickStart, handleQuickStartKeyPress, deleteSession, toggleSessionSelection,
    toggleSelectAll, deleteSelectedSessions,
  }
}

export type ConversationListModel = ReturnType<typeof useConversationList>
