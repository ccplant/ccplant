'use client'

import { Schedule } from '../../types/schedule'
import { useScheduleForm } from './useScheduleForm'
import { ScheduleFormView } from './ScheduleFormView'

interface ScheduleFormModalProps {
  isOpen: boolean
  onClose: () => void
  onSuccess: () => void
  editingSchedule?: Schedule | null
}

export default function ScheduleFormModal({
  isOpen,
  onClose,
  onSuccess,
  editingSchedule,
}: ScheduleFormModalProps) {
  const form = useScheduleForm({ isOpen, onClose, onSuccess, editingSchedule })

  if (!isOpen) return null
  return <ScheduleFormView model={form} editingSchedule={editingSchedule} />
}