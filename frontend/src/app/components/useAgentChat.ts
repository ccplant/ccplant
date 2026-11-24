'use client'

import { useRouter, useSearchParams } from 'next/navigation'
import { AgentAPIChatProps } from './agentChatPresentation'
import { useAgentChatClients } from './useAgentChatClients'
import { useAgentChatCommandHandlers } from './useAgentChatCommandHandlers'
import { useAgentChatDialogs } from './useAgentChatDialogs'
import { useAgentChatInitializationEffect } from './useAgentChatInitializationEffect'
import { useAgentChatAnnotations, useAgentChatSidebar } from './useAgentChatMetadata'
import { useAgentChatRuntime } from './useAgentChatRuntime'
import { useAgentChatSecrets } from './useAgentChatSecrets'
import { useAgentChatACPState, useAgentChatConnectionState, useAgentChatMessageState } from './useAgentChatState'
import { useChatMessageLibrary } from './useChatMessageLibrary'

export function useAgentChat({ sessionId: propSessionId }: AgentAPIChatProps = {}) {
  const searchParams = useSearchParams()
  const router = useRouter()
  const sessionId = propSessionId || searchParams.get('session')
  const clients = useAgentChatClients()
  const messages = useAgentChatMessageState()
  const connection = useAgentChatConnectionState()
  const acp = useAgentChatACPState()
  const dialogs = useAgentChatDialogs()
  const messageLibrary = useChatMessageLibrary()
  const annotations = useAgentChatAnnotations(messages.isInitialLoadComplete, sessionId, clients.agentAPIRef)
  const runtime = useAgentChatRuntime({ sessionId, clients, messages, connection, acp, dialogs, annotations })
  const commands = useAgentChatCommandHandlers({
    sessionId, clients, messages, connection, acpInfo: acp.acpInfo,
    agentType: acp.agentType, acpNextPromptId: acp.acpNextPromptId, messageLibrary,
  })
  useAgentChatInitializationEffect({ sessionId, clients, messages, connection, acp, dialogs, runtime })
  const secrets = useAgentChatSecrets(sessionId, clients.agentAPIRef, connection.setError)
  const sidebar = useAgentChatSidebar()
  return {
    ...clients, ...messages, ...connection, ...acp, ...secrets, ...sidebar,
    ...dialogs, ...messageLibrary, ...annotations, ...runtime, ...commands,
    searchParams, router, sessionId,
  }
}
