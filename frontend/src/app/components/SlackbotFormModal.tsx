'use client'

import {
  SlackBot,
} from '../../types/slackbot'
import { useSlackbotForm } from './useSlackbotForm'
import { SlackbotFormView } from './SlackbotFormView'

interface SlackbotFormModalProps {
  isOpen: boolean
  onClose: () => void
  onSuccess: () => void
  editingSlackbot?: SlackBot | null
}

export default function SlackbotFormModal({
  isOpen,
  onClose,
  onSuccess,
  editingSlackbot,
}: SlackbotFormModalProps) {
  const model = useSlackbotForm({ isOpen, onClose, onSuccess, editingSlackbot })

  if (!isOpen) return null
  return <SlackbotFormView model={model} onClose={onClose} />
}