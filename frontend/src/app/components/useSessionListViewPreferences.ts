'use client'

import { useEffect, useState } from 'react'
import { Session } from '../../types/agentapi'

interface TagFilter { [key: string]: string[] }

export function useSessionListViewPreferences(sessions: Session[], tagFilters: TagFilter) {
  const [isMobile, setIsMobile] = useState(false)
  const [isSelectionMode, setIsSelectionMode] = useState(false)
  const [showHiddenSessions, setShowHiddenSessions] = useState(false)
  const [openAnnotationMenuId, setOpenAnnotationMenuId] = useState<string | null>(null)
  const [expandedErrorSessionId, setExpandedErrorSessionId] = useState<string | null>(null)
  const [sortBy, setSortBy] = useState<'started_at' | 'updated_at'>(() => storedSortBy())
  const [sortOrder, setSortOrder] = useState<'asc' | 'desc'>(() => storedSortOrder())
  useEffect(() => {
    const checkMobile = () => setIsMobile(window.innerWidth < 640)
    checkMobile(); window.addEventListener('resize', checkMobile)
    return () => window.removeEventListener('resize', checkMobile)
  }, [])
  useEffect(() => { localStorage.setItem('sessionListSortBy', sortBy) }, [sortBy])
  useEffect(() => { localStorage.setItem('sessionListSortOrder', sortOrder) }, [sortOrder])
  const hiddenSessionsCount = sessions.filter((session) => session.tags?.hidden === 'true').length
  const filteredSessions = sessions.filter((session) => sessionMatches(session, tagFilters, showHiddenSessions)).sort((left, right) => compareSessions(left, right, sortBy, sortOrder))
  const pageState = { page: 1, limit: 20 }
  const paginatedSessions = filteredSessions.slice(0, pageState.limit)
  return {
    isMobile, isSelectionMode, setIsSelectionMode, showHiddenSessions, setShowHiddenSessions,
    openAnnotationMenuId, setOpenAnnotationMenuId, expandedErrorSessionId, setExpandedErrorSessionId,
    sortBy, setSortBy, sortOrder, setSortOrder, hiddenSessionsCount, filteredSessions, pageState,
    paginatedSessions, totalPages: Math.ceil(filteredSessions.length / pageState.limit), handlePageChange: (page: number) => { void page },
  }
}

function sessionMatches(session: Session, filters: TagFilter, showHidden: boolean): boolean {
  if (session.tags?.hidden === 'true' && !showHidden) return false
  return Object.entries(filters).every(([key, values]) => {
    if (values.length === 0) return true
    const tag = session.tags?.[key]
    if (tag && values.includes(tag)) return true
    const metadata = session.metadata?.[key]
    return Boolean(metadata && values.includes(String(metadata)))
  })
}

function compareSessions(left: Session, right: Session, sortBy: 'started_at' | 'updated_at', order: 'asc' | 'desc'): number {
  const leftValue = left[sortBy] || ''
  const rightValue = right[sortBy] || ''
  if (leftValue === rightValue) return 0
  const result = leftValue < rightValue ? -1 : 1
  return order === 'asc' ? result : -result
}

function storedSortBy(): 'started_at' | 'updated_at' {
  if (typeof window === 'undefined') return 'updated_at'
  return localStorage.getItem('sessionListSortBy') === 'started_at' ? 'started_at' : 'updated_at'
}

function storedSortOrder(): 'asc' | 'desc' {
  if (typeof window === 'undefined') return 'desc'
  return localStorage.getItem('sessionListSortOrder') === 'asc' ? 'asc' : 'desc'
}
