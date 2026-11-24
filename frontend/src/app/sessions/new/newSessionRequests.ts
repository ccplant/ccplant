import { createACPServerClientFromStorage } from '../../../lib/acp-server-client'
import type { AgentAPIProxyClient } from '../../../lib/agentapi-proxy-client'
import type { SessionCreationStatus } from '../../../types/sessionProgress'
import type { NewSessionDraft } from './newSessionDraft'
import { buildNewACPSessionRequest, buildNewSessionRequest } from './newSessionPayload'

export type ProgressUpdater = (status: SessionCreationStatus, errorMessage?: string) => void

export async function createRESTSession(draft: NewSessionDraft, client: AgentAPIProxyClient, update: ProgressUpdater) {
  try {
    update('creating')
    await client.start(buildNewSessionRequest(draft))
    update('completed')
    return { success: true }
  } catch (error) {
    console.error('Session creation failed:', error)
    const message = error instanceof Error ? error.message : 'セッション作成に失敗しました'
    update('failed', message)
    return { success: false, error: message }
  }
}

export async function createACPSession(draft: NewSessionDraft, update: ProgressUpdater) {
  try {
    const client = createACPServerClientFromStorage()
    await client.initialize()
    await client.createSession(buildNewACPSessionRequest(draft))
    update('completed')
    return { success: true }
  } catch (error) {
    const message = error instanceof Error ? error.message : 'ACP セッション作成に失敗しました'
    update('failed', message)
    return { success: false, error: message }
  }
}
