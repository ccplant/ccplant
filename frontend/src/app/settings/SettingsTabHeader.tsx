'use client'

import { useSettingsCategory } from './SettingsCategoryContext'

const categoryCopy: Record<string, { title: string; description: string }> = {
  'settings-overview': {
    title: '設定の概要',
    description: '現在の設定状態と、このスコープで有効な構成を確認できます。',
  },
  'ai-authentication': {
    title: 'AI・認証',
    description: '既定のエージェント、AIプロバイダー、認証情報を管理します。',
  },
  extensions: {
    title: '拡張機能',
    description: 'プラグイン、Marketplace、MCPサーバーを管理します。',
  },
  'session-settings': {
    title: 'セッション',
    description: 'セッションの動作、環境変数、ファイル、実行環境を設定します。',
  },
  'notification-settings': {
    title: '通知',
    description: 'セッションの完了や更新を受け取る通知先を設定します。',
  },
  'security-settings': {
    title: 'セキュリティ',
    description: 'APIトークンや共有認証情報など、機密性の高い設定を管理します。',
  },
}

export function SettingsTabHeader({ scopeLabel }: { scopeLabel: string }) {
  const { activeCategory } = useSettingsCategory()
  const copy = categoryCopy[activeCategory] || categoryCopy['settings-overview']

  return (
    <header className="order-0 border-b border-gray-200 pb-5 dark:border-gray-700">
      <div className="mb-2 text-xs font-medium text-blue-600 dark:text-blue-400">{scopeLabel}</div>
      <h1 className="text-2xl font-bold tracking-tight text-gray-900 dark:text-white">{copy.title}</h1>
      <p className="mt-1.5 max-w-2xl text-sm leading-6 text-gray-500 dark:text-gray-400">{copy.description}</p>
    </header>
  )
}

export function SettingsOverviewRows({ rows }: { rows: Array<{ label: string; value: string }> }) {
  return (
    <dl className="divide-y divide-gray-100 dark:divide-gray-800">
      {rows.map((row) => (
        <div key={row.label} className="flex min-h-12 items-center justify-between gap-4 py-2 first:pt-0 last:pb-0">
          <dt className="text-sm text-gray-600 dark:text-gray-300">{row.label}</dt>
          <dd className="text-right text-sm font-medium text-gray-900 dark:text-white">{row.value}</dd>
        </div>
      ))}
    </dl>
  )
}
