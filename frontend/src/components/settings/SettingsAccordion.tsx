'use client'

import { useSettingsCategory } from '@/app/settings/SettingsCategoryContext'

interface SettingsAccordionProps {
  title: string
  description?: string
  defaultOpen?: boolean
  sectionId?: string
  displayOrder?: number
  categoryId?: string
  children: React.ReactNode
}

/**
 * A flat, always-visible settings section.
 *
 * The historical component name is kept to avoid a broad migration of every
 * settings form, but the disclosure interaction has intentionally been
 * removed. Settings should be scannable and searchable without opening cards.
 */
export function SettingsAccordion({
  title,
  description,
  sectionId,
  displayOrder,
  categoryId,
  children,
}: SettingsAccordionProps) {
  const { activeCategory } = useSettingsCategory()
  if (categoryId && activeCategory !== '*' && categoryId !== activeCategory) return null

  return (
    <section
      id={sectionId}
      style={displayOrder === undefined ? undefined : { order: displayOrder }}
      className="scroll-mt-28 border-b border-gray-200 bg-white py-5 last:border-b-0 dark:border-gray-700 dark:bg-gray-900 md:grid md:grid-cols-[minmax(190px,230px)_minmax(0,1fr)] md:gap-10 md:py-6"
    >
      <div className="mb-4 md:mb-0">
        <h2 className="text-sm font-semibold text-gray-900 dark:text-white">
          {title}
        </h2>
        {description && (
          <p className="mt-1 text-xs leading-5 text-gray-500 dark:text-gray-400">
            {description}
          </p>
        )}
      </div>
      <div className="min-w-0">{children}</div>
    </section>
  )
}
