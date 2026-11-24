import { Session } from '../../types/agentapi'
import {
  getSessionAnnotations,
  getSessionDescription,
  getURLNumber,
  isNewSession,
  isOldSession,
} from './sessionListPresentation'
import type { SessionListState } from './useSessionListViewState'
export function buildSessionListRowState(context: SessionListState & { session: Session }) {
  const {
    session,
    selectedSessions,
    getAgentStatusForSession,
  } = context

  const isNew = isNewSession(session.started_at)
  const agentStatusInfo = getAgentStatusForSession(session)
  const description = getSessionDescription(session)
  const isOld = isOldSession(session)
  const annotations = getSessionAnnotations(session)
  const prNumber = annotations.prUrl ? getURLNumber(annotations.prUrl) : null
  const issueNumber = annotations.issueUrl ? getURLNumber(annotations.issueUrl) : null

  const isSelected = selectedSessions.has(session.session_id)

  return { ...context, isNew, agentStatusInfo, description, isOld, annotations, prNumber, issueNumber, isSelected }
}
export type SessionListRowState = ReturnType<typeof buildSessionListRowState>
