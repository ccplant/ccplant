'use client'

import { useEffect, useMemo } from 'react'
import { getRelatedLinks, getSessionTokenUsage, isACPAgentType } from './agentChatPresentation'
import { useACPModelSelection } from './useACPModelSelection'
import { useACPPromptHistory } from './useACPPromptHistory'
import { useAgentChatClients } from './useAgentChatClients'
import { useAgentChatDialogs } from './useAgentChatDialogs'
import { useAgentChatHistoryHandlers } from './useAgentChatHistoryHandlers'
import { useAgentChatAnnotations } from './useAgentChatMetadata'
import { useAgentChatModelHandlers } from './useAgentChatModelHandlers'
import { useAgentChatPolling } from './useAgentChatPolling'
import { useAgentChatACPState, useAgentChatConnectionState, useAgentChatMessageState } from './useAgentChatState'
import { useHistoryIntersection } from './useAgentChatUIEffects'
import { useAgentChatVisibility } from './useAgentChatVisibility'

interface RuntimeInput {
  sessionId: string | null
  clients: ReturnType<typeof useAgentChatClients>
  messages: ReturnType<typeof useAgentChatMessageState>
  connection: ReturnType<typeof useAgentChatConnectionState>
  acp: ReturnType<typeof useAgentChatACPState>
  dialogs: ReturnType<typeof useAgentChatDialogs>
  annotations: ReturnType<typeof useAgentChatAnnotations>
}

export function useAgentChatRuntime(input: RuntimeInput) {
  const { sessionId, clients, messages, connection, acp, dialogs, annotations } = input
  const derived = useAgentChatDerived(input)
  const modelSelection = useACPModelSelection(acp.acpInfo, annotations.sessionModelOptions)
  const modelHandlers = useAgentChatModelHandlers({ sessionId, clients, acpInfo: acp.acpInfo, setACPInfo: acp.setACPInfo, model: modelSelection })
  const promptHistory = useACPPromptHistory({
    sessionId, acpSessionId: acp.acpInfo?.sessionId, isACPSession: derived.isACPSession,
    agentAPIRef: clients.agentAPIRef, messagesContainerRef: messages.messagesContainerRef,
    setMessages: messages.setMessages, setHasMoreMessages: messages.setHasMoreMessages,
  })
  const historyHandlers = useAgentChatHistoryHandlers({
    sessionId, clients, messages, connection, annotations, dialogs, agentType: acp.agentType,
    acpInfo: acp.acpInfo, acpPendingPermission: acp.acpPendingPermission,
    setACPPendingPermission: acp.setACPPendingPermission,
  })
  useHistoryIntersection({
    topRef: messages.messagesTopRef, containerRef: messages.messagesContainerRef,
    hasMoreMessages: messages.hasMoreMessages, isInitialLoadComplete: messages.isInitialLoadComplete,
    isLoadingMore: messages.isLoadingMore, loadMoreMessages: historyHandlers.loadMoreMessages,
  })
  const pollingControl = useAgentChatPolling({ pollMessages: historyHandlers.pollMessages, isConnected: connection.isConnected, isInitialLoadComplete: messages.isInitialLoadComplete, sessionId, acpInfo: acp.acpInfo })
  const visibility = useRuntimeVisibility(input, promptHistory, modelHandlers.applyACPConfigOptions)
  useAgentChatPanelEffects(derived.isACPSession, messages.isInitialLoadComplete, connection.setIsResuming, dialogs)
  return { ...derived, ...modelSelection, ...modelHandlers, ...promptHistory, ...historyHandlers, pollingControl, ...visibility }
}

function useAgentChatDerived({ clients, messages, connection, acp, annotations }: RuntimeInput) {
  const isACPSession = !!acp.acpInfo || isACPAgentType(acp.agentType) || clients.acpServerEnabled
  const isMessageSSEConnected = !isACPSession || connection.messageSSEConnectionStatus === 'connected'
  const connected = connection.isConnected && isMessageSSEConnected
  const tokenUsage = useMemo(() => getSessionTokenUsage(acp.acpInfo, messages.messages), [acp.acpInfo, messages.messages])
  const relatedLinks = useMemo(() => getRelatedLinks(annotations.sessionAnnotations), [annotations.sessionAnnotations])
  return {
    isACPSession, isMessageSSEConnected, isConnectionStatusConnected: connected,
    connectionStatusLabel: connected ? 'Connected' : 'Connecting',
    connectionStatusTextClass: connected ? 'text-green-600 dark:text-green-400' : 'text-yellow-600 dark:text-yellow-400',
    connectionStatusDotClass: connected ? 'bg-green-500' : 'bg-yellow-500', tokenUsage, relatedLinks,
  }
}

function useRuntimeVisibility(input: RuntimeInput, prompt: ReturnType<typeof useACPPromptHistory>, applyACPConfigOptions: ReturnType<typeof useAgentChatModelHandlers>['applyACPConfigOptions']) {
  const { sessionId, clients, messages, connection, acp, dialogs } = input
  return useAgentChatVisibility({
    sessionId, acpInfo: acp.acpInfo, agentAPIRef: clients.agentAPIRef,
    acpServerEnabled: clients.acpServerEnabled, acpServerClientRef: clients.acpServerClientRef,
    acpEventSourceRef: acp.acpEventSourceRef, acpStatusEventSourceRef: acp.acpStatusEventSourceRef,
    loadedACPStartPromptIndexRef: prompt.loadedACPStartPromptIndexRef, setMessages: messages.setMessages,
    setLoadedACPStartPromptIndex: prompt.setLoadedACPStartPromptIndex, setACPUserPrompts: prompt.setACPUserPrompts,
    setAgentStatus: connection.setAgentStatus, setMessageSSEConnectionStatus: connection.setMessageSSEConnectionStatus,
    applyACPConfigOptions, setACPPendingPermission: acp.setACPPendingPermission,
    setPendingAction: dialogs.setPendingAction, setShowQuestionModal: dialogs.setShowQuestionModal,
  })
}

function useAgentChatPanelEffects(isACP: boolean, loaded: boolean, setIsResuming: (value: boolean) => void, dialogs: ReturnType<typeof useAgentChatDialogs>) {
  useEffect(() => { if (loaded) setIsResuming(false) }, [loaded, setIsResuming])
  useEffect(() => {
    if (isACP) dialogs.setShowControlPanel(false); else dialogs.setShowSessionInfo(false)
  }, [isACP, dialogs])
}
