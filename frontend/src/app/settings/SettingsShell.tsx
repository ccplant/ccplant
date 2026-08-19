'use client'

import Link from 'next/link'
import { usePathname } from 'next/navigation'
import { useEffect, useState } from 'react'
import { ArrowLeft, PanelLeft, User, Users, X } from 'lucide-react'
import { SettingsSidebar, SidebarGroup } from './SettingsSidebar'

const legacyGroups: SidebarGroup[] = [
  {
    title: '',
    items: [
      { href: '/settings/personal', label: 'Personal', icon: User },
      { href: '/settings/team', label: 'Team', icon: Users },
    ],
  },
]

export function SettingsShell({ children }: { children: React.ReactNode }) {
  const pathname = usePathname()
  const [drawerOpen, setDrawerOpen] = useState(false)

  // ページを移動したらモバイルのドロワーを閉じる
  useEffect(() => {
    setDrawerOpen(false)
  }, [pathname])

  const activeItem = legacyGroups
    .flatMap((group) => group.items)
    .find((item) => pathname === item.href || pathname.startsWith(`${item.href}/`))

  return (
    <main className="min-h-dvh bg-gray-50 dark:bg-gray-950">
      <div className="container mx-auto max-w-6xl px-4 py-6">
        <div className="mb-4 flex items-center gap-3">
          <button
            type="button"
            onClick={() => setDrawerOpen(true)}
            className="md:hidden rounded-md p-2 text-gray-500 hover:bg-gray-100 dark:text-gray-400 dark:hover:bg-gray-800"
            aria-label="設定メニューを開く"
          >
            <PanelLeft className="h-5 w-5" />
          </button>
          <Link
            href="/chats"
            className="inline-flex items-center gap-1.5 text-sm text-gray-600 transition-colors hover:text-gray-900 dark:text-gray-400 dark:hover:text-white"
          >
            <ArrowLeft className="h-4 w-4" />
            チャットに戻る
          </Link>
          <span className="md:hidden ml-auto text-sm font-medium text-gray-900 dark:text-white">
            {activeItem?.label}
          </span>
        </div>

        <div className="flex gap-8">
          <div className="hidden md:block">
            <SettingsSidebar groups={legacyGroups} activeHref={activeItem?.href ?? pathname} />
          </div>

          <div className="min-w-0 flex-1">{children}</div>
        </div>
      </div>

      {/* モバイル用ドロワー */}
      {drawerOpen && (
        <div className="fixed inset-0 z-50 md:hidden" role="dialog" aria-modal="true">
          <div
            className="absolute inset-0 bg-black bg-opacity-50"
            onClick={() => setDrawerOpen(false)}
          />
          <div className="absolute left-0 top-0 h-full w-72 overflow-y-auto bg-white p-4 shadow-xl dark:bg-gray-900">
            <div className="mb-2 flex items-center justify-between">
              <span className="text-sm font-semibold text-gray-900 dark:text-white">設定</span>
              <button
                type="button"
                onClick={() => setDrawerOpen(false)}
                className="rounded-md p-1 text-gray-500 hover:bg-gray-100 dark:text-gray-400 dark:hover:bg-gray-800"
                aria-label="閉じる"
              >
                <X className="h-5 w-5" />
              </button>
            </div>
            <SettingsSidebar groups={legacyGroups} activeHref={activeItem?.href ?? pathname} />
          </div>
        </div>
      )}
    </main>
  )
}
