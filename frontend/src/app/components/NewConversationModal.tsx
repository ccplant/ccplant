'use client'

import { useTeamScope } from '../../contexts/TeamScopeContext'
import { SessionFilter } from '../../lib/filter-utils'
import { NewConversationModalView } from './NewConversationModalView'
import { useNewConversationModal } from './useNewConversationModal'

interface NewConversationModalProps {
  isOpen: boolean
  onClose: () => void
  onSuccess: () => void
  currentFilters?: SessionFilter
  initialRepository?: string
}

export default function NewConversationModal({ isOpen, onClose, onSuccess, currentFilters, initialRepository }: NewConversationModalProps) {
  const { selectedTeam } = useTeamScope()
  const viewProps = useNewConversationModal({
    isOpen,
    selectedTeam,
    currentFilters,
    initialRepository,
    onClose,
    onSuccess,
  })

  if (!isOpen) return null
  return <NewConversationModalView {...viewProps} />
}
