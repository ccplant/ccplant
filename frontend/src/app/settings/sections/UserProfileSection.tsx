'use client'

import Link from 'next/link'
import CopyableResourceId from '@/app/components/CopyableResourceId'
import { SettingsPageHeader } from '@/components/settings'
import { useSettingsScope } from '../SettingsScopeContext'

export function UserProfileSection() {
  const { principalId, userName } = useSettingsScope()

  return (
    <>
      <SettingsPageHeader
        title="ユーザー情報と編集"
        description="ログイン中のユーザー情報と、アカウントに関連する設定を管理します。"
      />

      <section className="overflow-hidden rounded-lg border border-gray-200 bg-white dark:border-gray-700 dark:bg-gray-900">
        <dl className="divide-y divide-gray-200 dark:divide-gray-700">
          <div className="grid gap-1 px-4 py-4 sm:grid-cols-[11rem_1fr] sm:items-center">
            <dt className="text-sm font-medium text-gray-700 dark:text-gray-300">ユーザー名</dt>
            <dd className="min-w-0 text-sm text-gray-900 dark:text-white">{userName}</dd>
          </div>
          <div className="grid gap-2 px-4 py-4 sm:grid-cols-[11rem_1fr] sm:items-center">
            <dt>
              <span className="block text-sm font-medium text-gray-700 dark:text-gray-300">Principal ID</span>
              <span className="mt-0.5 block text-xs text-gray-500 dark:text-gray-400">プールのユーザー binding に使う ID</span>
            </dt>
            <dd className="min-w-0">
              <CopyableResourceId id={principalId} className="-ml-1.5 text-gray-600 dark:text-gray-300" />
            </dd>
          </div>
        </dl>
      </section>

      <section className="mt-8">
        <h3 className="text-base font-semibold text-gray-900 dark:text-white">アカウント設定の編集</h3>
        <p className="mt-1 text-sm text-gray-500 dark:text-gray-400">
          ユーザー名と Principal ID は認証元から取得するため、この画面では変更できません。
        </p>
        <div className="mt-3 flex flex-wrap gap-3">
          <Link href="/settings/personal/account-connections" className="rounded-md border border-gray-300 bg-white px-3 py-2 text-sm font-medium text-gray-700 hover:bg-gray-50 dark:border-gray-600 dark:bg-gray-900 dark:text-gray-200 dark:hover:bg-gray-800">
            アカウント連携を編集
          </Link>
          <Link href="/settings/personal/api-tokens" className="rounded-md border border-gray-300 bg-white px-3 py-2 text-sm font-medium text-gray-700 hover:bg-gray-50 dark:border-gray-600 dark:bg-gray-900 dark:text-gray-200 dark:hover:bg-gray-800">
            API トークンを管理
          </Link>
        </div>
      </section>
    </>
  )
}
