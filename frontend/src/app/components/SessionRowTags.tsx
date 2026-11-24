'use client'
import type { SessionListRowState } from './sessionListRowState'
export function SessionRowTags(context: SessionListRowState) {
  const { isMobile, session } = context

  return (
    <div className="flex flex-wrap gap-1 sm:gap-2">
      {/* Tags field (priority display) */}
      {session.tags &&
        Object.entries(session.tags)
          .filter(([key]) => key !== 'description')
          .slice(0, isMobile ? 2 : 5)
          .map(([key, value]) => (
            <span
              key={`tag-${key}`}
              className="inline-flex items-center px-2 py-1 text-xs bg-green-100 dark:bg-green-900 text-green-800 dark:text-green-200 rounded-full"
            >
              <span className="hidden sm:inline">{key}: </span>
              <span className="sm:hidden">{key.substring(0, 8)}: </span>
              {value.substring(0, isMobile ? 8 : 20)}
            </span>
          ))}

      {/* Metadata (fallback/additional info) */}
      {session.metadata &&
        Object.entries(session.metadata)
          .filter(([key]) => key !== 'description')
          .slice(0, Math.max(0, (isMobile ? 2 : 10) - (session.tags ? Object.keys(session.tags).length : 0)))
          .map(([key, value]) => (
            <span
              key={`meta-${key}`}
              className="inline-flex items-center px-2 py-1 text-xs bg-blue-100 dark:bg-blue-900 text-blue-800 dark:text-blue-200 rounded-full"
            >
              <span className="hidden sm:inline">{key}: </span>
              <span className="sm:hidden">{key.substring(0, 8)}: </span>
              {String(value).substring(0, isMobile ? 8 : 20)}
            </span>
          ))}

      {/* Show "more" indicator if there are additional tags/metadata */}
      {(() => {
        const totalTags =
          (session.tags ? Object.keys(session.tags).length : 0) +
          (session.metadata ? Object.entries(session.metadata).filter(([key]) => key !== 'description').length : 0)
        const displayed = isMobile ? 2 : 10
        return (
          totalTags > displayed &&
          isMobile && (
            <span className="inline-flex items-center px-2 py-1 text-xs bg-gray-100 dark:bg-gray-900 text-gray-600 dark:text-gray-400 rounded-full">
              +{totalTags - displayed}
            </span>
          )
        )
      })()}
    </div>
  )
}
