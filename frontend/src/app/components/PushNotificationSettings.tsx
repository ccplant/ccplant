'use client'

import { NotificationStatus, PushNotificationState, usePushNotificationSettings } from './usePushNotificationSettings'

function permissionPresentation(permission: NotificationPermission) {
  if (permission === 'granted') return { text: '許可済み', color: 'text-green-600' }
  if (permission === 'denied') return { text: '拒否済み', color: 'text-red-600' }
  return { text: '未設定', color: 'text-yellow-600' }
}

function StatusView({ status }: { status: NotificationStatus }) {
  const permission = permissionPresentation(status.permission)
  return <div className="grid grid-cols-2 gap-4 text-sm">
    <div><span className="font-medium text-gray-700 dark:text-gray-300">対応状況:</span><span className={`ml-2 ${status.isSupported ? 'text-green-600' : 'text-red-600'}`}>{status.isSupported ? 'サポート済み' : '非対応'}</span></div>
    <div><span className="font-medium text-gray-700 dark:text-gray-300">許可状況:</span><span className={`ml-2 ${permission.color}`}>{permission.text}</span></div>
    <div className="col-span-2"><span className="font-medium text-gray-700 dark:text-gray-300">購読状況:</span><span className={`ml-2 ${status.isSubscribed ? 'text-green-600' : 'text-gray-600'}`}>{status.isSubscribed ? '購読中' : '未購読'}</span></div>
  </div>
}

function ActionButton({ onClick, loading, color, children }: { onClick: () => void; loading: boolean; color: string; children: React.ReactNode }) {
  return <button onClick={onClick} disabled={loading} className={`px-4 py-2 text-sm font-medium text-white rounded-md disabled:opacity-50 disabled:cursor-not-allowed ${color}`}>{children}</button>
}

function NotificationActions({ state }: { state: PushNotificationState }) {
  const { status, isLoading } = state
  if (!status.isSupported) return <div className="w-full p-3 bg-red-50 dark:bg-red-900/20 border border-red-200 dark:border-red-800 rounded-md"><p className="text-sm text-red-800 dark:text-red-200">お使いのブラウザはプッシュ通知をサポートしていません</p></div>
  return <>
    <ActionButton onClick={state.initialize} loading={isLoading} color="bg-blue-600 hover:bg-blue-700">{isLoading ? '初期化中...' : 'Service Worker 初期化'}</ActionButton>
    {status.permission === 'default' && <ActionButton onClick={state.requestPermission} loading={isLoading} color="bg-yellow-600 hover:bg-yellow-700">{isLoading ? '許可要求中...' : '通知許可を取得'}</ActionButton>}
    {status.permission === 'granted' && !status.isSubscribed && <ActionButton onClick={state.subscribe} loading={isLoading} color="bg-green-600 hover:bg-green-700">{isLoading ? '購読中...' : 'プッシュ通知を有効化'}</ActionButton>}
    {status.isSubscribed && <><ActionButton onClick={state.test} loading={isLoading} color="bg-purple-600 hover:bg-purple-700">{isLoading ? '送信中...' : 'テスト通知送信'}</ActionButton><ActionButton onClick={state.unsubscribe} loading={isLoading} color="bg-red-600 hover:bg-red-700">{isLoading ? '解除中...' : 'プッシュ通知を無効化'}</ActionButton></>}
  </>
}

export const PushNotificationSettings = () => {
  const state = usePushNotificationSettings()
  return <div className="bg-white dark:bg-gray-800 rounded-lg shadow p-6"><h3 className="text-lg font-semibold mb-4 text-gray-900 dark:text-white">プッシュ通知設定</h3><div className="space-y-4">
    <StatusView status={state.status} />
    {state.message && <div className="p-3 bg-blue-50 dark:bg-blue-900/20 border border-blue-200 dark:border-blue-800 rounded-md"><p className="text-sm text-blue-800 dark:text-blue-200">{state.message}</p></div>}
    <div className="flex flex-wrap gap-2"><NotificationActions state={state} /></div>
    {state.status.endpoint && <div className="text-xs text-gray-500 dark:text-gray-400 break-all"><span className="font-medium">エンドポイント:</span><br />{state.status.endpoint.substring(0, 100)}...</div>}
  </div></div>
}
