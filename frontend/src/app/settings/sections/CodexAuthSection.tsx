'use client'

import { ModelConnectionSettings } from '@/components/settings/ModelConnectionSettings'
import { CodexCredentialsSettings } from '@/components/settings/CodexCredentialsSettings'
import { ImmediateSaveNotice, SettingsPageHeader } from '@/components/settings'
import { useSettingsScope } from '../SettingsScopeContext'

export function CodexAuthSection() {
  const {
    settings, saveConnection,
    scopeKind,
    scopeId,
    credentialsMetadata,
    reloadCredentials,
    clearCredentialsMetadata,
  } = useSettingsScope()

  return (
    <>
      <SettingsPageHeader
        title="Codex 認証"
        description={
          <>
            Codex の OpenAI 互換 API 接続、または認証情報ファイル <code className="rounded bg-gray-100 px-1 dark:bg-gray-800">~/.codex/auth.json</code>{' '}
            を登録します。登録するとセッション開始時に自動で配置されます。
          </>
        }
      />

      <ImmediateSaveNotice />

      <ModelConnectionSettings agent="codex" connection={settings.codex_connection} onSave={connection => saveConnection('codex', connection)} />

      <CodexCredentialsSettings
        scope={scopeKind === 'personal' ? 'user' : 'team'}
        scopeName={scopeId}
        teamId={scopeKind === 'team' ? scopeId : undefined}
        credentialsMetadata={credentialsMetadata}
        onChanged={reloadCredentials}
        onDeleted={clearCredentialsMetadata}
      />
    </>
  )
}
