'use client'

import { useState } from 'react'
import { Schedule } from '../../types/schedule'
import { ScheduleCardActions } from './ScheduleCardActions'
import { ScheduleCardDetails } from './ScheduleCardDetails'

interface ScheduleCardProps {
  schedule: Schedule
  onEdit: (schedule: Schedule) => void
  onDelete: (scheduleId: string) => void
  onTogglePause: (scheduleId: string, currentStatus: Schedule['status']) => void
  onTrigger: (scheduleId: string) => void
}

export default function ScheduleCard({
  schedule,
  onEdit,
  onDelete,
  onTogglePause,
  onTrigger,
}: ScheduleCardProps) {
  const [isDeleting, setIsDeleting] = useState(false)
  const [isTriggering, setIsTriggering] = useState(false)
  const [showExecutionHistory, setShowExecutionHistory] = useState(false)

  const handleDelete = async () => {
    if (!confirm(`スケジュール「${schedule.name}」を削除してもよろしいですか？`)) {
      return
    }
    setIsDeleting(true)
    try {
      await onDelete(schedule.id)
    } finally {
      setIsDeleting(false)
    }
  }

  const handleTrigger = async () => {
    setIsTriggering(true)
    try {
      await onTrigger(schedule.id)
    } finally {
      setIsTriggering(false)
    }
  }

  return (
    <div className="bg-white dark:bg-gray-800 rounded-lg border border-gray-200 dark:border-gray-700 p-4 hover:shadow-md transition-shadow">
      <ScheduleCardDetails
        schedule={schedule}
        showExecutionHistory={showExecutionHistory}
        onToggleExecutionHistory={() => setShowExecutionHistory((current) => !current)}
      />
      <ScheduleCardActions
        schedule={schedule}
        isDeleting={isDeleting}
        isTriggering={isTriggering}
        onDelete={handleDelete}
        onEdit={() => onEdit(schedule)}
        onTogglePause={() => onTogglePause(schedule.id, schedule.status)}
        onTrigger={handleTrigger}
      />
    </div>
  )
}
