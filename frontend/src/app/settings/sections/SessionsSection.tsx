'use client'

import { FieldGroup, FieldRow, SettingsPageHeader, ToggleSwitch } from '@/components/settings'
import { useSettingsScope } from '../SettingsScopeContext'

export function SessionsSection() {
  const { scopeKind, settings, update } = useSettingsScope()
  const policy = settings.auto_suspend ?? { enabled: false, idle_timeout_minutes: 60 }

  return (
    <>
      <SettingsPageHeader
        title="セッション"
        description={scopeKind === 'team'
          ? 'チームが所有するセッションの実行リソースを管理します。'
          : 'パーソナルセッションの実行リソースを管理します。'}
      />
      <FieldGroup>
        <FieldRow
          label="自動サスペンド"
          htmlFor="auto-suspend-enabled"
          description="操作がないセッションを停止します。会話履歴と永続データは保持され、次回アクセス時に自動で再開します。"
          control={<ToggleSwitch id="auto-suspend-enabled" checked={policy.enabled} onChange={(enabled) => update({ auto_suspend: { ...policy, enabled } })} />}
        />
        <FieldRow
          label="サスペンドまでの時間"
          htmlFor="auto-suspend-timeout"
          description="最後の操作が完了してからの時間"
          control={
            <div className="flex items-center gap-2">
              <input
                id="auto-suspend-timeout"
                type="number"
                min={1}
                max={10080}
                step={1}
                value={policy.idle_timeout_minutes}
                disabled={!policy.enabled}
                onChange={(event) => {
                  const minutes = Number(event.target.value)
                  if (Number.isInteger(minutes) && minutes >= 1 && minutes <= 10080) {
                    update({ auto_suspend: { ...policy, idle_timeout_minutes: minutes } })
                  }
                }}
                className="w-28 rounded-md border border-gray-300 bg-white px-3 py-2 text-sm text-gray-900 focus:border-transparent focus:outline-none focus:ring-2 focus:ring-blue-500 disabled:opacity-50 dark:border-gray-700 dark:bg-gray-800 dark:text-white"
              />
              <span className="text-sm text-gray-600 dark:text-gray-300">分</span>
            </div>
          }
        />
      </FieldGroup>
      <p className="mt-4 text-xs text-gray-500 dark:text-gray-400">
        永続化に対応した実行基盤のセッションにのみ適用されます。
      </p>
    </>
  )
}
