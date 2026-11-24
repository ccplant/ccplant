'use client'

import { useCallback, useEffect, useRef, useState } from 'react'

import { useTeamScope } from '../../contexts/TeamScopeContext'
import { agentAPI } from '../../lib/api'
import { extractSessionTags, fallbackTags, SidebarTag } from './tagFilterData'

export function useTagFilterSidebar(isVisible: boolean, onToggleVisibility?: () => void) {
  const { selectedTeam, isLoading: scopeLoading } = useTeamScope()
  const [tags, setTags] = useState<SidebarTag[]>([])
  const [loading, setLoading] = useState(true)
  const [expandedTags, setExpandedTags] = useState<Set<string>>(new Set())
  const requestedScopeRef = useRef<string | null>(null)
  requestedScopeRef.current = scopeLoading ? null : selectedTeam ? `team:${selectedTeam}` : 'user'

  const fetchTags = useCallback(async () => {
    if (scopeLoading) return
    const requestedScope = selectedTeam ? `team:${selectedTeam}` : 'user'
    setLoading(true)
    try {
      const scope = selectedTeam ? { scope: 'team' as const, team_id: selectedTeam } : { scope: 'user' as const }
      const response = await agentAPI.search!({ limit: 1000, ...scope })
      if (requestedScopeRef.current === requestedScope) {
        setTags(extractSessionTags(response.sessions || []))
      }
    } catch (error) {
      if (requestedScopeRef.current === requestedScope) {
        console.error('Failed to fetch tags:', error)
        setTags(fallbackTags)
      }
    } finally {
      if (requestedScopeRef.current === requestedScope) setLoading(false)
    }
  }, [scopeLoading, selectedTeam])

  useEffect(() => { void fetchTags() }, [fetchTags])

  useEffect(() => {
    const handleKeyDown = (event: KeyboardEvent) => {
      if (event.key === 'Escape' && isVisible) onToggleVisibility?.()
    }
    if (isVisible) document.addEventListener('keydown', handleKeyDown)
    return () => document.removeEventListener('keydown', handleKeyDown)
  }, [isVisible, onToggleVisibility])

  const toggleTag = (tagKey: string) => {
    setExpandedTags((current) => {
      const next = new Set(current)
      if (next.has(tagKey)) next.delete(tagKey)
      else next.add(tagKey)
      return next
    })
  }

  return { expandedTags, fetchTags, loading, tags, toggleTag }
}
