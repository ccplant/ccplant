'use client'

import LoadingSpinner from './LoadingSpinner'
import SessionFilterSidebar from './SessionFilterSidebar'
import {
  AdvancedConversationControls,
  BulkActionBar,
  ConversationError,
  QuickStartPanel,
  SessionListPanel,
  SessionListToolbar,
} from './ConversationListSections'
import type { ConversationListModel } from './useConversationList'

export function ConversationListView({ model }: { model: ConversationListModel }) {
  if (model.loading && !model.allSessions.length) return <LoadingSpinner />
  return (
    <div className="flex">
      <SessionFilterSidebar filterGroups={model.filterGroups} currentFilters={model.sessionFilters} onFiltersChange={model.handleFiltersChange} isVisible={model.sidebarVisible} onToggleVisibility={() => model.setSidebarVisible(!model.sidebarVisible)} />
      <div className="flex-1 space-y-6 p-4 md:p-6">
        <QuickStartPanel model={model} />
        <AdvancedConversationControls model={model} />
        <BulkActionBar model={model} />
        <SessionListToolbar model={model} />
        <ConversationError error={model.error} />
        <SessionListPanel model={model} />
      </div>
    </div>
  )
}
