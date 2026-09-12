'use client'

import { FieldGroup, FieldRow, SelectField, SettingsPageHeader, ToggleSwitch } from '@/components/settings'
import { useSettingsScope } from '../SettingsScopeContext'

const timeoutOptions = [
  { value: '15', label: '15分' },
  { value: '30', label: '30分' },
  { value: '60', label: '1時間' },
  { value: '120', label: '2時間' },
  { value: '240', label: '4時間' },
  { value: '480', label: '8時間' },
  { value: '720', label: '12時間' },
  { value: '1440', label: '24時間' },
]

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
            <SelectField
              id="auto-suspend-timeout"
              value={String(policy.idle_timeout_minutes)}
              disabled={!policy.enabled}
              onChange={(value) => update({ auto_suspend: { ...policy, idle_timeout_minutes: Number(value) } })}
              options={timeoutOptions}
            />
          }
        />
      </FieldGroup>
      <p className="mt-4 text-xs text-gray-500 dark:text-gray-400">
        永続化に対応した実行基盤のセッションにのみ適用されます。
      </p>
    </>
  )
}
