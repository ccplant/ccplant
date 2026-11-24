'use client'

import { useRef, useState } from 'react'
import { ACPSessionInfo } from '../../lib/agentapi-proxy-client'
import { PendingAction, SessionMessage } from '../../types/agentapi'
import { AgentStatus, MessageSSEConnectionStatus } from './agentChatPresentation'
import { useAgentChatFontSettings } from './useAgentChatFontSettings'
import { useAgentChatScroll } from './useAgentChatScroll'
import { useChatImages } from './useChatImages'

export function useAgentChatMessageState() {
  const [messages, setMessages] = useState<SessionMessage[]>([])
  const scroll = useAgentChatScroll(messages)
  const [hasMoreMessages, setHasMoreMessages] = useState(true)
  const [isLoadingMore, setIsLoadingMore] = useState(false)
  const [isInitialLoadComplete, setIsInitialLoadComplete] = useState(false)
  const [inputValue, setInputValue] = useState('')
  const messageInputRef = useRef<HTMLTextAreaElement>(null)
  const images = useChatImages()
  const [isLoading, setIsLoading] = useState(false)
  const font = useAgentChatFontSettings()
  return {
    messages, setMessages, ...scroll, hasMoreMessages, setHasMoreMessages, isLoadingMore,
    setIsLoadingMore, isInitialLoadComplete, setIsInitialLoadComplete, inputValue, setInputValue,
    messageInputRef, ...images, isLoading, setIsLoading, ...font,
  }
}

export function useAgentChatConnectionState() {
  const [agentStatus, setAgentStatus] = useState<AgentStatus | null>(null)
  const [error, setError] = useState<string | null>(null)
  const [isConnected, setIsConnected] = useState(false)
  const [messageSSEConnectionStatus, setMessageSSEConnectionStatus] = useState<MessageSSEConnectionStatus>('connecting')
  const [isStarting, setIsStarting] = useState(false)
  const [isResuming, setIsResuming] = useState(false)
  const [isDeleting, setIsDeleting] = useState(false)
  const retryTimerRef = useRef<ReturnType<typeof setTimeout> | null>(null)
  const prevAgentStatusRef = useRef<AgentStatus | null>(null)
  const lastLoadTimeRef = useRef(0)
  return {
    agentStatus, setAgentStatus, error, setError, isConnected, setIsConnected,
    messageSSEConnectionStatus, setMessageSSEConnectionStatus, isStarting, setIsStarting,
    isResuming, setIsResuming, isDeleting, setIsDeleting, retryTimerRef, prevAgentStatusRef, lastLoadTimeRef,
  }
}

export function useAgentChatACPState() {
  const [agentType, setAgentType] = useState<string | null>(null)
  const [acpInfo, setACPInfo] = useState<ACPSessionInfo | null>(null)
  const [acpPendingPermission, setACPPendingPermission] = useState<{ action: PendingAction; rpcId: number } | null>(null)
  const acpNextPromptId = useRef(1)
  const acpEventSourceRef = useRef<{ close: () => void } | null>(null)
  const acpStatusEventSourceRef = useRef<{ close: () => void } | null>(null)
  return {
    agentType, setAgentType, acpInfo, setACPInfo, acpPendingPermission, setACPPendingPermission,
    acpNextPromptId, acpEventSourceRef, acpStatusEventSourceRef,
  }
}
