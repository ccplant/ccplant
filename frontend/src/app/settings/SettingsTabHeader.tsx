'use client'

import { useSettingsCategory } from './SettingsCategoryContext'

const categoryCopy: Record<string, { title: string; description: string }> = {
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
  'client-settings': {
    title: 'クライアント',
    description: 'このブラウザで使用する表示と入力操作を設定します。変更はすぐに反映されます。',
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
  const copy = categoryCopy[activeCategory] || categoryCopy['ai-authentication']

  return (
    <header className="order-0 border-b border-gray-200 pb-5 dark:border-gray-700">
      <div className="mb-2 text-xs font-medium text-blue-600 dark:text-blue-400">{scopeLabel}</div>
      <h1 className="text-2xl font-bold tracking-tight text-gray-900 dark:text-white">{copy.title}</h1>
      <p className="mt-1.5 max-w-2xl text-sm leading-6 text-gray-500 dark:text-gray-400">{copy.description}</p>
    </header>
  )
}
