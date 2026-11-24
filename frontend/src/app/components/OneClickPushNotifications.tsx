'use client'

import React from 'react'
import { PushNotificationState, usePushNotificationState } from './usePushNotificationState'

export const OneClickPushNotifications: React.FC = () => {
  const notifications = usePushNotificationState()
  return <div className="bg-white dark:bg-gray-800 rounded-lg shadow p-6">
    <h3 className="text-lg font-semibold mb-4 text-gray-900 dark:text-white">プッシュ通知</h3>
    <NotificationStatus state={notifications.state} />
    {notifications.state.lastMessage && <NotificationMessage message={notifications.state.lastMessage} />}
    <NotificationActions state={notifications.state} enable={notifications.enable} disable={notifications.disable} test={notifications.test} />
    {notifications.state.isEnabled && <EnabledInformation />}
    <NotificationHelp />
  </div>
}

function NotificationStatus({ state }: { state: PushNotificationState }) {
  const tone = notificationTone(state)
  return <div className="mb-6 p-4 bg-gray-50 dark:bg-gray-700 rounded-lg border border-gray-200 dark:border-gray-600">
    <div className="flex items-center justify-between mb-3">
      <div className="flex items-center"><div className={`w-3 h-3 rounded-full mr-3 ${tone.dot}`} /><span className="text-sm font-medium text-gray-700 dark:text-gray-300">ステータス</span></div>
      <span className={`text-sm font-semibold px-2 py-1 rounded-full ${tone.badge}`}>{statusText(state)}</span>
    </div>
    {state.isSupported && <div className="flex items-center text-xs text-gray-500 dark:text-gray-400"><CheckIcon />許可状況: {permissionText(state.permission)}</div>}
    {state.endpoint && <div className="flex items-start text-xs text-gray-400 dark:text-gray-500 mt-2"><LinkIcon /><span className="break-all">エンドポイント: {state.endpoint.substring(0, 60)}...</span></div>}
  </div>
}

function notificationTone(state: PushNotificationState) {
  if (!state.isSupported || state.permission === 'denied') return { dot: 'bg-red-400', badge: 'bg-red-100 dark:bg-red-900/30 text-red-700 dark:text-red-300' }
  if (state.isEnabled) return { dot: 'bg-green-400', badge: 'bg-green-100 dark:bg-green-900/30 text-green-700 dark:text-green-300' }
  return { dot: 'bg-yellow-400', badge: 'bg-yellow-100 dark:bg-yellow-900/30 text-yellow-700 dark:text-yellow-300' }
}

function statusText(state: PushNotificationState) {
  if (!state.isSupported) return '非対応'
  if (state.permission === 'denied') return '許可が拒否されています'
  return state.isEnabled ? '有効' : '無効'
}

function permissionText(permission: NotificationPermission) {
  if (permission === 'granted') return '許可済み'
  if (permission === 'denied') return '拒否済み'
  return '未設定'
}

function NotificationMessage({ message }: { message: string }) {
  const failed = message.includes('エラー') || message.includes('失敗')
  const style = failed ? 'bg-red-50 dark:bg-red-900/20 border-red-200 dark:border-red-800 text-red-800 dark:text-red-200' : 'bg-green-50 dark:bg-green-900/20 border-green-200 dark:border-green-800 text-green-800 dark:text-green-200'
  return <div className={`mb-4 p-3 rounded-md border ${style}`}><p className="text-sm">{message}</p></div>
}

function NotificationActions({ state, enable, disable, test }: { state: PushNotificationState; enable: () => void; disable: () => void; test: () => void }) {
  if (!state.isSupported) return <UnsupportedNotice />
  if (!state.isEnabled) return <ActionButton onClick={enable} loading={state.isLoading} loadingText="有効化中..." label="プッシュ通知を有効にする" primary />
  return <div className="space-y-3">
    <ActionButton onClick={test} loading={state.isLoading} loadingText="送信中..." label="テスト通知を送信" primary />
    <ActionButton onClick={disable} loading={state.isLoading} loadingText="無効化中..." label="通知を無効にする" />
  </div>
}

function ActionButton({ onClick, loading, loadingText, label, primary = false }: { onClick: () => void; loading: boolean; loadingText: string; label: string; primary?: boolean }) {
  const colors = primary ? 'bg-blue-600 hover:bg-blue-700 text-white' : 'bg-gray-100 dark:bg-gray-700 text-gray-700 dark:text-gray-300 hover:bg-gray-200 dark:hover:bg-gray-600'
  return <button onClick={onClick} disabled={loading} className={`w-full inline-flex items-center justify-center px-4 py-2.5 font-medium rounded-lg transition-all duration-200 disabled:opacity-50 disabled:cursor-not-allowed shadow-sm ${colors}`}>
    {loading && <div className="animate-spin rounded-full h-4 w-4 border-b-2 border-current mr-2" />}{loading ? loadingText : label}
  </button>
}

function UnsupportedNotice() {
  return <div className="p-4 bg-red-50 dark:bg-red-900/20 border border-red-200 dark:border-red-800 rounded-lg"><div className="flex items-center"><WarningIcon /><p className="text-sm text-red-800 dark:text-red-200 font-medium">お使いのブラウザはプッシュ通知をサポートしていません</p></div></div>
}

function EnabledInformation() {
  return <div className="mt-4 p-4 bg-blue-50 dark:bg-blue-900/20 border border-blue-200 dark:border-blue-800 rounded-lg"><div className="flex items-start"><InfoIcon /><div><p className="text-sm font-medium text-blue-800 dark:text-blue-200 mb-1">プッシュ通知が有効です</p><p className="text-xs text-blue-700 dark:text-blue-300">AgentAPIからの応答や重要な更新をリアルタイムで受け取れます</p></div></div></div>
}

function NotificationHelp() {
  const items = ['設定はブラウザに永続的に保存されます', '通知許可は一度拒否すると、ブラウザ設定から手動で有効にする必要があります', 'プライベートブラウジングモードでは設定が保存されません']
  return <details className="mt-4 group"><summary className="flex items-center text-sm text-gray-600 dark:text-gray-400 cursor-pointer hover:text-gray-800 dark:hover:text-gray-200 transition-colors">設定について</summary><div className="mt-3 ml-6 p-3 bg-gray-50 dark:bg-gray-800 rounded-lg border border-gray-200 dark:border-gray-700"><div className="text-xs text-gray-600 dark:text-gray-400 space-y-2">{items.map((item) => <div key={item} className="flex items-start"><div className="w-1.5 h-1.5 bg-gray-400 rounded-full mt-1.5 mr-2 flex-shrink-0" /><span>{item}</span></div>)}</div></div></details>
}

function CheckIcon() { return <svg className="w-3 h-3 mr-2" fill="none" stroke="currentColor" viewBox="0 0 24 24"><path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M9 12l2 2 4-4m6 2a9 9 0 11-18 0 9 9 0 0118 0z" /></svg> }
function LinkIcon() { return <svg className="w-3 h-3 mr-2 mt-0.5 flex-shrink-0" fill="none" stroke="currentColor" viewBox="0 0 24 24"><path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M13.828 10.172a4 4 0 00-5.656 0l-4 4a4 4 0 105.656 5.656l1.102-1.101m-.758-4.899a4 4 0 005.656 0l4-4a4 4 0 00-5.656-5.656l-1.1 1.1" /></svg> }
function WarningIcon() { return <svg className="w-5 h-5 text-red-600 dark:text-red-400 mr-3" fill="none" stroke="currentColor" viewBox="0 0 24 24"><path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M12 9v2m0 4h.01m-6.938 4h13.856c1.54 0 2.502-1.667 1.732-2.5L13.732 4.5c-.77-.833-2.694-.833-3.464 0L3.34 16.5c-.77.833.192 2.5 1.732 2.5z" /></svg> }
function InfoIcon() { return <svg className="w-5 h-5 text-blue-600 dark:text-blue-400 mr-3 mt-0.5 flex-shrink-0" fill="none" stroke="currentColor" viewBox="0 0 24 24"><path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M13 16h-1v-4h-1m1-4h.01M21 12a9 9 0 11-18 0 9 9 0 0118 0z" /></svg> }
