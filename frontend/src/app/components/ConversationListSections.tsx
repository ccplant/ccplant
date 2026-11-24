'use client'

import NewConversationModal from './NewConversationModal'
import { SmartSessionList } from './VirtualizedSessionList'
import type { ConversationListModel, SortField } from './useConversationList'

export function QuickStartPanel({ model }: { model: ConversationListModel }) {
  return (
    <div className="bg-white dark:bg-gray-800 border border-gray-200 dark:border-gray-700 rounded-lg p-4">
      <div className="flex gap-3"><input type="text" value={model.quickStartMessage} onChange={event => model.setQuickStartMessage(event.target.value)} onKeyPress={model.handleQuickStartKeyPress} placeholder="Type your message to start a new session..." className="flex-1 px-3 py-2 border rounded-md dark:bg-gray-700" disabled={model.isCreatingQuickSession} /><button onClick={model.handleQuickStart} disabled={!model.quickStartMessage.trim() || model.isCreatingQuickSession} className="px-4 py-2 bg-blue-600 disabled:bg-gray-400 text-white rounded-md">{model.isCreatingQuickSession ? 'Starting...' : 'Start'}</button></div>
      <p className="text-sm text-gray-500 mt-2">Press Enter or click Start to begin a new conversation session</p>
    </div>
  )
}

export function BulkActionBar({ model }: { model: ConversationListModel }) {
  return (
    <div className={`fixed bottom-8 left-1/2 -translate-x-1/2 z-50 flex items-center gap-3 px-5 py-3 bg-gray-900 dark:bg-gray-100 text-white dark:text-gray-900 rounded-full shadow-2xl transition-all ${model.selectedSessions.size ? 'translate-y-0 opacity-100' : 'translate-y-24 opacity-0 pointer-events-none'}`}>
      <span className="text-sm font-semibold">{model.selectedSessions.size}件選択中</span><div className="w-px h-4 bg-gray-600" />
      <button onClick={() => model.setSelectedSessions(new Set())} className="text-sm">選択解除</button>
      <button onClick={model.deleteSelectedSessions} disabled={model.isBulkDeleting} className="px-3 py-1.5 bg-red-500 disabled:opacity-50 text-white text-sm rounded-full">{model.isBulkDeleting ? '削除中...' : '削除'}</button>
    </div>
  )
}

function SelectionControls({ model }: { model: ConversationListModel }) {
  const allSelected = model.selectedSessions.size > 0 && model.selectedSessions.size === model.paginatedSessions.length
  return (
    <div className="flex items-center gap-4">
      <button onClick={model.toggleSelectAll} className="flex items-center gap-2 text-xs"><span className={`w-4 h-4 rounded border-2 ${model.selectedSessions.size ? 'bg-blue-600 border-blue-600' : 'border-gray-300'}`}>{allSelected ? '✓' : ''}</span>全て選択</button>
      <button onClick={() => model.setSidebarVisible(!model.sidebarVisible)} className="px-3 py-2 text-sm bg-gray-100 dark:bg-gray-700 rounded-md">{model.sidebarVisible ? 'Hide Filters' : 'Show Filters'}</button>
      <span className="text-sm text-gray-500">{model.filteredSessions.length} of {model.allSessions.length} sessions</span>
    </div>
  )
}

function SortControls({ model }: { model: ConversationListModel }) {
  return (
    <div className="flex items-center gap-2">
      <button onClick={() => model.setIsSelectionMode(previous => { if (previous) model.setSelectedSessions(new Set()); return !previous })} className={`px-3 py-1.5 text-sm rounded-md border ${model.isSelectionMode ? 'bg-blue-50 border-blue-400' : 'bg-gray-50 border-gray-200'}`}>選択</button>
      <select value={model.sortBy} onChange={event => model.setSortBy(event.target.value as SortField)} className="text-sm border rounded-md px-3 py-1.5 dark:bg-gray-800"><option value="started_at">開始日時順</option><option value="updated_at">更新日時順</option></select>
      <button onClick={() => model.setSortOrder(model.sortOrder === 'asc' ? 'desc' : 'asc')} className="px-2 py-1.5 bg-gray-100 rounded-md" title={model.sortOrder === 'asc' ? '昇順' : '降順'}>{model.sortOrder === 'asc' ? '↑' : '↓'}</button>
      <button onClick={model.fetchSessions} disabled={model.loading} className="px-3 py-1.5 text-sm text-blue-600 disabled:opacity-50">{model.loading ? '更新中...' : '更新'}</button>
    </div>
  )
}

export function SessionListToolbar({ model }: { model: ConversationListModel }) {
  return <div className="bg-white dark:bg-gray-800 border rounded-lg p-4"><div className="flex flex-col sm:flex-row sm:items-center justify-between gap-4"><SelectionControls model={model} /><SortControls model={model} /></div></div>
}

function EmptySessions({ hasAny }: { hasAny: boolean }) {
  return <div className="text-center py-12"><h3 className="text-lg font-medium mb-1">No sessions found</h3><p className="text-gray-500">{hasAny ? 'No sessions match the current filters. Try adjusting your filters.' : 'Start a new session to see it here.'}</p></div>
}

export function SessionListPanel({ model }: { model: ConversationListModel }) {
  return (
    <div className="bg-white dark:bg-gray-800 border rounded-lg">
      {!model.paginatedSessions.length ? <EmptySessions hasAny={Boolean(model.allSessions.length)} /> : <><SmartSessionList sessions={model.paginatedSessions} onDelete={model.deleteSession} deletingSession={model.deletingSession} selectedSessions={model.selectedSessions} onToggleSelect={model.toggleSessionSelection} isSelectionMode={model.isSelectionMode} virtualizationThreshold={50} />{model.totalPages > 1 && <div className="flex items-center justify-center gap-2 px-4 py-4 border-t"><button onClick={() => model.handlePageChange(model.pageState.page - 1)} disabled={model.pageState.page === 1} className="px-3 py-2 border rounded-md disabled:opacity-50">Previous</button><span>Page {model.pageState.page} of {model.totalPages}</span><button onClick={() => model.handlePageChange(model.pageState.page + 1)} disabled={model.pageState.page === model.totalPages} className="px-3 py-2 border rounded-md disabled:opacity-50">Next</button></div>}</>}
    </div>
  )
}

export function ConversationError({ error }: { error: string | null }) {
  return error ? <div className="bg-red-50 dark:bg-red-900/20 border border-red-200 rounded-lg p-4"><span className="text-red-600 text-sm">{error}</span></div> : null
}

export function AdvancedConversationControls({ model }: { model: ConversationListModel }) {
  return (
    <><div className="flex justify-end"><button onClick={() => model.setShowNewConversationModal(true)} className="px-4 py-2 bg-gray-600 text-white text-sm rounded-lg">Advanced Options</button></div><NewConversationModal isOpen={model.showNewConversationModal} onClose={() => model.setShowNewConversationModal(false)} onSuccess={model.handleNewConversationSuccess} currentFilters={model.sessionFilters} initialRepository={model.repositoryParam || undefined} /></>
  )
}
