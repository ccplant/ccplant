import { SettingsSidebar } from './SettingsSidebar'
import { SettingsCategoryProvider } from './SettingsCategoryContext'

export default function SettingsLayout({
  children,
}: {
  children: React.ReactNode
}) {
  return (
    <main className="min-h-dvh bg-gray-50 dark:bg-gray-900">
      <div className="container mx-auto max-w-7xl px-4 py-4 md:py-8">
        <SettingsCategoryProvider>
        <div className="flex flex-col gap-5 md:flex-row md:gap-10">
          {/* Sidebar */}
          <SettingsSidebar />

          {/* Content */}
          <div className="min-w-0 flex-1">
            {children}
          </div>
        </div>
        </SettingsCategoryProvider>
      </div>
    </main>
  )
}
