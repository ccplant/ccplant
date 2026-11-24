'use client'

import { useRouter } from 'next/navigation'
import { getEnterKeyBehavior } from '../../types/settings'
import { sendMessageAction } from './agentChatSendActions'
import { deleteSessionAction, sendStopSignalAction } from './agentChatSessionActions'
import { useAgentChatClients } from './useAgentChatClients'
import { useAgentChatConnectionState, useAgentChatMessageState } from './useAgentChatState'
import { useAgentChatHerdr } from './useAgentChatHerdr'
import { useChatMessageLibrary } from './useChatMessageLibrary'
import { ACPSessionInfo } from '../../lib/agentapi-proxy-client'

interface CommandHandlerInput {
  sessionId: string | null
  clients: ReturnType<typeof useAgentChatClients>
  messages: ReturnType<typeof useAgentChatMessageState>
  connection: ReturnType<typeof useAgentChatConnectionState>
  acpInfo: ACPSessionInfo | null
  agentType: string | null
  acpNextPromptId: React.MutableRefObject<number>
  messageLibrary: ReturnType<typeof useChatMessageLibrary>
}

export function useAgentChatCommandHandlers(input: CommandHandlerInput) {
  const router = useRouter()
  const sender = useAgentChatSender(input)
  const sendStopSignal = async () => sendStopSignalAction({
    sessionId: input.sessionId, agentAPIRef: input.clients.agentAPIRef,
    acpServerEnabled: input.clients.acpServerEnabled, acpServerClientRef: input.clients.acpServerClientRef,
    setError: input.connection.setError, agentType: input.agentType, acpInfo: input.acpInfo,
  })
  const deleteSession = async () => deleteSessionAction({
    router, sessionId: input.sessionId, agentAPIRef: input.clients.agentAPIRef,
    setError: input.connection.setError, setIsDeleting: input.connection.setIsDeleting,
  })
  const handleKeyDown = (event: React.KeyboardEvent) => {
    if (event.key !== 'Enter') return
    const modifier = event.metaKey || event.ctrlKey
    const shouldSend = getEnterKeyBehavior() === 'send' ? !modifier : modifier
    if (shouldSend) { event.preventDefault(); void sender.herdr.sendComposerMessage() }
  }
  return {
    ...sender, sendStopSignal, deleteSession, handleKeyDown,
    sendArrowUp: () => sender.sendMessage('raw', '\u001b[A'),
    sendArrowDown: () => sender.sendMessage('raw', '\u001b[B'), sendEnterKey: () => sender.sendMessage('raw', '\r'),
    formatTimestamp: (timestamp: string) => new Date(timestamp).toLocaleTimeString(),
  }
}

function useAgentChatSender(input: CommandHandlerInput) {
  const { clients, messages, connection } = input
  const sendMessage = async (messageType: 'user' | 'raw' = 'user', content?: string) => sendMessageAction({
    sessionId: input.sessionId, agentAPIRef: clients.agentAPIRef, acpServerEnabled: clients.acpServerEnabled,
    acpServerClientRef: clients.acpServerClientRef, setMessages: messages.setMessages,
    inputValue: messages.inputValue, setInputValue: messages.setInputValue, attachedImages: messages.attachedImages,
    setAttachedImages: messages.setAttachedImages, isLoading: messages.isLoading, setIsLoading: messages.setIsLoading,
    agentStatus: connection.agentStatus, setAgentStatus: connection.setAgentStatus, setError: connection.setError,
    isConnected: connection.isConnected, setShouldAutoScroll: messages.setShouldAutoScroll, acpInfo: input.acpInfo,
    acpNextPromptId: input.acpNextPromptId, scrollToBottom: messages.scrollToBottom,
    loadRecentMessages: input.messageLibrary.loadRecentMessages,
  }, messageType, content)
  const herdr = useAgentChatHerdr({
    sessionId: input.sessionId, agentAPIRef: clients.agentAPIRef, inputValue: messages.inputValue,
    setInputValue: messages.setInputValue, isConnected: connection.isConnected,
    setError: connection.setError, sendMessage: () => sendMessage(),
  })
  return { sendMessage, herdr, ...herdr }
}
