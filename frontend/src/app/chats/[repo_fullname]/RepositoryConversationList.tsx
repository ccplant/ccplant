'use client'

import Link from 'next/link'
import ConversationCard from '../../components/ConversationCard'
import LoadingSpinner from '../../components/LoadingSpinner'
import { statusOptions } from './repositoryConversationData'
import { useRepositoryConversations } from './useRepositoryConversations'

function Filters({ repository, state }: { repository: string; state: ReturnType<typeof useRepositoryConversations> }) {
  return <div className="bg-white dark:bg-gray-800 border border-gray-200 dark:border-gray-700 rounded-lg p-4">
    <div className="flex flex-wrap items-center gap-3"><span className="text-sm font-medium text-gray-700 dark:text-gray-300">Filter by status:</span><div className="flex flex-wrap gap-2">{statusOptions.map((option) => <button key={option.value} onClick={() => state.changeStatus(option.value)} className={`px-3 py-1.5 text-sm rounded-md border transition-colors ${state.status === option.value ? 'bg-blue-100 border-blue-300 text-blue-800 dark:bg-blue-900 dark:border-blue-600 dark:text-blue-200' : 'bg-white border-gray-300 text-gray-700 hover:bg-gray-50 dark:bg-gray-700 dark:border-gray-600 dark:text-gray-300 dark:hover:bg-gray-600'}`}>{option.label}</button>)}</div></div>
    <div className="mt-3 flex items-center justify-between"><span className="text-sm text-gray-500 dark:text-gray-400">{state.totalChats} conversation{state.totalChats !== 1 ? 's' : ''} for {repository}</span><button onClick={state.fetchChats} disabled={state.loading} className="text-sm text-blue-600 dark:text-blue-400 hover:text-blue-800 dark:hover:text-blue-300 disabled:opacity-50">{state.loading ? 'Refreshing...' : 'Refresh'}</button></div>
  </div>
}

function ConversationList({ repository, state }: { repository: string; state: ReturnType<typeof useRepositoryConversations> }) {
  if (state.filteredChats.length === 0) return <div className="text-center py-12"><div className="text-gray-400 dark:text-gray-500 mb-2"><svg className="w-12 h-12 mx-auto" fill="none" stroke="currentColor" viewBox="0 0 24 24"><path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M8 12h.01M12 12h.01M16 12h.01M21 12c0 4.418-4.03 8-9 8a9.863 9.863 0 01-4.255-.949L3 20l1.395-3.72C3.512 15.042 3 13.574 3 12c0-4.418 4.03-8 9-8s9 3.582 9 8z" /></svg></div><h3 className="text-lg font-medium text-gray-900 dark:text-white mb-1">No conversations found</h3><p className="text-gray-500 dark:text-gray-400">{state.status === 'all' ? `No conversations found for repository "${repository}".` : `No conversations with status "${state.status}" for repository "${repository}".`}</p></div>
  return <div className="space-y-4">{state.filteredChats.map((chat) => <ConversationCard key={chat.id} chat={chat} />)}{state.totalPages > 1 && <div className="flex items-center justify-center gap-2 pt-6"><button onClick={() => state.setPage(state.page - 1)} disabled={state.page === 1} className="px-3 py-2 text-sm border border-gray-300 dark:border-gray-600 rounded-md disabled:opacity-50 disabled:cursor-not-allowed hover:bg-gray-50 dark:hover:bg-gray-700">Previous</button><span className="px-4 py-2 text-sm text-gray-700 dark:text-gray-300">Page {state.page} of {state.totalPages}</span><button onClick={() => state.setPage(state.page + 1)} disabled={state.page === state.totalPages} className="px-3 py-2 text-sm border border-gray-300 dark:border-gray-600 rounded-md disabled:opacity-50 disabled:cursor-not-allowed hover:bg-gray-50 dark:hover:bg-gray-700">Next</button></div>}</div>
}

export default function RepositoryConversationList({ repository }: { repository: string }) {
  const state = useRepositoryConversations(repository)
  if (state.loading && state.filteredChats.length === 0) return <LoadingSpinner />
  return <div className="space-y-6">
    <div className="flex items-center gap-2 text-sm"><Link href="/chats" className="text-blue-600 dark:text-blue-400 hover:text-blue-800 dark:hover:text-blue-300">All Conversations</Link><span className="text-gray-400 dark:text-gray-500">→</span><span className="text-gray-600 dark:text-gray-400">{repository}</span></div>
    <Filters repository={repository} state={state} />
    {state.error && <div className="bg-red-50 dark:bg-red-900/20 border border-red-200 dark:border-red-800 rounded-lg p-4"><span className="text-red-600 dark:text-red-400 text-sm">{state.error}</span></div>}
    <ConversationList repository={repository} state={state} />
  </div>
}
