'use client'

import { useCallback, useEffect, useState } from 'react'
import { pushNotificationSettings } from '../../lib/pushNotificationSettings'
import { pushNotificationManager } from '../../utils/pushNotification'

interface NotificationState {
  isEnabled: boolean
  isSupported: boolean
  permission: NotificationPermission
  isLoading: boolean
  lastMessage: string
  endpoint?: string
}

const initialState: NotificationState = { isEnabled: false, isSupported: false, permission: 'default', isLoading: false, lastMessage: '' }

export function usePushNotificationState() {
  const [state, setState] = useState(initialState)
  const checkStatus = useCallback(() => {
    const isSupported = 'serviceWorker' in navigator && 'PushManager' in window
    const settings = pushNotificationSettings.getSettings()
    const subscription = pushNotificationManager.getSubscriptionStatus()
    setState((previous) => ({
      ...previous, isSupported, permission: isSupported ? Notification.permission : 'denied',
      isEnabled: settings.enabled, endpoint: settings.endpoint || subscription.endpoint,
    }))
  }, [])
  useEffect(() => {
    checkStatus()
    if (pushNotificationSettings.isEnabled()) void pushNotificationManager.autoInitialize()
  }, [checkStatus])
  const enable = () => runNotificationAction(setState, async () => {
    const result = await pushNotificationManager.enableOneClick()
    if (result.success) checkStatus()
    return { message: result.message, enabled: result.success }
  })
  const disable = () => runNotificationAction(setState, async () => {
    const result = await pushNotificationManager.disableOneClick()
    if (result.success) checkStatus()
    return { message: result.message, enabled: !result.success ? state.isEnabled : false }
  })
  const test = async () => setState((previous) => ({ ...previous, isLoading: false, lastMessage: 'テスト通知機能は無効化されています（ローカル通知機能削除済み）' }))
  return { state, enable, disable, test }
}

async function runNotificationAction(
  setState: React.Dispatch<React.SetStateAction<NotificationState>>,
  action: () => Promise<{ message: string; enabled: boolean }>,
) {
  setState((previous) => ({ ...previous, isLoading: true, lastMessage: '' }))
  try {
    const result = await action()
    setState((previous) => ({ ...previous, isLoading: false, lastMessage: result.message, isEnabled: result.enabled }))
  } catch (reason) {
    const message = reason instanceof Error ? reason.message : '不明なエラー'
    setState((previous) => ({ ...previous, isLoading: false, lastMessage: `エラー: ${message}` }))
  }
}

export type PushNotificationState = ReturnType<typeof usePushNotificationState>['state']
