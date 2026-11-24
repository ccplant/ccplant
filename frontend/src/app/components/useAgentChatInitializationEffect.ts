'use client'

import { useEffect } from 'react'
import { initializeAgentChat } from './agentChatInitialization'
import { useAgentChatClients } from './useAgentChatClients'
import { useAgentChatDialogs } from './useAgentChatDialogs'
import { useAgentChatRuntime } from './useAgentChatRuntime'
import { useAgentChatACPState, useAgentChatConnectionState, useAgentChatMessageState } from './useAgentChatState'

interface InitializationInput {
  sessionId: string | null
  clients: ReturnType<typeof useAgentChatClients>
  messages: ReturnType<typeof useAgentChatMessageState>
  connection: ReturnType<typeof useAgentChatConnectionState>
  acp: ReturnType<typeof useAgentChatACPState>
  dialogs: ReturnType<typeof useAgentChatDialogs>
  runtime: ReturnType<typeof useAgentChatRuntime>
}

export function useAgentChatInitializationEffect(input: InitializationInput) {
  const { sessionId, clients, messages, connection, acp, dialogs, runtime } = input
  useEffect(() => initializeAgentChat({
    agentAPI: clients.agentAPI, sessionId, setIsInitialLoadComplete: messages.setIsInitialLoadComplete,
    setIsStarting: connection.setIsStarting, setIsResuming: connection.setIsResuming,
    setIsConnected: connection.setIsConnected, setAgentStatus: connection.setAgentStatus,
    setAgentType: acp.setAgentType, setMessages: messages.setMessages, setACPInfo: acp.setACPInfo,
    setACPUserPrompts: runtime.setACPUserPrompts, setLoadedACPStartPromptIndex: runtime.setLoadedACPStartPromptIndex,
    setMessageSSEConnectionStatus: connection.setMessageSSEConnectionStatus, retryTimerRef: connection.retryTimerRef,
    acpInfo: acp.acpInfo, acpEventSourceRef: acp.acpEventSourceRef, setError: connection.setError,
    agentAPIRef: clients.agentAPIRef, applyACPConfigOptions: runtime.applyACPConfigOptions,
    setACPPendingPermission: acp.setACPPendingPermission, setPendingAction: dialogs.setPendingAction,
    setShowQuestionModal: dialogs.setShowQuestionModal, acpStatusEventSourceRef: acp.acpStatusEventSourceRef,
    acpServerEnabled: clients.acpServerEnabled, setHasMoreMessages: messages.setHasMoreMessages,
    acpServerClientRef: clients.acpServerClientRef, messagesEndRef: messages.messagesEndRef,
    // Metadata setters are stable and must not restart the history/subscription handshake.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }), [sessionId, clients.agentAPI, clients.acpServerEnabled])
}
