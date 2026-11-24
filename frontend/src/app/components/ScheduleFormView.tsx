'use client'

import type { Schedule } from '../../types/schedule'
import {
  CompletedScheduleNotice,
  ScheduleBehaviorFields,
  ScheduleFormActions,
  ScheduleSessionFields,
  ScheduleTimingFields,
} from './ScheduleFormSections'
import type { ScheduleFormModel } from './useScheduleForm'

interface ScheduleFormViewProps { model: ScheduleFormModel; editingSchedule?: Schedule | null }

export function ScheduleFormView({ model, editingSchedule }: ScheduleFormViewProps) {
  return (
    <div className="fixed inset-0 bg-black bg-opacity-50 flex items-center justify-center z-50" onClick={event => { if (event.target === event.currentTarget) model.handleClose() }}>
      <div className="bg-white dark:bg-gray-800 rounded-lg p-6 w-full max-w-lg mx-4 max-h-[90vh] overflow-y-auto">
        <div className="flex justify-between items-center mb-6"><h2 className="text-xl font-semibold text-gray-900 dark:text-white">{model.isEditing ? 'スケジュールを編集' : '新しいスケジュール'}</h2><button onClick={model.handleClose} className="text-gray-400 hover:text-gray-600" aria-label="Close">×</button></div>
        <form onSubmit={model.handleSubmit} className="space-y-4">
          <CompletedScheduleNotice schedule={editingSchedule} />
          <ScheduleTimingFields form={model} />
          <ScheduleSessionFields form={model} />
          <ScheduleBehaviorFields form={model} />
          <ScheduleFormActions form={model} />
        </form>
      </div>
    </div>
  )
}
