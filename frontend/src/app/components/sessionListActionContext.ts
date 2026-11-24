import type { Dispatch, SetStateAction } from 'react'
import type { ACPServerClient } from '../../lib/acp-server-client'
import type { AgentAPIProxyClient } from '../../lib/agentapi-proxy-client'
import type { Session } from '../../types/agentapi'

type Setter<T> = Dispatch<SetStateAction<T>>

export interface SessionListActionContext {
  agentAPI: AgentAPIProxyClient
  acpClient: ACPServerClient
  acpMode: boolean
  selectedSessions: Set<string>
  templateTarget: Session | null
  templateName: string
  templateDescription: string
  setDeletingSession: Setter<string | null>
  setSuspendingSession: Setter<string | null>
  setError: Setter<string | null>
  setSuccess: Setter<string | null>
  setSessions: Setter<Session[]>
  setTemplateTarget: Setter<Session | null>
  setIsBulkDeleting: Setter<boolean>
  setSelectedSessions: Setter<Set<string>>
  fetchSessions: (silent?: boolean) => Promise<void>
  onSessionsUpdate: () => void
}
