'use client'

import { useEffect, useState } from 'react'
import { Session } from '../../types/agentapi'
import { SessionCardActions, SessionCardBody, SessionSelectButton, getSessionAnnotations } from './SessionCardParts'

export interface SessionCardProps { session: Session; onDelete?: (sessionId: string) => Promise<void>; isDeleting?: boolean; isSelected?: boolean; onToggleSelect?: (sessionId: string) => void; isSelectionMode?: boolean }

function useIsMobile() {
  const [isMobile, setIsMobile] = useState(false)
  useEffect(() => { const check = () => setIsMobile(window.innerWidth < 640); check(); window.addEventListener('resize', check); return () => window.removeEventListener('resize', check) }, [])
  return isMobile
}

function isOldSession(session: Session) {
  const threshold = new Date(); threshold.setDate(threshold.getDate() - 3)
  const startedAt = new Date(session.started_at)
  return startedAt < threshold && new Date(session.updated_at || session.started_at) < threshold
}

export default function SessionCard(props: SessionCardProps) {
  const { session, isSelected, onToggleSelect, isSelectionMode } = props
  const isMobile = useIsMobile()
  const annotations = getSessionAnnotations(session)
  const select = isSelectionMode && onToggleSelect ? () => onToggleSelect(session.session_id) : undefined
  return <div onClick={select} className={`group border-b border-gray-200 dark:border-gray-700 px-3 py-4 sm:px-4 transition-colors duration-150 ${isSelectionMode ? 'cursor-pointer' : ''} ${isSelected ? 'bg-blue-50 dark:bg-blue-900/20 border-l-2 border-l-blue-500' : 'hover:bg-gray-50 dark:hover:bg-gray-800/50'}`} style={isOldSession(session) ? { filter: 'grayscale(50%)', opacity: 0.7 } : {}}>
    <div className="flex gap-3">
      <SessionSelectButton session={session} selected={Boolean(isSelected)} selectionMode={Boolean(isSelectionMode)} onToggle={onToggleSelect} />
      <div className="flex flex-col sm:flex-row sm:items-start sm:justify-between space-y-3 sm:space-y-0 flex-1 min-w-0">
        <SessionCardBody session={session} annotations={annotations} isMobile={isMobile} />
        <SessionCardActions {...props} annotations={annotations} />
      </div>
    </div>
  </div>
}
