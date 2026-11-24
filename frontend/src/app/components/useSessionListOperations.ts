'use client'

import { useState } from 'react'
import type { FormEvent } from 'react'
import { useRouter } from 'next/navigation'
import { Session } from '../../types/agentapi'
import { getSessionAnnotations } from './sessionListPresentation'
import { deleteSelectedSessionsAction, deleteSessionAction, restartSessionWithSettingsAction, suspendSessionAction, templateizeSessionAction } from './sessionListActions'
import { useSessionListData } from './useSessionListData'

interface OperationInput {
  data: ReturnType<typeof useSessionListData>
  paginatedSessions: Session[]
  onSessionsUpdate: () => void
  closeAnnotationMenu: () => void
}

export function useSessionListOperations({ data, paginatedSessions, onSessionsUpdate, closeAnnotationMenu }: OperationInput) {
  const router = useRouter()
  const [deletingSession, setDeletingSession] = useState<string | null>(null)
  const [suspendingSession, setSuspendingSession] = useState<string | null>(null)
  const [selectedSessions, setSelectedSessions] = useState<Set<string>>(new Set())
  const [isBulkDeleting, setIsBulkDeleting] = useState(false)
  const [templateTarget, setTemplateTarget] = useState<Session | null>(null)
  const [templateName, setTemplateName] = useState('')
  const [templateDescription, setTemplateDescription] = useState('')
  const context = {
    agentAPI: data.agentAPI, acpClient: data.acpClient, acpMode: data.acpMode, selectedSessions,
    templateTarget, templateName, templateDescription, setDeletingSession, setSuspendingSession,
    setError: data.setError, setSuccess: data.setSuccess, setSessions: data.setSessions, setTemplateTarget,
    setIsBulkDeleting, setSelectedSessions, fetchSessions: data.fetchSessions, onSessionsUpdate,
  }
  const openTemplateDialog = (session: Session) => {
    const suggested = getSessionAnnotations(session).description || session.tags?.repository || `Session ${session.session_id.slice(0, 8)}`
    closeAnnotationMenu()
    setTemplateTarget(session); setTemplateName(suggested); setTemplateDescription('')
  }
  const toggleSessionSelection = (id: string) => setSelectedSessions((previous) => toggleSelection(previous, id))
  const toggleSelectAll = () => setSelectedSessions(selectedSessions.size === paginatedSessions.length && paginatedSessions.length > 0 ? new Set() : new Set(paginatedSessions.map((session) => session.session_id)))
  return {
    deletingSession, suspendingSession, selectedSessions, setSelectedSessions, isBulkDeleting,
    templateTarget, setTemplateTarget, templateName, setTemplateName, templateDescription, setTemplateDescription,
    navigateToChat: (sessionId: string) => router.push(`/sessions/${sessionId}`),
    deleteSession: (id: string) => deleteSessionAction(context, id), suspendSession: (id: string) => suspendSessionAction(context, id),
    restartSessionWithSettings: (id: string) => restartSessionWithSettingsAction(context, id), openTemplateDialog,
    templateizeSession: (event: FormEvent<HTMLFormElement>) => templateizeSessionAction(context, event),
    toggleSessionSelection, toggleSelectAll, deleteSelectedSessions: () => deleteSelectedSessionsAction(context),
  }
}

function toggleSelection(previous: Set<string>, id: string): Set<string> {
  const next = new Set(previous)
  if (next.has(id)) next.delete(id); else next.add(id)
  return next
}
