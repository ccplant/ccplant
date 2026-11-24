import { useCallback, useEffect, useState } from 'react'
import { pushNotificationManager } from '../../utils/pushNotification'

export interface NotificationStatus { isSupported: boolean; permission: NotificationPermission; isSubscribed: boolean; endpoint?: string }
type Operation = 'initialize' | 'permission' | 'subscribe' | 'unsubscribe'

const messages: Record<Operation, { success: string; failure: string; error: string }> = {
  initialize: { success: 'Service Worker が正常に初期化されました', failure: 'Service Worker の初期化に失敗しました', error: '初期化エラー' },
  permission: { success: '通知許可が取得されました', failure: '通知許可が拒否されました', error: '許可エラー' },
  subscribe: { success: 'プッシュ通知の購読が完了しました', failure: 'プッシュ通知の購読に失敗しました', error: '購読エラー' },
  unsubscribe: { success: 'プッシュ通知の購読が解除されました', failure: 'プッシュ通知の購読解除に失敗しました', error: '購読解除エラー' },
}

export function usePushNotificationSettings() {
  const [status, setStatus] = useState<NotificationStatus>({ isSupported: false, permission: 'default', isSubscribed: false })
  const [isLoading, setIsLoading] = useState(false)
  const [message, setMessage] = useState('')
  const checkStatus = useCallback(() => {
    const isSupported = 'serviceWorker' in navigator && 'PushManager' in window
    const subscription = pushNotificationManager.getSubscriptionStatus()
    setStatus({ isSupported, permission: isSupported ? Notification.permission : 'denied', isSubscribed: subscription.isSubscribed, endpoint: subscription.endpoint })
  }, [])
  useEffect(() => { checkStatus() }, [checkStatus])

  const run = async (operation: Operation) => {
    setIsLoading(true); setMessage('')
    try {
      const action = { initialize: () => pushNotificationManager.initialize(), permission: () => pushNotificationManager.requestPermission(), subscribe: () => pushNotificationManager.subscribe(), unsubscribe: () => pushNotificationManager.unsubscribe() }[operation]
      const succeeded = await action()
      setMessage(succeeded ? messages[operation].success : messages[operation].failure)
      if (succeeded) checkStatus()
    } catch (error) {
      setMessage(`${messages[operation].error}: ${error instanceof Error ? error.message : '不明なエラー'}`)
    } finally { setIsLoading(false) }
  }
  const test = () => setMessage('テスト通知機能は無効化されています（ローカル通知機能削除済み）')
  return { status, isLoading, message, initialize: () => run('initialize'), requestPermission: () => run('permission'), subscribe: () => run('subscribe'), unsubscribe: () => run('unsubscribe'), test }
}
export type PushNotificationState = ReturnType<typeof usePushNotificationSettings>
