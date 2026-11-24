import type { Dispatch, SetStateAction } from 'react'
import { createAgentAPIClient } from '../../../lib/api'
import type { SessionCreationProgress } from '../../../types/sessionProgress'
import { addRepositoryToHistory, getACPServerEnabled } from '../../../types/settings'
import { InitialMessageCache } from '../../../utils/initialMessageCache'
import { recentMessagesManager } from '../../../utils/recentMessagesManager'
import type { NewSessionDraft } from './newSessionDraft'
import { createACPSession, createRESTSession, type ProgressUpdater } from './newSessionRequests'

export interface NewSessionSubmission {
  draft: NewSessionDraft
  setError: (error: string | null) => void
  setIsCreating: (value: boolean) => void
  setStatusMessage: (value: string) => void
  setCreationProgress: Dispatch<SetStateAction<SessionCreationProgress | null>>
  setShowProgressModal: (value: boolean) => void
  updateProgress: ProgressUpdater
  router: { push: (path: string) => void }
}

function saveRepository(repository: string) {
  if (!repository) return
  try {
    addRepositoryToHistory(repository)
  } catch (error) {
    console.error('Failed to add repository to history (pre-session):', error)
  }
}

export async function submitNewSession(context: NewSessionSubmission) {
  const { draft } = context
  const message = draft.initialMessage.trim()
  if (!message) {
    context.setError('初期メッセージを入力してください')
    return
  }
  context.setIsCreating(true)
  context.setError(null)
  context.setStatusMessage('')
  const acpEnabled = getACPServerEnabled()
  const client = createAgentAPIClient()
  const repository = draft.freeFormRepository.trim()
  InitialMessageCache.addMessage(message)
  await recentMessagesManager.saveMessage(message)
  saveRepository(repository)
  context.setCreationProgress({
    status: 'creating',
    message,
    repository: repository || undefined,
    startTime: new Date(),
  })
  context.setShowProgressModal(true)
  const result = acpEnabled
    ? await createACPSession(draft, context.updateProgress)
    : await createRESTSession(draft, client, context.updateProgress)
  if (result.success) setTimeout(() => context.router.push('/chats'), 1500)
  else context.setIsCreating(false)
}
