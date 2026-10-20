'use client'

import { FieldGroup, FieldRow, SettingsPageHeader, TextField } from '@/components/settings'
import { useSettingsScope } from '../SettingsScopeContext'

export function GitHubAppSection() {
  const { settings, update } = useSettingsScope()

  return (
    <>
      <SettingsPageHeader
        title="GitHub App 認証"
        description="チームのセッションで使う GitHub App のインストールを指定します。"
      />

      <FieldGroup>
        <FieldRow
          label="Installation ID"
          htmlFor="github-app-installation-id"
          description="セッション開始時に、この installation 用の短期トークンを生成して GITHUB_TOKEN に設定します"
        >
          <TextField
            id="github-app-installation-id"
            value={settings.github_app_installation_id || ''}
            onChange={(github_app_installation_id) => update({ github_app_installation_id })}
            inputMode="numeric"
            pattern="[0-9]*"
            placeholder="12345678"
          />
        </FieldRow>
        <FieldRow
          label="クローン可能な追加リポジトリ"
          htmlFor="github-repositories"
          description="1行に1つ、RE2正規表現を指定します。リポジトリ名全体に適用され、セッション起動時のリポジトリは自動的に含まれます"
        >
          <textarea
            id="github-repositories"
            value={(settings.github_repositories || []).join('\n')}
            onChange={(event) => update({ github_repositories: event.target.value.split('\n') })}
            rows={5}
            placeholder={'^owner/repository$\n^owner/service-[a-z]+$'}
            className="w-full rounded-md border border-gray-300 bg-white px-3 py-2 font-mono text-sm dark:border-gray-600 dark:bg-gray-800 dark:text-white"
          />
        </FieldRow>
      </FieldGroup>
    </>
  )
}
