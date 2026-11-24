'use client'

import type { SlackbotFormModel } from './useSlackbotForm'
import {
  SlackAppSection,
  SlackbotAdvancedSection,
  SlackbotBasicFields,
  SlackbotFormFooter,
  SlackbotFormHeader,
} from './SlackbotFormSections'

interface SlackbotFormViewProps {
  model: SlackbotFormModel
  onClose: () => void
}

export function SlackbotFormView({ model, onClose }: SlackbotFormViewProps) {
  return (
    <div className="fixed inset-0 z-50 overflow-y-auto">
      <div className="fixed inset-0 bg-black bg-opacity-50 transition-opacity" onClick={onClose} />
      <div className="relative min-h-full flex items-center justify-center p-4">
        <div className="relative bg-white dark:bg-gray-800 rounded-xl shadow-2xl w-full max-w-2xl" onClick={event => event.stopPropagation()}>
          <SlackbotFormHeader isEditing={model.isEditing} onClose={onClose} />
          <form onSubmit={model.handleSubmit} className="px-6 py-5 space-y-5 max-h-[70vh] overflow-y-auto">
            <SlackbotBasicFields model={model} />
            <SlackAppSection model={model} />
            <SlackbotAdvancedSection model={model} />
          </form>
          <SlackbotFormFooter model={model} onClose={onClose} />
        </div>
      </div>
    </div>
  )
}
