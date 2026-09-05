'use client'

import { ModelConnectionSettings } from '@/components/settings/ModelConnectionSettings'
import { AuthMode, BedrockConfig } from '@/types/settings'
import {
  BedrockSettings,
  ClaudeOAuthSettings,
  SelectField,
  SettingsPageHeader,
  SettingsSubsection,
} from '@/components/settings'
import { useSettingsScope } from '../SettingsScopeContext'

export function AIProvidersSection() {
  const { scopeKind, settings, update, userTeams, saveConnection } = useSettingsScope()
  const isPersonal = scopeKind === 'personal'

  const handleBedrockChange = (bedrock: BedrockConfig) => update({ bedrock })

  const handleClaudeOAuthChange = (token: string, authMode: AuthMode) =>
    update({ claude_code_oauth_token: token, auth_mode: authMode })

  return (
    <>
      <SettingsPageHeader
        title="AI プロバイダ"
        description="セッションで使う AI プロバイダと認証方法を設定します。"
      />

      {isPersonal && userTeams.length > 0 && (
        <SettingsSubsection
          title="使用するチーム設定"
          description="選択したチームの設定（Bedrock、MCP サーバー、環境変数など）がセッションに適用されます。未選択の場合はすべてのチームの設定をマージします。"
        >
          <SelectField
            id="preferred-team"
            value={settings.preferred_team_id || ''}
            onChange={(value) => update({ preferred_team_id: value })}
            options={[
              { value: '', label: 'すべてのチームの設定をマージして使用' },
              ...userTeams.map((team) => ({ value: team, label: team })),
            ]}
            className="w-full"
          />
          {settings.preferred_team_id && (
            <p className="mt-1.5 text-xs text-blue-600 dark:text-blue-400">
              セッションは <strong>{settings.preferred_team_id}</strong> の設定のみを使用します
            </p>
          )}
        </SettingsSubsection>
      )}

      <ModelConnectionSettings agent="claude" connection={settings.claude_connection} legacyMode={settings.auth_mode} onSave={connection => saveConnection('claude', connection)} />

      {isPersonal && settings.claude_connection?.mode !== 'anthropic_compatible' && (
        <SettingsSubsection
          title="Claude Code OAuth"
          description="Claude Code の OAuth トークンでセッションを認証します"
        >
          <ClaudeOAuthSettings
            showAuthModeSelector={false}
            hasToken={settings.has_claude_code_oauth_token ?? false}
            authMode={settings.auth_mode}
            onChange={handleClaudeOAuthChange}
          />
        </SettingsSubsection>
      )}

      <SettingsSubsection
        title="Amazon Bedrock"
        description="Bedrock 経由でモデルを利用する場合に設定します"
      >
        <BedrockSettings
          config={settings.bedrock}
          onChange={handleBedrockChange}
          showCredentials={!isPersonal}
        />
      </SettingsSubsection>

      <p className="mt-8 text-xs text-gray-500 dark:text-gray-400">
        Codex は「Codex 認証」で接続を設定できます。両方を設定した場合、自動選択は Codex を優先します。Claude を使う場合はエージェント設定で選択してください。
      </p>
    </>
  )
}
