'use client'
import { useEffect, useState } from 'react'
import { createAgentAPIProxyClientFromStorage } from '../../../lib/agentapi-proxy-client'
import type { GitHubIdentity } from '../../../types/github-connection'
import { MessageTemplate } from '../../../types/messageTemplate'
import type { LogicalSessionPool } from '../../../types/session_pool'
import { messageTemplateManager } from '../../../utils/messageTemplateManager'
import { recentMessagesManager } from '../../../utils/recentMessagesManager'

export function useNewSessionOptions() {
  const [templates, setTemplates] = useState<MessageTemplate[]>([])
  const [recentMessages, setRecentMessages] = useState<string[]>([])
  const [availablePools, setAvailablePools] = useState<LogicalSessionPool[]>([])
  const [linkedConnections, setLinkedConnections] = useState<GitHubIdentity[]>([])
  useEffect(() => {
    loadTemplates()
    loadRecentMessages()
    loadAvailablePools()
    loadLinkedConnections()
  }, [])
  const loadTemplates = async () => {
    try {
      const allTemplates = await messageTemplateManager.getTemplates()
      setTemplates(allTemplates)
    } catch (error) {
      console.error('Failed to load templates:', error)
      setTemplates([])
    }
  }
  const loadRecentMessages = async () => {
    try {
      const messages = await recentMessagesManager.getRecentMessages()
      setRecentMessages(messages.map((msg) => msg.content))
    } catch (error) {
      console.error('Failed to load recent messages:', error)
    }
  }
  const loadAvailablePools = async () => {
    try {
      const client = createAgentAPIProxyClientFromStorage()
      const pools = await client.getAvailableSessionPools()
      setAvailablePools(pools)
    } catch (error) {
      console.error('Failed to load available pools:', error)
    }
  }
  const loadLinkedConnections = async () => {
    try {
      const client = createAgentAPIProxyClientFromStorage()
      const result = await client.listGitHubIdentities()
      setLinkedConnections(result.identities)
    } catch (error) {
      console.error('Failed to load linked connections:', error)
      setLinkedConnections([])
    }
  }
  return {
    templates,
    setTemplates,
    recentMessages,
    setRecentMessages,
    availablePools,
    setAvailablePools,
    linkedConnections,
    setLinkedConnections,
    loadTemplates,
    loadRecentMessages,
    loadAvailablePools,
    loadLinkedConnections,
  }
}
