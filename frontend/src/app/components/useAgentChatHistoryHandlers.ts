'use client'

import { ACPSessionInfo } from '../../lib/agentapi-proxy-client'
import { PendingAction } from '../../types/agentapi'
import { loadMoreMessagesAction, pollMessagesAction } from './agentChatHistoryActions'
import { handleAnswerSubmitAction } from './agentChatPermissionActions'
import { useAgentChatClients } from './useAgentChatClients'
import { useAgentChatDialogs } from './useAgentChatDialogs'
import { useAgentChatAnnotations } from './useAgentChatMetadata'
import { useAgentChatConnectionState, useAgentChatMessageState } from './useAgentChatState'

interface HistoryHandlerInput {
  sessionId: string | null
  clients: ReturnType<typeof useAgentChatClients>
  messages: ReturnType<typeof useAgentChatMessageState>
  connection: ReturnType<typeof useAgentChatConnectionState>
  annotations: ReturnType<typeof useAgentChatAnnotations>
  dialogs: ReturnType<typeof useAgentChatDialogs>
  agentType: string | null
  acpInfo: ACPSessionInfo | null
  acpPendingPermission: { action: PendingAction; rpcId: number } | null
  setACPPendingPermission: React.Dispatch<React.SetStateAction<{ action: PendingAction; rpcId: number } | null>>
}

export function useAgentChatHistoryHandlers(input: HistoryHandlerInput) {
  const { clients, messages, connection, annotations, dialogs } = input
  const loadMoreMessages = async () => loadMoreMessagesAction({
    sessionId: input.sessionId, agentAPIRef: clients.agentAPIRef, messages: messages.messages,
    setMessages: messages.setMessages, hasMoreMessages: messages.hasMoreMessages,
    setHasMoreMessages: messages.setHasMoreMessages, isLoadingMore: messages.isLoadingMore,
    setIsLoadingMore: messages.setIsLoadingMore, messagesContainerRef: messages.messagesContainerRef,
    lastLoadTimeRef: connection.lastLoadTimeRef,
  })
  const pollMessages = async () => pollMessagesAction({
    sessionId: input.sessionId, agentAPIRef: clients.agentAPIRef, messages: messages.messages,
    setMessages: messages.setMessages, setAgentStatus: connection.setAgentStatus, setError: connection.setError,
    isConnected: connection.isConnected, setSessionAnnotations: annotations.setSessionAnnotations,
    setSessionModelOptions: annotations.setSessionModelOptions, agentType: input.agentType,
    pendingAction: dialogs.pendingAction, setPendingAction: dialogs.setPendingAction,
    setShowQuestionModal: dialogs.setShowQuestionModal, prevAgentStatusRef: connection.prevAgentStatusRef,
    lastSessionAnnotationLoadTimeRef: annotations.lastSessionAnnotationLoadTimeRef,
  })
  const handleAnswerSubmit = async (answers: Record<string, string | string[]>) => handleAnswerSubmitAction({
    sessionId: input.sessionId, agentAPIRef: clients.agentAPIRef, acpServerEnabled: clients.acpServerEnabled,
    acpServerClientRef: clients.acpServerClientRef, setError: connection.setError,
    pendingAction: dialogs.pendingAction, setPendingAction: dialogs.setPendingAction,
    setShowQuestionModal: dialogs.setShowQuestionModal, acpInfo: input.acpInfo,
    acpPendingPermission: input.acpPendingPermission, setACPPendingPermission: input.setACPPendingPermission,
  }, answers)
  return { loadMoreMessages, pollMessages, handleAnswerSubmit, handleQuestionModalClose: () => dialogs.setShowQuestionModal(false) }
}
