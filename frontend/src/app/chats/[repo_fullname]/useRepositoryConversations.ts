import { useCallback, useEffect, useState } from 'react'
import { Chat, ChatListResponse } from '../../../types/chat'
import { chatApi, ApiError } from '../../../lib/api'
import { belongsToRepository, ConversationStatus, getMockChats } from './repositoryConversationData'

export function useRepositoryConversations(repository: string) {
  const [chats, setChats] = useState<Chat[]>([])
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)
  const [totalChats, setTotalChats] = useState(0)
  const [status, setStatus] = useState<ConversationStatus>('all')
  const [page, setPage] = useState(1)
  const limit = 20

  const fetchChats = useCallback(async () => {
    try {
      setLoading(true); setError(null)
      const response: ChatListResponse = await chatApi.getChats({ page, limit, repository, ...(status !== 'all' && { status }) })
      setChats(response.chats); setTotalChats(response.total)
    } catch (err) {
      setError(err instanceof ApiError ? `Failed to load conversations: ${err.message}` : 'An unexpected error occurred while loading conversations')
      const fallback = getMockChats().filter((chat) => belongsToRepository(chat, repository))
      setChats(fallback); setTotalChats(fallback.length)
    } finally { setLoading(false) }
  }, [page, repository, status])
  useEffect(() => { void fetchChats() }, [fetchChats])

  const changeStatus = (nextStatus: ConversationStatus) => { setStatus(nextStatus); setPage(1) }
  const filteredChats = status === 'all' ? chats : chats.filter((chat) => chat.status === status)
  return { filteredChats, loading, error, totalChats, status, page, totalPages: Math.ceil(totalChats / limit), fetchChats, changeStatus, setPage }
}
